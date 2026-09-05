package envelope

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const validBody = `{ "schema_version":1, "message_id":"0123456789abcdef0123456789abcdef", "type":"metrics", "source":"redis", "timestamp":"2026-09-03T12:34:56.123456789Z", "payload":{"samples":[]} }`

func TestValidatePreservesValidBody(t *testing.T) {
	message, err := Validate([]byte(validBody))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if message.MessageID != "0123456789abcdef0123456789abcdef" || string(message.Body) != validBody {
		t.Fatalf("unexpected message: %+v", message)
	}
}

func TestValidateRejectsInvalidAndUnsupportedMessages(t *testing.T) {
	for name, body := range map[string]string{
		"non object":     `[]`,
		"quoted schema":  `{"schema_version":"1","message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`,
		"decimal schema": `{"schema_version":1.0,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`,
		"duplicate":      `{"schema_version":1,"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`,
		"unknown":        `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{},"topic":"other"}`,
		"trailing":       validBody + ` {}`,
		"message id":     `{"schema_version":1,"message_id":"ABC","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`,
		"timestamp":      `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56+08:00","payload":{}}`,
		"payload":        `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate([]byte(body)); err == nil {
				t.Fatal("Validate() accepted invalid message")
			}
		})
	}

	unsupported := `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"logs","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`
	_, err := Validate([]byte(unsupported))
	var target UnsupportedError
	if !errors.As(err, &target) {
		t.Fatalf("Validate() error = %v, want UnsupportedError", err)
	}
}

func TestValidateRejectsInvalidUTF8(t *testing.T) {
	body := append([]byte(`{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{"x":"`), 0xff)
	body = append(body, []byte(`"}}`)...)
	if _, err := Validate(body); err == nil {
		t.Fatal("Validate() accepted invalid UTF-8")
	}
}

func TestValidateAcceptsBackendLogsAndPreservesBytes(t *testing.T) {
	body := `{"schema_version":1,"message_id":"abcdef0123456789abcdef0123456789","type":"logs","source":"backend","timestamp":"2026-09-04T12:00:00Z","payload":{"service":"backend"}}`
	message, err := Validate([]byte(body))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if message.Type != "logs" || message.Source != "backend" || string(message.Body) != body {
		t.Fatalf("message = %+v", message)
	}
}

func TestValidateAcceptsEveryLogSource(t *testing.T) {
	for _, source := range []string{"backend", "business-worker", "search-indexer", "search-reindex"} {
		body := fmt.Sprintf(`{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"logs","source":%q,"timestamp":"2026-09-04T12:00:00Z","payload":{"service":%q}}`, source, source)
		message, err := Validate([]byte(body))
		if err != nil {
			t.Fatalf("source %q: %v", source, err)
		}
		if message.Source != source {
			t.Fatalf("source = %q, want %q", message.Source, source)
		}
	}
}

func TestValidateAcceptsMonitorEventsAndPreservesBytes(t *testing.T) {
	body := []byte(`{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"events","source":"monitor","timestamp":"2026-09-05T08:00:00Z","payload":{"event_schema_version":1}}`)
	message, err := Validate(body)
	if err != nil {
		t.Fatal(err)
	}
	if message.Type != "events" || message.Source != "monitor" || string(message.Body) != string(body) {
		t.Fatalf("unexpected message: %+v", message)
	}
	unsupported := []byte(strings.Replace(string(body), `"source":"monitor"`, `"source":"backend"`, 1))
	if _, err := Validate(unsupported); err == nil {
		t.Fatal("events/backend was accepted")
	}
}
