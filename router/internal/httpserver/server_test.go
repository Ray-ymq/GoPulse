package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/router/internal/config"
)

const (
	testToken = "0123456789abcdef0123456789abcdef"
	testBody  = `{ "schema_version":1, "message_id":"0123456789abcdef0123456789abcdef", "type":"metrics", "source":"redis", "timestamp":"2026-09-03T12:34:56Z", "payload":{"samples":[]} }`
)

type fakeProducer struct {
	mu                   sync.Mutex
	calls                int
	topic, key           string
	value                []byte
	produceErr, readyErr error
	block                bool
	started              chan struct{}
}

func (p *fakeProducer) Produce(ctx context.Context, topic, key string, value []byte) error {
	p.mu.Lock()
	p.calls++
	p.topic, p.key, p.value = topic, key, append([]byte(nil), value...)
	block, err := p.block, p.produceErr
	p.mu.Unlock()
	if block {
		if p.started != nil {
			select {
			case p.started <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}
func (p *fakeProducer) Ready(context.Context, string) error { return p.readyErr }
func (p *fakeProducer) count() int                          { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }

func testServer(producer Producer, timeout time.Duration, maxBody int64) *Server {
	cfg := config.Config{HTTPHost: "127.0.0.1", HTTPPort: 9091, APIToken: testToken, RequestTimeout: timeout, MaxMessageBytes: maxBody}
	return New(cfg, producer, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func request(t *testing.T, handler http.Handler, method, path, body, token string, headers map[string][]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestPublishProducesOriginalBytesAfterAcknowledgement(t *testing.T) {
	producer := &fakeProducer{}
	server := testServer(producer, time.Second, 1<<20)
	response := request(t, server.Handler(), http.MethodPost, "/internal/v1/messages", testBody, testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}})
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if producer.topic != config.Topic || producer.key != "0123456789abcdef0123456789abcdef" || string(producer.value) != testBody {
		t.Fatalf("unexpected produced record: topic=%q key=%q value=%q", producer.topic, producer.key, producer.value)
	}
}

func TestPublishRejectsInvalidRequestsWithoutProducing(t *testing.T) {
	cases := []struct {
		name, body, token string
		headers           map[string][]string
		want              int
	}{
		{"authentication", testBody, "wrong-token-0123456789abcdef012345", map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}}, http.StatusUnauthorized},
		{"duplicate JSON", strings.Replace(testBody, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1), testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}}, http.StatusBadRequest},
		{"idempotency mismatch", testBody, testToken, map[string][]string{"Idempotency-Key": {"ffffffffffffffffffffffffffffffff"}}, http.StatusBadRequest},
		{"duplicate idempotency", testBody, testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef"}}, http.StatusBadRequest},
		{"unsupported", strings.Replace(testBody, `"type":"metrics"`, `"type":"logs"`, 1), testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}}, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			producer := &fakeProducer{}
			server := testServer(producer, time.Second, 1<<20)
			response := request(t, server.Handler(), http.MethodPost, "/internal/v1/messages", tc.body, tc.token, tc.headers)
			if response.Code != tc.want || producer.count() != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, producer.count(), response.Body.String())
			}
		})
	}
}

func TestPublishHandlesSizeFailureAndBlockedProducer(t *testing.T) {
	producer := &fakeProducer{}
	server := testServer(producer, 50*time.Millisecond, int64(len(testBody)-1))
	response := request(t, server.Handler(), http.MethodPost, "/internal/v1/messages", testBody, testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}})
	if response.Code != http.StatusRequestEntityTooLarge || producer.count() != 0 {
		t.Fatalf("oversize status=%d calls=%d", response.Code, producer.count())
	}

	producer = &fakeProducer{block: true}
	server = testServer(producer, 50*time.Millisecond, 1<<20)
	started := time.Now()
	response = request(t, server.Handler(), http.MethodPost, "/internal/v1/messages", testBody, testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}})
	if response.Code != http.StatusServiceUnavailable || time.Since(started) > time.Second {
		t.Fatalf("blocked producer status=%d duration=%s", response.Code, time.Since(started))
	}
}

