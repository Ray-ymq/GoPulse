package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
)

func TestDiscardPublisherDoesNotRequireExternalTransport(t *testing.T) {
	var transport Transport = Discard{}
	message := envelope.Envelope{MessageID: "0123456789abcdef0123456789abcdef"}
	if err := transport.Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := transport.PublishRaw(context.Background(), message.MessageID, message); err != nil {
		t.Fatal(err)
	}
}

func TestMessageFromRawPreservesValidatedWireBody(t *testing.T) {
	body := json.RawMessage(`{"schema_version":2,"message_id":"0123456789abcdef0123456789abcdef","type":"logs","source":"backend","timestamp":"2026-10-17T00:00:00Z","payload":{"message":"keep exact bytes"}}`)
	message, err := MessageFromRaw("0123456789abcdef0123456789abcdef", body)
	if err != nil {
		t.Fatal(err)
	}
	if string(message.Body) != string(body) || message.Type != "logs" {
		t.Fatalf("message=%+v", message)
	}
	if _, err := MessageFromRaw("0123456789abcdef0123456789abcdef", json.RawMessage(`{"schema_version":2,"message_id":"0123456789abcdef0123456789abcdef","type":"metrics","source":"monitor","timestamp":"2026-10-17T00:00:00Z","payload":{}}`)); err == nil {
		t.Fatal("metrics raw message unexpectedly accepted by log/event transport")
	} else {
		var permanent *PermanentError
		if !errors.As(err, &permanent) || permanent.Code != "unsupported_envelope" {
			t.Fatalf("error=%v", err)
		}
	}
}
