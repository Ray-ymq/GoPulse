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
	PluginVersion         string `json:"plugin_version,omitempty"`
	PreviousPluginVersion string `json:"previous_plugin_version,omitempty"`
	Operation             string `json:"operation,omitempty"`
	FromState             string `json:"from_state,omitempty"`
	ToState               string `json:"to_state,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	ScrapeStatus          string `json:"scrape_status,omitempty"`
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
	message  string
	severity string
}

var specs = map[string]spec{
	"exporter_plugin_installed": {"exporter plugin installed", "info"}, "exporter_plugin_started": {"exporter plugin started", "info"}, "exporter_plugin_stopped": {"exporter plugin stopped", "info"}, "exporter_plugin_updated": {"exporter plugin updated", "info"},
	"exporter_plugin_failed": {"exporter plugin operation failed", "error"}, "exporter_plugin_exited": {"exporter plugin exited unexpectedly", "error"},
	"metrics_collection_failed": {"metrics collection failed", "warn"}, "metrics_collection_recovered": {"metrics collection recovered", "info"}, "metrics_target_unavailable": {"metrics target unavailable", "warn"}, "metrics_target_recovered": {"metrics target recovered", "info"},
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
	document, err := json.Marshal(Document{payload.Timestamp, payload.EventSchemaVersion, payload.EventName, payload.Source, payload.Severity, payload.Message, payload.Metadata})
	if err != nil {
		return nil, &envelope.PermanentError{Code: "transform_failed"}
	}
	request, err := json.Marshal(WriteRequest{message.MessageID, message.Timestamp.UTC().Format("2006.01.02"), document})
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
	if !ok || payload.EventSchemaVersion != 1 || payload.Source != "monitor" || payload.Severity != s.severity || payload.Message != s.message {
		return Payload{}, errors.New("invalid event vocabulary")
	}
	at, err := time.Parse(time.RFC3339Nano, payload.Timestamp)
	if err != nil || at.Location() != time.UTC || !at.Equal(envelopeTime) {
		return Payload{}, errors.New("invalid event timestamp")
	}
	if !validMetadata(payload.EventName, payload.Metadata) {
		return Payload{}, errors.New("invalid event metadata")
	}
	m := payload.Metadata
	for _, value := range []string{payload.EventName, payload.Source, payload.Severity, payload.Message, m.PluginID, m.PluginVersion, m.PreviousPluginVersion, m.Operation, m.FromState, m.ToState, m.ErrorCode, m.ScrapeStatus} {
		if !safe(value) {
			return Payload{}, errors.New("unsafe event text")
		}
	}
	return payload, nil
}

func validMetadata(name string, m Metadata) bool {
	if m.PluginID != "redis-exporter" {
		return false
	}
	empty := m.ErrorCode == "" && m.ScrapeStatus == ""
	switch name {
	case "exporter_plugin_installed":
		return empty && semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "install" && m.FromState == "not_installed" && m.ToState == "running"
	case "exporter_plugin_started":
		return empty && semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "start" && (m.FromState == "stopped" || m.FromState == "failed") && m.ToState == "running"
	case "exporter_plugin_stopped":
		return empty && semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "stop" && (m.FromState == "running" || m.FromState == "failed") && m.ToState == "stopped"
	case "exporter_plugin_updated":
		return empty && semverPattern.MatchString(m.PluginVersion) && semverPattern.MatchString(m.PreviousPluginVersion) && m.Operation == "update" && (m.FromState == "running" || m.FromState == "stopped") && m.FromState == m.ToState
	case "exporter_plugin_failed":
		return semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.FromState == "" && validState(m.ToState) && validPluginFailure(m.Operation, m.ErrorCode) && m.ScrapeStatus == ""
	case "exporter_plugin_exited":
		return semverPattern.MatchString(m.PluginVersion) && m.PreviousPluginVersion == "" && m.Operation == "start" && m.FromState == "" && m.ToState == "failed" && m.ErrorCode == "process_exited" && m.ScrapeStatus == ""
	case "metrics_collection_failed":
		return m.PluginVersion == "" && m.PreviousPluginVersion == "" && m.FromState == "" && m.ToState == "" && validMetricsFailure(m.Operation, m.ErrorCode) && m.ScrapeStatus == ""
	case "metrics_collection_recovered", "metrics_target_recovered":
		return metricsTransition(m, "success")
	case "metrics_target_unavailable":
		return metricsTransition(m, "target_unavailable")
	}
	return false
}
func validPluginFailure(operation, code string) bool {
	allowed := map[string]map[string]bool{"start": {"start_failed": true}, "stop": {"stop_failed": true}, "update": {"update_failed": true, "rollback_failed": true}, "recover": {"recovery_failed": true, "recovery_invalid": true}}
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
func validState(value string) bool {
	return value == "not_installed" || value == "stopped" || value == "running" || value == "failed"
}
func safe(value string) bool {
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
