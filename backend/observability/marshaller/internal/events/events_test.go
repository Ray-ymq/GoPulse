package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/retention"
)

func eventEnvelope() envelope.Envelope {
	payload := json.RawMessage(`{"event_schema_version":1,"event_name":"exporter_plugin_started","source":"monitor","severity":"info","timestamp":"2026-09-05T08:00:00.123Z","message":"exporter plugin started","metadata":{"plugin_id":"redis-exporter","plugin_version":"1.7.1","operation":"start","from_state":"stopped","to_state":"running"}}`)
	return envelope.Envelope{SchemaVersion: 1, MessageID: "0123456789abcdef0123456789abcdef", Type: "events", Source: "monitor", Timestamp: time.Date(2026, 9, 5, 8, 0, 0, 123000000, time.UTC), TimestampText: "2026-09-05T08:00:00.123Z", RawPayload: payload}
}

func TestTransformerProducesStrictEventWriteRequest(t *testing.T) {
	body, err := (Transformer{MaxBytes: 16 * 1024}).Transform(eventEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	var request WriteRequest
	if json.Unmarshal(body, &request) != nil || request.MessageID != "0123456789abcdef0123456789abcdef" || request.IndexDate != "2026.09.05" {
		t.Fatalf("unexpected request: %s", body)
	}
	var document map[string]any
	if json.Unmarshal(request.Document, &document) != nil || document["@timestamp"] != "2026-09-05T08:00:00.123Z" || len(document) != 7 {
		t.Fatalf("unexpected document: %s", request.Document)
	}
	if _, exists := document["timestamp"]; exists {
		t.Fatal("payload timestamp was not renamed")
	}
}

func TestTransformerRejectsUnknownDuplicateAndMismatchedPayload(t *testing.T) {
	base := eventEnvelope()
	cases := []json.RawMessage{
		json.RawMessage(strings.Replace(string(base.RawPayload), `"message":"exporter plugin started"`, `"message":"free text"`, 1)),
		json.RawMessage(strings.Replace(string(base.RawPayload), `"source":"monitor"`, `"source":"monitor","source":"monitor"`, 1)),
		json.RawMessage(strings.Replace(string(base.RawPayload), `"metadata":{`, `"unknown":true,"metadata":{`, 1)),
	}
	for _, payload := range cases {
		message := base
		message.RawPayload = payload
		if _, err := (Transformer{MaxBytes: 16 * 1024}).Transform(message); envelope.Code(err) != "invalid_event_payload" {
			t.Fatalf("payload=%s error=%v", payload, err)
		}
	}
}

func TestTransformerAppliesUTCRetentionBeforeBuildingWrite(t *testing.T) {
	message := eventEnvelope()
	message.Timestamp = time.Date(2026, 9, 23, 8, 0, 0, 123000000, time.UTC)
	message.RawPayload = json.RawMessage(strings.Replace(string(message.RawPayload), "2026-09-05", "2026-09-23", 1))
	policy := retention.DefaultConfig().Events
	transformer := Transformer{Retention: &policy, Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }}
	if _, err := transformer.Transform(message); envelope.Code(err) != "expired_event_retention" {
		t.Fatalf("expired error=%v code=%q", err, envelope.Code(err))
	}
	message.Timestamp = time.Date(2026, 9, 24, 8, 0, 0, 123000000, time.UTC)
	message.RawPayload = json.RawMessage(strings.Replace(string(message.RawPayload), "2026-09-23", "2026-09-24", 1))
	if _, err := transformer.Transform(message); err != nil {
		t.Fatalf("cutoff date rejected: %v", err)
	}
}
