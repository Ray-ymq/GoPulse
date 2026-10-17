package logs

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

	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

var (
	messageIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	traceIDPattern   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern    = regexp.MustCompile(`^[0-9a-f]{16}$`)
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	tokenPattern     = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	identityPattern  = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,255}$`)
)

var allowedFields = map[string]struct{}{
	"version": {}, "revision": {}, "event": {}, "runtime_contract_version": {}, "runtime_mode": {}, "listen": {},
	"log_schema_version": {}, "timestamp": {}, "level": {}, "service": {}, "module": {}, "message": {}, "instance_id": {},
	"request_id": {}, "trace_id": {}, "span_id": {}, "event_id": {}, "command_id": {}, "data_epoch": {}, "event_type": {}, "projection_generation": {}, "routing_contract_revision": {}, "consumer": {}, "user_id": {}, "post_id": {}, "content_revision": {}, "comment_id": {},
	"notification_id": {}, "outbox_id": {}, "method": {}, "route": {}, "status": {}, "duration_ms": {},
	"response_bytes": {}, "error_code": {}, "reason": {}, "operation": {}, "resource": {}, "stage": {},
	"result": {}, "attempt": {}, "attempt_id": {}, "batch_size": {}, "document_count": {}, "panic_recovered": {}, "response_committed": {},
}

var workerMessages = map[string]struct{}{
	"event ignored": {}, "event processed": {}, "message acknowledgement failed": {},
	"retry publish failed": {}, "message requeue failed": {}, "event retry scheduled": {},
	"dead letter publish failed": {}, "event dead lettered": {}, "connection unavailable": {},
	"connection restored": {}, "session close failed": {}, "session interrupted": {},
	"delivery stop failed": {}, "shutdown timeout": {},
}

var serviceModules = map[string]map[string]map[string]struct{}{
	"backend": {
		"alert": {"alert evaluation failed": {}},
		"http":  {"request id generation failed": {}, "http request completed": {}, "http panic recovered": {}},
		"auth":  {"user registered": {}, "user logged in": {}, "user logged out": {}},
		"post":  {"post created": {}, "post committed": {}}, "comment": {"comment created": {}},
		"like": {"post liked": {}, "post unliked": {}}, "notification": {"notification marked read": {}},
		"cache":     {"post detail cache fill failed": {}, "post detail cache read failed": {}, "post detail cache invalidation failed": {}},
		"outbox":    {"outbox cleanup failed": {}, "outbox claim failed": {}, "outbox event invalid": {}, "outbox publish failed": {}, "outbox mark published failed": {}, "outbox event published": {}, "outbox release failed": {}},
		"lifecycle": {"backend listening": {}, "backend stopped": {}, "backend server failed": {}, "backend shutdown started": {}, "backend shutdown failed": {}, "resource close failed": {}},
	},
	"business-worker": {
		"lifecycle": {"business worker started": {}, "business worker stopped": {}, "business worker initialization failed": {}, "resource close failed": {}},
		"worker":    workerMessages, "notification": workerMessages,
	},
	"search-indexer": {
		"lifecycle": {"search indexer started": {}, "search indexer stopped": {}, "search indexer initialization failed": {}, "resource close failed": {}},
		"worker":    workerMessages, "search": workerMessages,
	},
	"search-reindex": {
		"search": {"search reindex arguments invalid": {}, "search reindex initialization failed": {}, "search reindex started": {}, "search reindex skipped": {}, "search reindex completed": {}, "search reindex failed": {}, "resource close failed": {}},
	},
}

var sensitiveFragments = []string{"password", "authorization", "cookie", "jwt", "bearer ", "post_content", "comment_content", "stack", "http://", "https://", "/home/", "/mnt/"}

type Envelope struct {
	SchemaVersion int             `json:"schema_version"`
	MessageID     string          `json:"message_id"`
	Type          string          `json:"type"`
	Source        string          `json:"source"`
	Timestamp     string          `json:"timestamp"`
	Payload       json.RawMessage `json:"payload"`
}

type Validated struct {
	Timestamp string
	Source    string
	Payload   json.RawMessage
	Entries   []Entry
}

type Entry struct {
	Timestamp string
	Level     string
	Service   string
	Module    string
	Message   string
	Fields    map[string]any
}

type wireEntry struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Service   string         `json:"service"`
	Module    string         `json:"module"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields"`
}

