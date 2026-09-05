package metricquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stubUpstream struct {
	body  []byte
	err   error
	calls int
	query string
}

func (s *stubUpstream) QueryRange(_ context.Context, query string, _, _ time.Time, _ time.Duration) ([]byte, error) {
	s.calls++
	s.query = query
	return s.body, s.err
}

func TestParseOptionsAndServiceValidateResponse(t *testing.T) {
	options, err := ParseOptions(url.Values{"metric": {"gopulse_redis_cpu_seconds_total"}, "range": {"15m"}})
	if err != nil {
		t.Fatal(err)
	}
	upstream := &stubUpstream{body: []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_cpu_seconds_total","source":"redis","target_id":"redis-exporter-local","mode":"user"},"values":[[1788595200,"12.5"]]}]}}`)}
	service := NewService(upstream)
	service.now = func() time.Time { return time.Date(2026, 9, 5, 8, 15, 0, 0, time.UTC) }
	result, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metric != options.Definition.Metric || len(result.Series) != 1 || result.Series[0].Labels.Mode != "user" || result.Series[0].Points[0].Value != 12.5 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if upstream.query != `gopulse_redis_cpu_seconds_total{source="redis",target_id="redis-exporter-local"}` {
		t.Fatalf("query=%q", upstream.query)
	}
}

func TestRejectsUntrustedResponse(t *testing.T) {
	options, _ := ParseOptions(url.Values{"metric": {"gopulse_redis_up"}})
	upstream := &stubUpstream{body: []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_up","source":"redis","target_id":"other"},"values":[[1,"1"]]}]}}`)}
	_, err := NewService(upstream).Query(context.Background(), options)
	if err == nil {
		t.Fatal("expected unavailable error")
	}
}

func TestClientUsesPostBasicAuthAndNoRedirect(t *testing.T) {
	var method, username, password, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		username, password, _ = r.BasicAuth()
		_ = r.ParseForm()
		query = r.Form.Get("query")
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "reader", strings.Repeat("p", 32), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.QueryRange(context.Background(), "fixed", time.Now(), time.Now(), time.Minute); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if method != http.MethodPost || username != "reader" || password != strings.Repeat("p", 32) || query != "fixed" {
		t.Fatalf("request method=%s user=%s query=%s", method, username, query)
	}
}

func TestClientRejectsOversizedResponseHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Oversized", strings.Repeat("x", 128<<10))
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "reader", strings.Repeat("p", 32), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.QueryRange(context.Background(), "fixed", time.Now().Add(-time.Minute), time.Now(), time.Minute); err == nil {
		t.Fatal("expected oversized response header rejection")
	}
}
