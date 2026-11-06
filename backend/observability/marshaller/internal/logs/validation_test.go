package logs

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewEnvelopeUsesLogBatchPayload(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	body := []byte(`{"log_schema_version":1,"timestamp":"2026-10-17T08:00:00Z","level":"info","service":"backend","module":"http","message":"http request completed","method":"GET"}`)
	validated, err := Validate(body, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := NewEnvelope("0123456789abcdef0123456789abcdef", validated)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 2 {
		t.Fatalf("schema version=%d", envelope.SchemaVersion)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 3 || payload["dataid"] == nil || payload["time"] == nil || payload["data"] == nil {
		t.Fatalf("unexpected payload=%s", envelope.Payload)
	}
}
