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

func TestRuntimeUsesStdoutOnlyWhenDisabledAndDrainsWhenEnabled(t *testing.T) {
	var stdout bytes.Buffer
	disabled, err := Open("business-worker", &stdout, Config{})
	if err != nil {
		t.Fatal(err)
	}
	disabled.Logger.Info("business worker started")
	if err := disabled.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"service":"business-worker"`)) {
		t.Fatalf("stdout = %q", stdout.String())
	}

	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	stdout.Reset()
	enabled, err := Open("search-indexer", &stdout, Config{
		Endpoint: server.URL, Token: "01234567890123456789012345678901",
		RequestTimeout: time.Second, QueueCapacity: 2, RetryMin: time.Millisecond,
		RetryMax: time.Millisecond, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	enabled.Logger.Info("search indexer started")
	if err := enabled.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if !bytes.Equal(body, stdout.Bytes()) {
			t.Fatalf("remote body = %q, stdout = %q", body, stdout.Bytes())
		}
	default:
		t.Fatal("remote log was not delivered")
	}
}

func TestShipperDropsPermanentRejectionAndContinues(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		attempt := len(bodies)
		mu.Unlock()
		if attempt == 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	shipper, err := New(Config{
		Endpoint: server.URL, Token: "01234567890123456789012345678901",
		RequestTimeout: time.Second, QueueCapacity: 2, RetryMin: time.Millisecond,
		RetryMax: time.Millisecond, ShutdownTimeout: time.Second,
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	first, second := []byte(`{"sequence":1}`), []byte(`{"sequence":2}`)
	if !shipper.Enqueue(first) || !shipper.Enqueue(second) {
		t.Fatal("enqueue failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shipper.Close(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || !bytes.Equal(bodies[0], first) || !bytes.Equal(bodies[1], second) {
		t.Fatalf("bodies = %q", bodies)
	}
}

func TestShipperShutdownCancelsRetryingDelivery(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	shipper, err := New(Config{
		Endpoint: server.URL, Token: "01234567890123456789012345678901",
		RequestTimeout: time.Second, QueueCapacity: 1, RetryMin: time.Second,
		RetryMax: time.Second, ShutdownTimeout: 20 * time.Millisecond,
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !shipper.Enqueue([]byte(`{"message":"retry"}`)) {
		t.Fatal("enqueue failed")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := shipper.Close(ctx); err == nil {
		t.Fatal("Close() error = nil, want timeout")
	}
}
