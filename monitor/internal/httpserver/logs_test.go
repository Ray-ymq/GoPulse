package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type capturedLogPublisher struct {
	calls   int
	id      string
	message any
	err     error
}

func (p *capturedLogPublisher) PublishRaw(_ context.Context, id string, message any) error {
	p.calls++
	p.id = id
	p.message = message
	return p.err
}

func TestLogIngestUsesDedicatedTokenAndPublishesValidatedEnvelope(t *testing.T) {
	publisher := &capturedLogPublisher{}
	server := New("admin-token-012345678901234567890", t.TempDir(), &fakeManager{}, nil, LogOptions{Token: "ingest-token-01234567890123456789", MaxBytes: 65536, FutureSkew: 5 * time.Minute, Publisher: publisher, Now: func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }})
	body := `{"log_schema_version":1,"timestamp":"2026-09-04T11:59:59Z","level":"info","service":"backend","module":"http","message":"http request completed","request_id":"0123456789abcdef0123456789abcdef","method":"GET","route":"/api/v1/posts","status":200,"duration_ms":1,"response_bytes":2}`
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/logs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer ingest-token-01234567890123456789")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "abcdef0123456789abcdef0123456789")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusAccepted || publisher.calls != 1 || publisher.id != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("status=%d calls=%d id=%q body=%s", response.Code, publisher.calls, publisher.id, response.Body.String())
	}
	encoded, _ := json.Marshal(publisher.message)
	if !strings.Contains(string(encoded), `"type":"logs"`) || !strings.Contains(string(encoded), `"source":"backend"`) {
		t.Fatalf("envelope=%s", encoded)
	}

	bad := httptest.NewRequest(http.MethodPost, "/internal/v1/logs", strings.NewReader(body))
	bad.Header.Set("Authorization", "Bearer admin-token-012345678901234567890")
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("Idempotency-Key", "abcdef0123456789abcdef0123456789")
	badResponse := httptest.NewRecorder()
	server.ServeHTTP(badResponse, bad)
	if badResponse.Code != http.StatusUnauthorized || publisher.calls != 1 {
		t.Fatalf("admin token status=%d calls=%d", badResponse.Code, publisher.calls)
	}
}
