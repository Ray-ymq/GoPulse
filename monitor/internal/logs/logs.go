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
)

var (
	messageIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	requestIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	tokenPattern     = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

var allowedFields = map[string]struct{}{
	"log_schema_version": {}, "timestamp": {}, "level": {}, "service": {}, "module": {}, "message": {},
	"request_id": {}, "event_id": {}, "event_type": {}, "user_id": {}, "post_id": {}, "comment_id": {},
	"notification_id": {}, "outbox_id": {}, "method": {}, "route": {}, "status": {}, "duration_ms": {},
	"response_bytes": {}, "error_code": {}, "reason": {}, "operation": {}, "resource": {}, "stage": {},
	"result": {}, "attempt": {}, "batch_size": {}, "document_count": {}, "panic_recovered": {}, "response_committed": {},
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
		"http": {"request id generation failed": {}, "http request completed": {}, "http panic recovered": {}},
		"auth": {"user registered": {}, "user logged in": {}, "user logged out": {}},
		"post": {"post created": {}}, "comment": {"comment created": {}},
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
}

func NewEnvelope(messageID string, value Validated) (Envelope, error) {
	if !messageIDPattern.MatchString(messageID) {
		return Envelope{}, errors.New("invalid message id")
	}
	return Envelope{SchemaVersion: 1, MessageID: messageID, Type: "logs", Source: value.Source, Timestamp: value.Timestamp, Payload: value.Payload}, nil
}

func Validate(body []byte, now time.Time, futureSkew time.Duration) (Validated, error) {
	if !utf8.Valid(body) || len(body) == 0 {
		return Validated{}, errors.New("invalid log JSON")
	}
	if err := checkUnique(body); err != nil {
		return Validated{}, err
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

func validateOptional(fields map[string]any) error {
	positive := map[string]bool{"user_id": true, "post_id": true, "comment_id": true, "notification_id": true, "outbox_id": true}
	nonnegative := map[string]bool{"status": true, "duration_ms": true, "response_bytes": true, "attempt": true, "batch_size": true, "document_count": true}
	for key, value := range fields {
		switch typed := value.(type) {
		case string:
			if typed == "" || len(typed) > 256 || hasControl(typed) || sensitive(typed) {
				return errors.New("invalid string field")
			}
			switch key {
			case "request_id":
				if !requestIDPattern.MatchString(typed) {
					return errors.New("invalid request id")
				}
			case "event_id":
				if !uuidPattern.MatchString(typed) {
					return errors.New("invalid event id")
				}
			case "method":
				if typed != "GET" && typed != "POST" && typed != "PUT" && typed != "PATCH" && typed != "DELETE" && typed != "OPTIONS" && typed != "HEAD" {
					return errors.New("invalid method")
				}
			case "route":
				if typed != "unmatched" && (!strings.HasPrefix(typed, "/") || strings.ContainsAny(typed, "?#") || strings.Contains(typed, "//")) {
					return errors.New("invalid route")
				}
			case "event_type", "error_code", "reason", "operation", "resource", "stage", "result":
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
