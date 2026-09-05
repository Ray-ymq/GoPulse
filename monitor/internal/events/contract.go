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
	PluginVersion         string `json:"plugin_version,omitempty"`
	PreviousPluginVersion string `json:"previous_plugin_version,omitempty"`
	Operation             string `json:"operation,omitempty"`
	FromState             string `json:"from_state,omitempty"`
	ToState               string `json:"to_state,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	ScrapeStatus          string `json:"scrape_status,omitempty"`
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
	message  string
	severity string
}

var specifications = map[string]specification{
	"exporter_plugin_installed":    {message: "exporter plugin installed", severity: "info"},
	"exporter_plugin_started":      {message: "exporter plugin started", severity: "info"},
	"exporter_plugin_stopped":      {message: "exporter plugin stopped", severity: "info"},
	"exporter_plugin_updated":      {message: "exporter plugin updated", severity: "info"},
	"exporter_plugin_failed":       {message: "exporter plugin operation failed", severity: "error"},
	"exporter_plugin_exited":       {message: "exporter plugin exited unexpectedly", severity: "error"},
	"metrics_collection_failed":    {message: "metrics collection failed", severity: "warn"},
	"metrics_collection_recovered": {message: "metrics collection recovered", severity: "info"},
	"metrics_target_unavailable":   {message: "metrics target unavailable", severity: "warn"},
	"metrics_target_recovered":     {message: "metrics target recovered", severity: "info"},
}

func event(name string, metadata Metadata, at time.Time) Event {
	spec := specifications[name]
	return Event{EventSchemaVersion: SchemaVersion, EventName: name, Source: Source, Severity: spec.severity, Timestamp: at.UTC().Format(time.RFC3339Nano), Message: spec.message, Metadata: metadata}
}

func New(name, version, previousVersion, from, to string, at time.Time) Event {
	operations := map[string]string{"exporter_plugin_installed": "install", "exporter_plugin_started": "start", "exporter_plugin_stopped": "stop", "exporter_plugin_updated": "update"}
	return event(name, Metadata{PluginID: "redis-exporter", PluginVersion: version, PreviousPluginVersion: previousVersion, Operation: operations[name], FromState: from, ToState: to}, at)
}

func NewPluginFailure(version, operation, errorCode, to string, at time.Time) Event {
	return event("exporter_plugin_failed", Metadata{PluginID: "redis-exporter", PluginVersion: version, Operation: operation, ToState: to, ErrorCode: errorCode}, at)
}

func NewPluginExited(version string, at time.Time) Event {
	return event("exporter_plugin_exited", Metadata{PluginID: "redis-exporter", PluginVersion: version, Operation: "start", ToState: "failed", ErrorCode: "process_exited"}, at)
}

func NewMetrics(name, errorCode, scrapeStatus string, at time.Time) Event {
	operation := "scrape"
	if errorCode == "publish_failed" {
		operation = "publish"
	}
	return event(name, Metadata{PluginID: "redis-exporter", Operation: operation, ErrorCode: errorCode, ScrapeStatus: scrapeStatus}, at)
}

func Validate(event Event, now time.Time) error {
	spec, ok := specifications[event.EventName]
	if !ok || event.EventSchemaVersion != SchemaVersion || event.Source != Source || event.Severity != spec.severity || event.Message != spec.message {
		return errors.New("event contract is invalid")
	}
	at, err := time.Parse(time.RFC3339Nano, event.Timestamp)
	if err != nil || at.Location() != time.UTC || at.After(now.UTC().Add(5*time.Minute)) {
		return errors.New("event timestamp is invalid")
	}
	if !validMetadata(event.EventName, event.Metadata) {
		return errors.New("event metadata is invalid")
	}
	m := event.Metadata
	for _, value := range []string{event.EventName, event.Source, event.Severity, event.Message, m.PluginID, m.PluginVersion, m.PreviousPluginVersion, m.Operation, m.FromState, m.ToState, m.ErrorCode, m.ScrapeStatus} {
		if !safeString(value) {
			return errors.New("event contains unsafe text")
		}
	}
	return nil
}

func validMetadata(name string, m Metadata) bool {
	if m.PluginID != "redis-exporter" {
		return false
	}
	emptyFailure := m.ErrorCode == "" && m.ScrapeStatus == ""
	switch name {
	case "exporter_plugin_installed":
		return emptyFailure && validSemver(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "install" && m.FromState == "not_installed" && m.ToState == "running"
	case "exporter_plugin_started":
		return emptyFailure && validSemver(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "start" && (m.FromState == "stopped" || m.FromState == "failed") && m.ToState == "running"
	case "exporter_plugin_stopped":
		return emptyFailure && validSemver(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "stop" && (m.FromState == "running" || m.FromState == "failed") && m.ToState == "stopped"
	case "exporter_plugin_updated":
		return emptyFailure && validSemver(m.PluginVersion) && validSemver(m.PreviousPluginVersion) && m.Operation == "update" && (m.FromState == "running" || m.FromState == "stopped") && m.FromState == m.ToState
	case "exporter_plugin_failed":
		return validSemver(m.PluginVersion) && m.PreviousPluginVersion == "" && m.FromState == "" && validState(m.ToState) && validPluginFailure(m.Operation, m.ErrorCode) && m.ScrapeStatus == ""
	case "exporter_plugin_exited":
		return validSemver(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "start" && m.FromState == "" && m.ToState == "failed" && m.ErrorCode == "process_exited" && m.ScrapeStatus == ""
	case "metrics_collection_failed":
		return m.PluginVersion == "" && m.PreviousPluginVersion == "" && m.FromState == "" && m.ToState == "" && validMetricsFailure(m.Operation, m.ErrorCode) && m.ScrapeStatus == ""
	case "metrics_collection_recovered":
		return metricsTransition(m, "success")
	case "metrics_target_unavailable":
		return metricsTransition(m, "target_unavailable")
	case "metrics_target_recovered":
		return metricsTransition(m, "success")
	default:
		return false
	}
}

func validPluginFailure(operation, code string) bool {
	allowed := map[string]map[string]bool{
		"start":   {"start_failed": true},
		"stop":    {"stop_failed": true},
		"update":  {"update_failed": true, "rollback_failed": true},
		"recover": {"recovery_failed": true, "recovery_invalid": true},
	}
	return allowed[operation][code]
}

func validMetricsFailure(operation, code string) bool {
	if operation == "publish" {
		return code == "publish_failed"
	}
	if operation != "scrape" {
		return false
	}
	return map[string]bool{"scrape_timeout": true, "network_failed": true, "response_too_large": true, "parse_failed": true, "contract_invalid": true, "content_invalid": true, "http_invalid": true, "scrape_failed": true, "message_id_failed": true}[code]
}

func metricsTransition(m Metadata, status string) bool {
	return m.PluginVersion == "" && m.PreviousPluginVersion == "" && m.Operation == "scrape" && m.FromState == "" && m.ToState == "" && m.ErrorCode == "" && m.ScrapeStatus == status
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
	if !utf8.ValidString(value) || len(value) > 256 {
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