func TestHealthAndReadyAreSeparated(t *testing.T) {
	producer := &fakeProducer{readyErr: errors.New("broker unavailable")}
	server := testServer(producer, time.Second, 1<<20)
	if response := request(t, server.Handler(), http.MethodGet, "/health", "", "", nil); response.Code != http.StatusOK {
		t.Fatalf("health status = %d", response.Code)
	}
	if response := request(t, server.Handler(), http.MethodGet, "/ready", "", testToken, nil); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d", response.Code)
	}
	producer.readyErr = nil
	if response := request(t, server.Handler(), http.MethodGet, "/ready", "", testToken, nil); response.Code != http.StatusOK {
		t.Fatalf("recovered ready status = %d", response.Code)
	}
}

func TestPublishRejectsBrowserCredentialsAndInvalidHeaders(t *testing.T) {
	producer := &fakeProducer{}
	server := testServer(producer, time.Second, 1<<20)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/messages?token="+testToken, strings.NewReader(testBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "0123456789abcdef0123456789abcdef")
	req.AddCookie(&http.Cookie{Name: "gopulse_session", Value: "admin-cookie"})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized || producer.count() != 0 {
		t.Fatalf("browser credential status=%d calls=%d", response.Code, producer.count())
	}

	duplicateType := httptest.NewRequest(http.MethodPost, "/internal/v1/messages", strings.NewReader(testBody))
	duplicateType.Header.Add("Authorization", "Bearer "+testToken)
	duplicateType.Header.Add("Content-Type", "application/json")
	duplicateType.Header.Add("Content-Type", "text/plain")
	duplicateType.Header.Add("Idempotency-Key", "0123456789abcdef0123456789abcdef")
	duplicateResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(duplicateResponse, duplicateType)
	if duplicateResponse.Code != http.StatusBadRequest {
		t.Fatalf("duplicate Content-Type status=%d", duplicateResponse.Code)
	}

	for name, header := range map[string]string{"content type": "text/plain", "content encoding": "gzip"} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/messages", strings.NewReader(testBody))
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "0123456789abcdef0123456789abcdef")
			if name == "content type" {
				req.Header.Set("Content-Type", header)
			} else {
				req.Header.Set("Content-Encoding", header)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
}

func TestProducerFailureAndHTTPShutdownAreBounded(t *testing.T) {
	producer := &fakeProducer{produceErr: errors.New("produce failed")}
	server := testServer(producer, 100*time.Millisecond, 1<<20)
	response := request(t, server.Handler(), http.MethodPost, "/internal/v1/messages", testBody, testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("producer failure status=%d", response.Code)
	}

	producer = &fakeProducer{block: true, started: make(chan struct{}, 1)}
	server = testServer(producer, 50*time.Millisecond, 1<<20)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	req, _ := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/internal/v1/messages", strings.NewReader(testBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "0123456789abcdef0123456789abcdef")
	requestDone := make(chan struct{})
	go func() {
		response, _ := http.DefaultClient.Do(req)
		if response != nil {
			response.Body.Close()
		}
		close(requestDone)
	}()
	select {
	case <-producer.started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach producer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error=%v", err)
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("request did not finish during shutdown")
	}
	select {
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve() error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestPublishRoutesMonitorEventsWithoutChangingBytes(t *testing.T) {
	body := `{"schema_version":1,"message_id":"0123456789abcdef0123456789abcdef","type":"events","source":"monitor","timestamp":"2026-09-05T08:00:00Z","payload":{"event_schema_version":1}}`
	producer := &fakeProducer{}
	server := testServer(producer, time.Second, 1<<20)
	response := request(t, server.Handler(), http.MethodPost, "/internal/v1/messages", body, testToken, map[string][]string{"Idempotency-Key": {"0123456789abcdef0123456789abcdef"}})
	if response.Code != http.StatusAccepted || producer.topic != config.Topic || producer.key != "0123456789abcdef0123456789abcdef" || string(producer.value) != body {
		t.Fatalf("status=%d topic=%q key=%q value=%q", response.Code, producer.topic, producer.key, producer.value)
	}
}
