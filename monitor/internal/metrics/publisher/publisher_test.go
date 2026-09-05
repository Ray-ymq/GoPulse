package publisher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
)

func TestHTTPPublisherUsesMessageContract(t *testing.T) {
	token := "01234567890123456789012345678901"
	seen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/messages" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Idempotency-Key") != "0123456789abcdef0123456789abcdef" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s %#v", r.Method, r.URL.Path, r.Header)
		}
		seen <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client, err := NewHTTP(server.URL, token, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	message := envelope.Envelope{SchemaVersion: 1, MessageID: "0123456789abcdef0123456789abcdef", Type: "metrics", Source: "redis", Timestamp: time.Now().UTC()}
	if err = client.Publish(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("request not received")
	}
}

func TestHTTPPublisherRequires202(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client, err := NewHTTP(server.URL, "01234567890123456789012345678901", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Publish(context.Background(), envelope.Envelope{MessageID: "0123456789abcdef0123456789abcdef"}); err == nil {
		t.Fatal("non-202 response was accepted")
	}
}

func TestHTTPPublisherClassifiesPermanentAndTemporaryRejections(t *testing.T) {
	for _, tc := range []struct {
		status    int
		permanent bool
	}{{http.StatusUnprocessableEntity, true}, {http.StatusTooManyRequests, false}, {http.StatusServiceUnavailable, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
		client, err := NewHTTP(server.URL, "01234567890123456789012345678901", time.Second)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		err = client.Publish(context.Background(), envelope.Envelope{MessageID: "0123456789abcdef0123456789abcdef"})
		server.Close()
		classified, ok := err.(interface{ Permanent() bool })
		if !ok || classified.Permanent() != tc.permanent {
			t.Fatalf("status=%d error=%v permanent=%v", tc.status, err, ok && classified.Permanent())
		}
	}
}
