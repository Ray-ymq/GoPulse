package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Metadata struct {
	PluginID              string `json:"plugin_id"`
	PluginVersion         string `json:"plugin_version"`
	PreviousPluginVersion string `json:"previous_plugin_version,omitempty"`
	Operation             string `json:"operation"`
	FromState             string `json:"from_state"`
	ToState               string `json:"to_state"`
}

type Payload struct {
	EventSchemaVersion int      `json:"event_schema_version"`
	EventName          string   `json:"event_name"`
	Source             string   `json:"source"`
	Severity           string   `json:"severity"`
	Timestamp          string   `json:"timestamp"`
	Message            string   `json:"message"`
	Metadata           Metadata `json:"metadata"`
}

type Document struct {
	Timestamp          string   `json:"@timestamp"`
	EventSchemaVersion int      `json:"event_schema_version"`
	EventName          string   `json:"event_name"`
	Source             string   `json:"source"`
	Severity           string   `json:"severity"`
	Message            string   `json:"message"`
	Metadata           Metadata `json:"metadata"`
}

type WriteRequest struct {
	MessageID string          `json:"message_id"`
	IndexDate string          `json:"index_date"`
	Document  json.RawMessage `json:"document"`
}

type Transformer struct{ MaxBytes int }

type spec struct {
	message   string
	operation string
	from      string
	to        string
	update    bool
}

var specs = map[string]spec{
	"exporter_plugin_installed": {message: "exporter plugin installed", operation: "install", from: "not_installed", to: "running"},
	"exporter_plugin_started":   {message: "exporter plugin started", operation: "start", from: "stopped", to: "running"},
	"exporter_plugin_stopped":   {message: "exporter plugin stopped", operation: "stop", from: "running", to: "stopped"},
	"exporter_plugin_updated":   {message: "exporter plugin updated", operation: "update", update: true},
}

func (t Transformer) Transform(message envelope.Envelope) ([]byte, error) {
	if message.Type != "events" || message.Source != "monitor" {
		return nil, &envelope.PermanentError{Code: "unsupported_envelope"}
	}
	if t.MaxBytes > 0 && len(message.RawPayload) > t.MaxBytes {
		return nil, &envelope.PermanentError{Code: "record_too_large"}
	}
	payload, err := Validate(message.RawPayload, message.Timestamp)
	if err != nil {
		return nil, &envelope.PermanentError{Code: "invalid_event_payload"}
	}
	if payload.Source != message.Source {
		return nil, &envelope.PermanentError{Code: "source_mismatch"}
	}
	payloadTime, _ := time.Parse(time.RFC3339Nano, payload.Timestamp)
	if !payloadTime.Equal(message.Timestamp) {
		return nil, &envelope.PermanentError{Code: "timestamp_mismatch"}
	}
	document, err := json.Marshal(Document{Timestamp: payload.Timestamp, EventSchemaVersion: payload.EventSchemaVersion, EventName: payload.EventName, Source: payload.Source, Severity: payload.Severity, Message: payload.Message, Metadata: payload.Metadata})
	if err != nil {
		return nil, &envelope.PermanentError{Code: "transform_failed"}
	}
	request, err := json.Marshal(WriteRequest{MessageID: message.MessageID, IndexDate: message.Timestamp.UTC().Format("2006.01.02"), Document: document})
	if err != nil {
		return nil, &envelope.PermanentError{Code: "transform_failed"}
	}
	return request, nil
}

func Validate(body []byte, envelopeTime time.Time) (Payload, error) {
	if !utf8.Valid(body) || len(body) == 0 || checkUnique(body) != nil {
		return Payload{}, errors.New("invalid event payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var payload Payload
	if decoder.Decode(&payload) != nil || expectEOF(decoder) != nil {
		return Payload{}, errors.New("invalid event payload")
	}
	s, ok := specs[payload.EventName]
	if !ok || payload.EventSchemaVersion != 1 || payload.Source != "monitor" || payload.Severity != "info" || payload.Message != s.message {
		return Payload{}, errors.New("invalid event vocabulary")
	}
	at, err := time.Parse(time.RFC3339Nano, payload.Timestamp)
	if err != nil || at.Location() != time.UTC || !at.Equal(envelopeTime) {
		return Payload{}, errors.New("invalid event timestamp")
	}
	m := payload.Metadata
	if m.PluginID != "redis-exporter" || !semverPattern.MatchString(m.PluginVersion) || m.Operation != s.operation {
		return Payload{}, errors.New("invalid event metadata")
	}
	if s.update {
		if !semverPattern.MatchString(m.PreviousPluginVersion) || !state(m.FromState) || m.FromState != m.ToState {
			return Payload{}, errors.New("invalid event metadata")
		}
	} else if m.PreviousPluginVersion != "" || m.ToState != s.to {
		return Payload{}, errors.New("invalid event metadata")
	} else if payload.EventName == "exporter_plugin_started" {
		if m.FromState != "stopped" && m.FromState != "failed" {
			return Payload{}, errors.New("invalid event metadata")
		}
	} else if payload.EventName == "exporter_plugin_stopped" {
		if m.FromState != "running" && m.FromState != "failed" {
			return Payload{}, errors.New("invalid event metadata")
		}
	} else if m.FromState != s.from {
		return Payload{}, errors.New("invalid event metadata")
	}
	for _, value := range []string{payload.EventName, payload.Source, payload.Severity, payload.Message, m.PluginID, m.PluginVersion, m.PreviousPluginVersion, m.Operation, m.FromState, m.ToState} {
		if !safe(value) {
			return Payload{}, errors.New("unsafe event text")
		}
	}
	return payload, nil
}

func state(value string) bool { return value == "running" || value == "stopped" }
func safe(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 256 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	lower := strings.ToLower(value)
	for _, fragment := range []string{"authorization", "bearer ", "cookie", "token", "password", "jwt", "http://", "https://", "/home/", "/mnt/"} {
		if strings.Contains(lower, fragment) {
			return false
		}
	}
	return true
}

func checkUnique(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := scan(decoder); err != nil {
		return err
	}
	return expectEOF(decoder)
}
func scan(decoder *json.Decoder) error {
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
				return errors.New("invalid key")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate key %s", key)
			}
			seen[key] = struct{}{}
			if err := scan(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scan(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("invalid JSON")
	}
}
func expectEOF(decoder *json.Decoder) error {
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
