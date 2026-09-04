package logs

import (
	"fmt"
	"testing"
	"time"
)

func TestValidateBuildsEnvelopeForEveryApplicationSource(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	cases := []struct{ service, module, message, fields string }{
		{"backend", "lifecycle", "backend listening", ""},
		{"business-worker", "worker", "event processed", `,"event_id":"123e4567-e89b-12d3-a456-426614174000","event_type":"comment.created","attempt":0,"reason":"processed"`},
		{"search-indexer", "search", "event processed", `,"event_id":"123e4567-e89b-12d3-a456-426614174001","event_type":"post.created","post_id":7,"attempt":0,"reason":"processed"`},
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
		})
	}
}

func TestValidateRejectsMessageFromAnotherService(t *testing.T) {
	body := []byte(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"business-worker","module":"lifecycle","message":"backend listening"}`)
	if _, err := Validate(body, time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC), time.Minute); err == nil {
		t.Fatal("Validate() error = nil")
	}
}
