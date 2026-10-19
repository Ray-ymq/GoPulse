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
	body   []byte
	bodies map[string][]byte
	err    error
	calls  int
	query  string
}

func (s *stubUpstream) QueryRange(_ context.Context, query string, _, _ time.Time, _ time.Duration) ([]byte, error) {
	s.calls++
	s.query = query
	if body, ok := s.bodies[query]; ok {
		return body, s.err
	}
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
	if result.Metric != options.Definition.Metric || len(result.Series) != 1 || result.Series[0].Labels["mode"] != "user" || result.Series[0].Points[0].Value != 12.5 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if upstream.query != `gopulse_redis_cpu_seconds_total{source="redis",target_id="redis-exporter-local"}` {
		t.Fatalf("query=%q", upstream.query)
	}
}

func TestMetricSelectorsAreScopedAndSampleTimeIsReturned(t *testing.T) {
	options, err := ParseOptions(url.Values{"metric": {"gopulse_redis_command_calls_total"}, "command": {"get"}})
	if err != nil {
		t.Fatal(err)
	}
	if options.Labels["command"] != "get" {
		t.Fatalf("selector was not parsed: %#v", options.Labels)
	}
	upstream := &stubUpstream{body: []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_command_calls_total","source":"redis","target_id":"redis-exporter-local","command":"get"},"values":[[1788595200,"12"]]}]}}`)}
	service := NewService(upstream)
	service.now = func() time.Time { return time.Date(2026, 9, 5, 8, 15, 0, 0, time.UTC) }
	result, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if upstream.query != `gopulse_redis_command_calls_total{source="redis",target_id="redis-exporter-local",command="get"}` {
		t.Fatalf("query=%q", upstream.query)
	}
	if result.SampledAt != "2026-09-05T08:00:00Z" {
		t.Fatalf("sampled_at=%q", result.SampledAt)
	}
}

func TestRedisCommandLatencyQuantileQueryPreservesSecondsAndLabels(t *testing.T) {
	options, err := ParseOptions(url.Values{"metric": {"gopulse_redis_command_latency_seconds"}, "command": {"get"}, "quantile": {"0.999"}})
	if err != nil {
		t.Fatal(err)
	}
	if options.Definition.Unit != "seconds" || options.Definition.Kind != "gauge" || len(options.Definition.LabelKeys) != 2 {
		t.Fatalf("unexpected latency definition: %#v", options.Definition)
	}
	upstream := &stubUpstream{body: []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_command_latency_seconds","source":"redis","target_id":"redis-exporter-local","command":"get","quantile":"0.999"},"values":[[1788595200,"0.00000975"]]}]}}`)}
	service := NewService(upstream)
	service.now = func() time.Time { return time.Date(2026, 9, 5, 8, 15, 0, 0, time.UTC) }
	result, err := service.Query(context.Background(), options)
	if err != nil || result.Unit != "seconds" || len(result.Series) != 1 || result.Series[0].Labels["quantile"] != "0.999" || result.Series[0].Points[0].Value != 0.00000975 {
		t.Fatalf("unexpected latency result: %#v, %v", result, err)
	}
	if upstream.query != `gopulse_redis_command_latency_seconds{source="redis",target_id="redis-exporter-local",command="get",quantile="0.999"}` {
		t.Fatalf("query=%q", upstream.query)
	}
}

func TestDerivedRateHandlesCounterReset(t *testing.T) {
	options, err := ParseOptions(url.Values{"metric": {"gopulse_redis_commands_qps"}, "range": {"15m"}})
	if err != nil {
		t.Fatal(err)
	}
	upstream := &stubUpstream{body: []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_commands_processed_total","source":"redis","target_id":"redis-exporter-local"},"values":[[1788595200,"100"],[1788595215,"115"],[1788595230,"4"]]}]}}`)}
	service := NewService(upstream)
	service.now = func() time.Time { return time.Date(2026, 9, 5, 8, 15, 0, 0, time.UTC) }
	result, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.ValueKind != "rate" || result.DisplayUnit != "ops/s" || result.Quality != "complete" || len(result.Series) != 1 || len(result.Series[0].Points) != 2 {
		t.Fatalf("unexpected derived result: %#v", result)
	}
	if result.Series[0].Points[0].Value != 1 || result.Series[0].Points[1].Value != 4.0/15.0 {
		t.Fatalf("counter reset was not handled: %#v", result.Series[0].Points)
	}
}

func TestDerivedRatioSkipsZeroDenominator(t *testing.T) {
	options, err := ParseOptions(url.Values{"metric": {"gopulse_redis_keyspace_hit_rate"}})
	if err != nil {
		t.Fatal(err)
	}
	upstream := &stubUpstream{bodies: map[string][]byte{
		`gopulse_redis_keyspace_hits_total{source="redis",target_id="redis-exporter-local"}`:   []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_keyspace_hits_total","source":"redis","target_id":"redis-exporter-local"},"values":[[1788595200,"5"]]}]}}`),
		`gopulse_redis_keyspace_misses_total{source="redis",target_id="redis-exporter-local"}`: []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"gopulse_redis_keyspace_misses_total","source":"redis","target_id":"redis-exporter-local"},"values":[[1788595200,"0"]]}]}}`),
	}}
	service := NewService(upstream)
	service.now = func() time.Time { return time.Date(2026, 9, 5, 8, 15, 0, 0, time.UTC) }
	result, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Quality != "zero_denominator" || len(result.Series) != 1 || len(result.Series[0].Points) != 0 {
		t.Fatalf("zero denominator was not marked: %#v", result)
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

func TestRedisRejectsAdditionalProducerLabels(t *testing.T) {
	definition := Presets[0]
	labels := map[string]string{"__name__": definition.Metric, "source": "redis", "target_id": "redis-exporter-local"}
	if _, _, err := validateLabels(labels, definition); err != nil {
		t.Fatal("historical provenance rejected", err)
	}
	labels["producer_id"] = "redis-exporter"
	if _, _, err := validateLabels(labels, definition); err == nil {
		t.Fatal("forked producer labels accepted")
	}
}