type wirePayload struct {
	DataID int         `json:"dataid"`
	Time   int64       `json:"time"`
	Data   []wireEntry `json:"data"`
}

func NewEnvelope(messageID string, value Validated) (Envelope, error) {
	if !messageIDPattern.MatchString(messageID) {
		return Envelope{}, errors.New("invalid message id")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, value.Timestamp)
	if err != nil || timestamp.Location() != time.UTC || value.Source == "" {
		return Envelope{}, errors.New("invalid log envelope")
	}
	entries := make([]wireEntry, 0, len(value.Entries))
	if len(value.Entries) > 0 {
		for _, entry := range value.Entries {
			if entry.Service != value.Source || entry.Timestamp == "" {
				return Envelope{}, errors.New("invalid log envelope")
			}
			if _, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err != nil {
				return Envelope{}, errors.New("invalid log envelope")
			}
			entries = append(entries, wireEntry{Timestamp: entry.Timestamp, Level: entry.Level, Service: entry.Service, Module: entry.Module, Message: entry.Message, Fields: entry.Fields})
		}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(value.Payload))
		decoder.UseNumber()
		var fields map[string]any
		if err := decoder.Decode(&fields); err != nil || fields == nil || expectEOF(decoder) != nil {
			return Envelope{}, errors.New("invalid log envelope")
		}
		extra := make(map[string]any)
		for key, item := range fields {
			switch key {
			case "log_schema_version", "timestamp", "level", "service", "module", "message":
			default:
				extra[key] = item
			}
		}
		level, levelOK := fields["level"].(string)
		module, moduleOK := fields["module"].(string)
		message, messageOK := fields["message"].(string)
		if !levelOK || !moduleOK || !messageOK {
			return Envelope{}, errors.New("invalid log envelope")
		}
		entries = append(entries, wireEntry{Timestamp: value.Timestamp, Level: level, Service: value.Source, Module: module, Message: message, Fields: extra})
	}
	if len(entries) == 0 || timestamp.Unix() <= 0 {
		return Envelope{}, errors.New("invalid log envelope")
	}
	payload, err := json.Marshal(wirePayload{DataID: 3001, Time: timestamp.Unix(), Data: entries})
	if err != nil {
		return Envelope{}, errors.New("log serialization failed")
	}
	return Envelope{SchemaVersion: 2, MessageID: messageID, Type: "logs", Source: value.Source, Timestamp: value.Timestamp, Payload: payload}, nil
}

