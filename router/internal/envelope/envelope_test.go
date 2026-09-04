package envelope

import (
	"errors"
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
		"non object": `[]`,
		"duplicate":  `{"schema_version":1,"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`,
		"unknown":    `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{},"topic":"other"}`,
		"trailing":   validBody + ` {}`,
		"message id": `{"schema_version":1,"message_id":"ABC","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":{}}`,
		"timestamp":  `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56+08:00","payload":{}}`,
		"payload":    `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"redis","timestamp":"2026-09-03T12:34:56Z","payload":null}`,
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
