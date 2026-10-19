package logs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestValidateBuildsEnvelopeForEveryApplicationSource(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	cases := []struct{ service, module, message, fields string }{
		{"backend", "lifecycle", "backend listening", `,"instance_id":"backend-1"`},
		{"business-worker", "worker", "event processed", `,"event_id":"123e4567-e89b-12d3-a456-426614174000","command_id":"123e4567-e89b-42d3-a456-426614174001","data_epoch":"123e4567-e89b-42d3-a456-426614174000","event_type":"comment.created","consumer":"business","attempt":0,"reason":"processed"`},
		{"search-indexer", "search", "event processed", `,"event_id":"123e4567-e89b-12d3-a456-426614174001","command_id":"123e4567-e89b-42d3-a456-426614174001","data_epoch":"123e4567-e89b-42d3-a456-426614174000","event_type":"post.created","projection_generation":"gopulse-post-search-v1-20261012","routing_contract_revision":"post-created-search/1","consumer":"search","post_id":7,"attempt":0,"reason":"processed"`},
		{"search-reindex", "search", "search reindex completed", `,"result":"completed","batch_size":500,"document_count":7`},
	}
	for _, test := range cases {
		t.Run(test.service, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":%q,"module":%q,"message":%q%s}`, test.service, test.module, test.message, test.fields))
			validated, err := Validate(body, now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := NewEnvelope("0123456789abcdef0123456789abcdef", validated)
			if err != nil {
				t.Fatal(err)
			}
			if envelope.Source != test.service || validated.Source != test.service {
				t.Fatalf("source envelope=%q validated=%q", envelope.Source, validated.Source)
			}
			wireBody, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				SchemaVersion int             `json:"schema_version"`
				Payload       json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(wireBody, &wire); err != nil || wire.SchemaVersion != 2 {
				t.Fatalf("invalid log envelope: %s %v", wireBody, err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(wire.Payload, &fields); err != nil || len(fields) != 3 || fields["dataid"] == nil || fields["time"] == nil || fields["data"] == nil {
				t.Fatalf("invalid log batch: %s %v", wire.Payload, err)
			}
		})
	}
}

func TestValidateRejectsInvalidInstanceIdentity(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	for _, instanceID := range []string{"Backend-1", "backend_1", "backend-"} {
		t.Run(instanceID, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"backend","module":"lifecycle","message":"backend listening","instance_id":%q}`, instanceID))
			if _, err := Validate(body, now, time.Minute); err == nil {
				t.Fatalf("Validate(%q) error = nil", instanceID)
			}
		})
	}
}

func TestValidateRejectsMessageFromAnotherService(t *testing.T) {
	body := []byte(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"business-worker","module":"lifecycle","message":"backend listening"}`)
	if _, err := Validate(body, time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC), time.Minute); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestValidateAcceptsReplicaInstanceIdentity(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"backend","module":"lifecycle","message":"backend listening","instance_id":"backend-2"}`)
	if _, err := Validate(body, now, time.Minute); err != nil {
		t.Fatal(err)
	}

	bad := []byte(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"backend","module":"lifecycle","message":"backend listening","instance_id":"backend/2"}`)
	if _, err := Validate(bad, now, time.Minute); err == nil {
		t.Fatal("Validate() accepted an unsafe instance identity")
	}
}

func TestValidateAcceptsBoundedTraceFieldsAndRejectsInvalidIDs(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	valid := []byte(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"backend","module":"http","message":"http request completed","trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"0102030405060708"}`)
	if _, err := Validate(valid, now, time.Minute); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, field := range []string{"trace_id", "span_id"} {
		body := string(valid)
		if field == "trace_id" {
			body = strings.Replace(body, "0102030405060708090a0b0c0d0e0f10", "00000000000000000000000000000000", 1)
		} else {
			body = strings.Replace(body, `"span_id":"0102030405060708"`, `"span_id":"0000000000000000"`, 1)
		}
		if _, err := Validate([]byte(body), now, time.Minute); err == nil {
			t.Fatalf("Validate() accepted zero %s", field)
		}
	}
}