func Validate(body []byte, now time.Time, futureSkew time.Duration) (Validated, error) {
	if !utf8.Valid(body) || len(body) == 0 {
		return Validated{}, errors.New("invalid log JSON")
	}
	if err := checkUnique(body); err != nil {
		return Validated{}, err
	}
	if batch, ok := decodeBatch(body); ok {
		return validateBatch(batch, now, futureSkew)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return Validated{}, errors.New("invalid log object")
	}
	if err := expectEOF(decoder); err != nil {
		return Validated{}, err
	}
	for key := range fields {
		if _, ok := allowedFields[key]; !ok {
			return Validated{}, fmt.Errorf("unknown field %q", key)
		}
	}
	for _, key := range []string{"log_schema_version", "timestamp", "level", "service", "module", "message"} {
		if _, ok := fields[key]; !ok {
			return Validated{}, errors.New("required field missing")
		}
	}
	if number, ok := fields["log_schema_version"].(json.Number); !ok || number.String() != "1" {
		return Validated{}, errors.New("invalid schema")
	}
	timestamp, ok := fields["timestamp"].(string)
	if !ok || !strings.HasSuffix(timestamp, "Z") {
		return Validated{}, errors.New("invalid timestamp")
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || parsed.Location() != time.UTC || parsed.After(now.UTC().Add(futureSkew)) {
		return Validated{}, errors.New("invalid timestamp")
	}
	level, levelOK := fields["level"].(string)
	service, serviceOK := fields["service"].(string)
	module, moduleOK := fields["module"].(string)
	message, messageOK := fields["message"].(string)
	if !levelOK || (level != "info" && level != "warn" && level != "error") || !serviceOK || !moduleOK || !messageOK {
		return Validated{}, errors.New("invalid log vocabulary")
	}
	modules, ok := serviceModules[service]
	if !ok {
		return Validated{}, errors.New("invalid service")
	}
	messages, ok := modules[module]
	if !ok {
		return Validated{}, errors.New("invalid module")
	}
	if _, ok := messages[message]; !ok {
		return Validated{}, errors.New("invalid message")
	}
	if err := validateOptional(fields); err != nil {
		return Validated{}, err
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		return Validated{}, errors.New("log serialization failed")
	}
	return Validated{Timestamp: timestamp, Source: service, Payload: canonical}, nil
}

func decodeBatch(body []byte) (wirePayload, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return wirePayload{}, false
	}
	if fields["dataid"] == nil && fields["data"] == nil && fields["time"] == nil {
		return wirePayload{}, false
	}
	if len(fields) != 3 || fields["dataid"] == nil || fields["time"] == nil || fields["data"] == nil {
		return wirePayload{}, true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var batch wirePayload
	if decoder.Decode(&batch) != nil || expectEOF(decoder) != nil {
		return wirePayload{}, true
	}
	return batch, true
}

func validateBatch(batch wirePayload, now time.Time, futureSkew time.Duration) (Validated, error) {
	if batch.DataID != 3001 || batch.Time <= 0 || len(batch.Data) == 0 || len(batch.Data) > 256 {
		return Validated{}, errors.New("invalid log batch")
	}
	if batch.Data[0].Service == "" {
		return Validated{}, errors.New("invalid service")
	}
	source := batch.Data[0].Service
	entries := make([]Entry, len(batch.Data))
	for i, raw := range batch.Data {
		if raw.Fields == nil || raw.Timestamp == "" || !strings.HasSuffix(raw.Timestamp, "Z") {
			return Validated{}, errors.New("invalid log entry")
		}
		if raw.Service != source {
			return Validated{}, errors.New("log source mismatch")
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw.Timestamp)
		if err != nil || parsed.Location() != time.UTC || parsed.After(now.UTC().Add(futureSkew)) {
			return Validated{}, errors.New("invalid log timestamp")
		}
		if i == 0 && batch.Time != parsed.Unix() {
			return Validated{}, errors.New("invalid log batch time")
		}
		if raw.Level != "info" && raw.Level != "warn" && raw.Level != "error" {
			return Validated{}, errors.New("invalid log vocabulary")
		}
		modules, ok := serviceModules[raw.Service]
		if !ok {
			return Validated{}, errors.New("invalid service")
		}
		messages, ok := modules[raw.Module]
		if !ok {
			return Validated{}, errors.New("invalid module")
		}
		if _, ok := messages[raw.Message]; !ok {
			return Validated{}, errors.New("invalid message")
		}
		if err := validateBatchFields(raw.Fields); err != nil {
			return Validated{}, err
		}
		fields := make(map[string]any, len(raw.Fields))
		for key, value := range raw.Fields {
			fields[key] = value
		}
		entries[i] = Entry{Timestamp: raw.Timestamp, Level: raw.Level, Service: raw.Service, Module: raw.Module, Message: raw.Message, Fields: fields}
	}
	if entries[0].Timestamp == "" {
		return Validated{}, errors.New("invalid log entry")
	}
	canonical, err := json.Marshal(flattenEntry(entries[0]))
	if err != nil {
		return Validated{}, errors.New("log serialization failed")
	}
	return Validated{Timestamp: entries[0].Timestamp, Source: source, Payload: canonical, Entries: entries}, nil
}

func validateBatchFields(fields map[string]any) error {
	for key := range fields {
		if _, ok := allowedFields[key]; !ok || key == "timestamp" || key == "level" || key == "service" || key == "module" || key == "message" || key == "log_schema_version" {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	return validateOptional(fields)
}

func flattenEntry(entry Entry) map[string]any {
	fields := map[string]any{
		"log_schema_version": 1,
		"timestamp":          entry.Timestamp,
		"level":              entry.Level,
		"service":            entry.Service,
		"module":             entry.Module,
		"message":            entry.Message,
	}
	for key, value := range entry.Fields {
		fields[key] = value
	}
	return fields
}

func validateOptional(fields map[string]any) error {
	positive := map[string]bool{"user_id": true, "post_id": true, "content_revision": true, "comment_id": true, "notification_id": true, "outbox_id": true}
	nonnegative := map[string]bool{"status": true, "duration_ms": true, "response_bytes": true, "attempt": true, "batch_size": true, "document_count": true}
	for key, value := range fields {
		switch typed := value.(type) {
		case string:
			if typed == "" || len(typed) > 256 || hasControl(typed) || sensitive(typed) {
				return errors.New("invalid string field")
			}
			switch key {
			case "instance_id":
				if componentmetrics.ValidateInstanceID(typed) != nil {
					return errors.New("invalid instance identity")
				}
			case "request_id":
				if !requestIDPattern.MatchString(typed) {
					return errors.New("invalid request id")
				}
			case "trace_id":
				if !traceIDPattern.MatchString(typed) || strings.Trim(typed, "0") == "" {
					return errors.New("invalid trace id")
				}
			case "span_id":
				if !spanIDPattern.MatchString(typed) || strings.Trim(typed, "0") == "" {
					return errors.New("invalid span id")
				}
			case "event_id":
				if !uuidPattern.MatchString(typed) {
					return errors.New("invalid event id")
				}
			case "command_id", "data_epoch":
				if !uuidPattern.MatchString(typed) {
					return errors.New("invalid business identity")
				}
			case "projection_generation":
				if !identityPattern.MatchString(typed) {
					return errors.New("invalid projection generation")
				}
			case "routing_contract_revision":
				if typed != "post-created-search/1" {
					return errors.New("invalid routing contract revision")
				}
			case "consumer":
				if typed != "business" && typed != "search" {
					return errors.New("invalid consumer")
				}
			case "method":
				if typed != "GET" && typed != "POST" && typed != "PUT" && typed != "PATCH" && typed != "DELETE" && typed != "OPTIONS" && typed != "HEAD" {
					return errors.New("invalid method")
				}
			case "route":
				if typed != "unmatched" && (!strings.HasPrefix(typed, "/") || strings.ContainsAny(typed, "?#") || strings.Contains(typed, "//")) {
					return errors.New("invalid route")
				}
			case "event", "version", "revision", "event_type", "error_code", "reason", "operation", "resource", "stage", "result":
				if !tokenPattern.MatchString(typed) {
					return errors.New("invalid token")
				}
			}
		case json.Number:
			integer, err := typed.Int64()
			if err != nil || integer > 1_000_000_000_000 || (positive[key] && integer <= 0) || (nonnegative[key] && integer < 0) {
				return errors.New("invalid numeric field")
			}
		case bool:
			if key != "panic_recovered" && key != "response_committed" {
				return errors.New("invalid boolean field")
			}
		default:
			return errors.New("nested or null field is not allowed")
		}
	}
	return nil
}

func sensitive(value string) bool {
	lower := strings.ToLower(value)
	for _, fragment := range sensitiveFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}
func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func checkUnique(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
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
				return errors.New("invalid object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate object key")
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
		return errors.New("invalid JSON delimiter")
	}
}
func expectEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("trailing JSON")
}
