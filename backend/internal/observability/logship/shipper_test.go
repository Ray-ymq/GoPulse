package logship

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestShipperRetriesSameMessageWithoutChangingBody(t *testing.T) {
	var mu sync.Mutex
	var ids []string
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		ids = append(ids, r.Header.Get("Idempotency-Key"))
		bodies = append(bodies, body)
		attempt := len(ids)
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	shipper, err := New(Config{Endpoint: server.URL, Token: "01234567890123456789012345678901", RequestTimeout: time.Second, QueueCapacity: 2, RetryMin: time.Millisecond, RetryMax: time.Millisecond, ShutdownTimeout: time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"service":"backend"}`)
	if !shipper.Enqueue(original) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shipper.Close(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || len(ids[0]) != 32 || ids[0] != ids[1] {
		t.Fatalf("ids = %#v", ids)
	}
	if !bytes.Equal(bodies[0], original) || !bytes.Equal(bodies[1], original) {
		t.Fatalf("bodies changed: %q", bodies)
	}
}
