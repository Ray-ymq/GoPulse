package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SchemaVersion = 1
	Source        = "monitor"
	MaxEnvelope   = 16 * 1024
)

type Metadata struct {
	PluginID              string `json:"plugin_id"`
	PluginVersion         string `json:"plugin_version"`
	PreviousPluginVersion string `json:"previous_plugin_version,omitempty"`
	Operation             string `json:"operation"`
	FromState             string `json:"from_state"`
	ToState               string `json:"to_state"`
}

type Event struct {
	EventSchemaVersion int      `json:"event_schema_version"`
	EventName          string   `json:"event_name"`
	Source             string   `json:"source"`
	Severity           string   `json:"severity"`
	Timestamp          string   `json:"timestamp"`
	Message            string   `json:"message"`
	Metadata           Metadata `json:"metadata"`
}

type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	MessageID     string `json:"message_id"`
	Type          string `json:"type"`
	Source        string `json:"source"`
	Timestamp     string `json:"timestamp"`
	Payload       Event  `json:"payload"`
}

type specification struct {
	message   string
	operation string
	from      string
	to        string
	update    bool
}

var specifications = map[string]specification{
	"exporter_plugin_installed": {message: "exporter plugin installed", operation: "install", from: "not_installed", to: "running"},
	"exporter_plugin_started":   {message: "exporter plugin started", operation: "start", from: "stopped", to: "running"},
	"exporter_plugin_stopped":   {message: "exporter plugin stopped", operation: "stop", from: "running", to: "stopped"},
	"exporter_plugin_updated":   {message: "exporter plugin updated", operation: "update", update: true},
}

func New(name, version, previousVersion, from, to string, at time.Time) Event {
	spec := specifications[name]
	return Event{EventSchemaVersion: SchemaVersion, EventName: name, Source: Source, Severity: "info", Timestamp: at.UTC().Format(time.RFC3339Nano), Message: spec.message, Metadata: Metadata{PluginID: "redis-exporter", PluginVersion: version, PreviousPluginVersion: previousVersion, Operation: spec.operation, FromState: from, ToState: to}}
}

func Validate(event Event, now time.Time) error {
	spec, ok := specifications[event.EventName]
	if !ok || event.EventSchemaVersion != SchemaVersion || event.Source != Source || event.Severity != "info" || event.Message != spec.message {
		return errors.New("event contract is invalid")
	}
	at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil || at.Location() != time.UTC || at.After(now.UTC().Add(5*time.Minute)) {
		return errors.New("event timestamp is invalid")
	}
	m := event.Metadata
	if m.PluginID != "redis-exporter" || !validSemver(m.PluginVersion) || m.Operation != spec.operation {
		return errors.New("event metadata is invalid")
	}
	if spec.update {
		if !validSemver(m.PreviousPluginVersion) || !validState(m.FromState) || m.FromState != m.ToState {
			return errors.New("event metadata is invalid")
		}
	} else if m.PreviousPluginVersion != "" || m.ToState != spec.to {
		return errors.New("event metadata is invalid")
	} else if event.EventName == "exporter_plugin_started" {
		if m.FromState != "stopped" && m.FromState != "failed" {
			return errors.New("event metadata is invalid")
		}
	} else if event.EventName == "exporter_plugin_stopped" {
		if m.FromState != "running" && m.FromState != "failed" {
			return errors.New("event metadata is invalid")
		}
	} else if m.FromState != spec.from {
		return errors.New("event metadata is invalid")
	}
	for _, value := range []string{event.EventName, event.Source, event.Severity, event.Message, m.PluginID, m.PluginVersion, m.PreviousPluginVersion, m.Operation, m.FromState, m.ToState} {
		if !safeString(value) {
			return errors.New("event contains unsafe text")
		}
	}
	return nil
}

func CanonicalEnvelope(event Event, messageID string, now time.Time) ([]byte, error) {
	if len(messageID) != 32 || strings.Trim(messageID, "0123456789abcdef") != "" {
		return nil, errors.New("message id is invalid")
	}
	if err := Validate(event, now); err != nil {
		return nil, err
	}
	body, err := json.Marshal(Envelope{SchemaVersion: SchemaVersion, MessageID: messageID, Type: "events", Source: Source, Timestamp: event.Timestamp, Payload: event})
	if err != nil {
		return nil, errors.New("event serialization failed")
	}
	if len(body) > MaxEnvelope {
		return nil, errors.New("event exceeds maximum size")
	}
	return body, nil
}

func DecodeStrict(body []byte, now time.Time) (Envelope, error) {
	if !utf8.Valid(body) || len(body) == 0 || len(body) > MaxEnvelope {
		return Envelope{}, errors.New("event envelope is invalid")
	}
	if err := checkUniqueJSON(body); err != nil {
		return Envelope{}, errors.New("event envelope is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, errors.New("event envelope is invalid")
	}
	if err := expectJSONEOF(decoder); err != nil {
		return Envelope{}, errors.New("event envelope has trailing data")
	}
	if envelope.SchemaVersion != SchemaVersion || envelope.Type != "events" || envelope.Source != Source || envelope.Timestamp != envelope.Payload.Timestamp {
		return Envelope{}, errors.New("event envelope is invalid")
	}
	if _, err := CanonicalEnvelope(envelope.Payload, envelope.MessageID, now); err != nil {
		return Envelope{}, fmt.Errorf("event envelope is invalid: %w", err)
	}
	return envelope, nil
}

func validSemver(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func validState(value string) bool {
	return value == "not_installed" || value == "stopped" || value == "running" || value == "failed"
}
func safeString(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"authorization", "bearer ", "cookie", "token", "password", "jwt", "http://", "https://", "/home/", "/mnt/"} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func checkUniqueJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	return expectJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON key")
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("invalid JSON delimiter")
	}
}

func expectJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("trailing JSON")
	}
	return err
}
