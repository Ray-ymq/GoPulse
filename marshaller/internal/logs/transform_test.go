package logs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

func TestTransformerBuildsStrictIdempotentWriteRequest(t *testing.T) {
	payload := json.RawMessage(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"backend","module":"post","message":"post created","request_id":"0123456789abcdef0123456789abcdef","user_id":7,"post_id":9}`)
	message := envelope.Envelope{MessageID: "abcdef0123456789abcdef0123456789", Type: "logs", Source: "backend", Timestamp: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC), TimestampText: "2026-09-04T12:00:00Z", RawPayload: payload}
	body, err := (Transformer{MaxBytes: 1 << 20}).Transform(message)
	if err != nil {
		t.Fatal(err)
	}
	var request WriteRequest
	if json.Unmarshal(body, &request) != nil {
		t.Fatalf("body=%s", body)
	}
	if request.MessageID != message.MessageID || request.IndexDate != "2026.09.04" || !strings.Contains(string(request.Document), `"@timestamp":"2026-09-04T12:00:00Z"`) || strings.Contains(string(request.Document), `"message_id"`) {
		t.Fatalf("request=%+v document=%s", request, request.Document)
	}
}

func TestTransformerRejectsUnknownOrSensitiveLog(t *testing.T) {
	payload := json.RawMessage(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":"backend","module":"post","message":"post created","content":"secret"}`)
	_, err := (Transformer{}).Transform(envelope.Envelope{MessageID: "abcdef0123456789abcdef0123456789", Type: "logs", Source: "backend", Timestamp: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC), RawPayload: payload})
	if envelope.Code(err) != "invalid_log_payload" {
		t.Fatalf("error=%v code=%q", err, envelope.Code(err))
	}
}

func TestTransformAcceptsBackgroundSourcesAndRejectsSourceMismatch(t *testing.T) {
	for _, test := range []struct{ source, module, message, fields string }{
		{"business-worker", "worker", "event processed", `,"event_id":"123e4567-e89b-12d3-a456-426614174000","event_type":"comment.created","attempt":0,"reason":"processed"`},
		{"search-indexer", "search", "event processed", `,"event_id":"123e4567-e89b-12d3-a456-426614174001","event_type":"post.created","post_id":7,"attempt":0,"reason":"processed"`},
		{"search-reindex", "search", "search reindex completed", `,"result":"completed","batch_size":500,"document_count":7`},
	} {
		t.Run(test.source, func(t *testing.T) {
			payload := json.RawMessage(fmt.Sprintf(`{"log_schema_version":1,"timestamp":"2026-09-04T12:00:00Z","level":"info","service":%q,"module":%q,"message":%q%s}`, test.source, test.module, test.message, test.fields))
			message := envelope.Envelope{MessageID: "abcdef0123456789abcdef0123456789", Type: "logs", Source: test.source, Timestamp: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC), RawPayload: payload}
			if _, err := (Transformer{}).Transform(message); err != nil {
				t.Fatal(err)
			}
			message.Source = "backend"
			if _, err := (Transformer{}).Transform(message); envelope.Code(err) != "source_mismatch" {
				t.Fatalf("mismatch error = %v", err)
			}
		})
	}
}
