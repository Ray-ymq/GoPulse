package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/collector"
)

type fakeCollector struct {
	calls    int
	snapshot collector.Snapshot
	err      error
}

func (f *fakeCollector) Collect(context.Context) (collector.Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestHealthDoesNotCollect(t *testing.T) {
	source := &fakeCollector{}
	handler := New(source, time.Second, nil)
	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
		if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok","service":"redis-exporter"}` || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("response = %#v body=%q", response.Result(), response.Body.String())
		}
	}
	if source.calls != 0 {
		t.Fatalf("health triggered %d collections", source.calls)
	}
}

func TestMetricsSuccessEncodesFixedFamilies(t *testing.T) {
	source := &fakeCollector{snapshot: collector.Snapshot{UptimeSeconds: 5, ConnectedClients: 2, UsedMemoryBytes: 42, CommandsProcessedTotal: 11, KeyspaceHitsTotal: 7, KeyspaceMissesTotal: 3, CPUUserSeconds: 1.5, CPUSystemSeconds: .5, DB: 2, DBKeys: 4, DBExpiringKeys: 1}}
	response := httptest.NewRecorder()
	New(source, time.Second, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || source.calls != 1 || response.Header().Get("Content-Type") != metricsContentType || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d calls=%d headers=%v", response.Code, source.calls, response.Header())
	}
	for _, sample := range []string{"gopulse_redis_up 1", "gopulse_redis_commands_processed_total 11", `gopulse_redis_cpu_seconds_total{mode="user"} 1.5`, `gopulse_redis_cpu_seconds_total{mode="system"} 0.5`, `gopulse_redis_db_keys{db="2"} 4`} {
		if !strings.Contains(body, sample) {
			t.Fatalf("missing %q in:\n%s", sample, body)
		}
	}
}

func TestMetricsFailureReturnsOnlyUpZeroAndCanRecover(t *testing.T) {
	source := &fakeCollector{err: &collector.Failure{Reason: collector.ReasonRedisUnavailable}}
	handler := New(source, time.Second, nil)
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if failed.Code != http.StatusServiceUnavailable || !strings.Contains(failed.Body.String(), "gopulse_redis_up 0") || strings.Contains(failed.Body.String(), "uptime_seconds") {
		t.Fatalf("failed response: %d %s", failed.Code, failed.Body.String())
	}
	source.err = nil
	source.snapshot = collector.Snapshot{DB: 0, UptimeSeconds: 99}
	recovered := httptest.NewRecorder()
	handler.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), "gopulse_redis_uptime_seconds 99") {
		t.Fatalf("recovered response: %d %s", recovered.Code, recovered.Body.String())
	}
}

func TestRequestBoundaries(t *testing.T) {
	handler := New(&fakeCollector{err: errors.New("unused")}, time.Second, nil)
	for _, test := range []struct {
		method, target, body string
		status               int
	}{
		{http.MethodPost, "/health", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/metrics?target=x", "", http.StatusBadRequest},
		{http.MethodGet, "/health", "body", http.StatusBadRequest},
		{http.MethodGet, "/unknown", "", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.target, strings.NewReader(test.body)))
		if response.Code != test.status {
			t.Fatalf("%s %s status=%d want=%d", test.method, test.target, response.Code, test.status)
		}
	}
}
