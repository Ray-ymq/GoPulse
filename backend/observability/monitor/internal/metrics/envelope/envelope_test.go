package envelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewSourceUsesContractPayloadWithoutProducerIdentity(t *testing.T) {
	at := time.Date(2026, 10, 17, 8, 0, 0, 123456789, time.UTC)
	message, err := NewSource("redis", []Sample{{Name: "gopulse_redis_up", Kind: "gauge", Labels: map[string]string{}, Value: 1}}, at)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var outer struct {
		SchemaVersion int             `json:"schema_version"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		t.Fatal(err)
	}
	if outer.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version=%d", outer.SchemaVersion)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(outer.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatalf("payload fields=%v", fields)
	}
	for _, key := range []string{"dataid", "time", "timestamp", "data"} {
		if fields[key] == nil {
			t.Fatalf("payload field %q missing", key)
		}
	}
	for _, key := range []string{"producer_kind", "producer_id", "target_id", "samples"} {
		if fields[key] != nil {
			t.Fatalf("legacy identity field %q leaked into payload", key)
		}
	}
}
