package logs

import (
	"encoding/json"
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
