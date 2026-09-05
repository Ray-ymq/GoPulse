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

func TestShipperCloseLinearizesWithAcceptedEnqueue(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	shipper, err := New(Config{
		Endpoint: server.URL, Token: "01234567890123456789012345678901",
		RequestTimeout: time.Second, QueueCapacity: 1, RetryMin: time.Millisecond,
		RetryMax: time.Millisecond, ShutdownTimeout: time.Second,
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	shipper.beforeQueue = func() {
		once.Do(func() { close(entered); <-release })
	}
	accepted := make(chan bool, 1)
	go func() { accepted <- shipper.Enqueue([]byte(`{"message":"accepted-before-close"}`)) }()
	<-entered
	closed := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		closed <- shipper.Close(ctx)
	}()
	close(release)
	if !<-accepted {
		t.Fatal("enqueue before the close linearization point was rejected")
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if string(body) != `{"message":"accepted-before-close"}` {
			t.Fatalf("body = %s", body)
		}
	default:
		t.Fatal("accepted entry was left without a consumer")
	}
	if shipper.Enqueue([]byte(`{"message":"after-close"}`)) {
		t.Fatal("enqueue after close was accepted")
	}
}

func TestShipperThrottlesQueueFullUntilQueueRecovers(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	var output bytes.Buffer
	shipper, err := New(Config{
		Endpoint: server.URL, Token: "01234567890123456789012345678901",
		RequestTimeout: time.Second, QueueCapacity: 1, RetryMin: time.Millisecond,
		RetryMax: time.Millisecond, ShutdownTimeout: time.Second,
	}, slog.New(slog.NewJSONHandler(&output, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !shipper.Enqueue([]byte(`{"sequence":1}`)) {
		t.Fatal("first enqueue failed")
	}
	<-started
	if !shipper.Enqueue([]byte(`{"sequence":2}`)) {
		t.Fatal("second enqueue failed")
	}
	for range 10 {
		if shipper.Enqueue([]byte(`{"sequence":3}`)) {
			t.Fatal("enqueue unexpectedly succeeded while queue was full")
		}
	}
	if got := bytes.Count(output.Bytes(), []byte(`"reason":"queue_full"`)); got != 1 {
		t.Fatalf("queue-full log count = %d, want 1; output=%s", got, output.String())
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for !shipper.Enqueue([]byte(`{"sequence":4}`)) {
		if time.Now().After(deadline) {
			t.Fatal("queue did not recover")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shipper.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(output.Bytes(), []byte(`"reason":"queue_available"`)); got != 1 {
		t.Fatalf("queue recovery log count = %d, want 1; output=%s", got, output.String())
	}
}

func TestJitterDelayUsesInjectedEntropyWithinBounds(t *testing.T) {
	zero := bytes.NewReader(make([]byte, 8))
	if got := jitterDelay(100*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond, zero); got != 80*time.Millisecond {
		t.Fatalf("zero entropy delay = %s, want 80ms", got)
	}
	ones := bytes.NewReader(bytes.Repeat([]byte{0xff}, 8))
	got := jitterDelay(100*time.Millisecond, 50*time.Millisecond, 200*time.Millisecond, ones)
	if got < 80*time.Millisecond || got > 120*time.Millisecond {
		t.Fatalf("jittered delay = %s, outside [80ms,120ms]", got)
	}
	if got := jitterDelay(50*time.Millisecond, 50*time.Millisecond, 50*time.Millisecond, zero); got != 50*time.Millisecond {
		t.Fatalf("fixed delay = %s, want 50ms", got)
	}
}
