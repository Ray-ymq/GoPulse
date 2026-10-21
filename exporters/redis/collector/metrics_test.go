package collector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/config"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type fakeCollector struct {
	calls    int
	snapshot Snapshot
	err      error
}

func uint64Ptr(value uint64) *uint64 { return &value }

func (f *fakeCollector) Collect(context.Context) (Snapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestHealthDoesNotCollect(t *testing.T) {
	source := &fakeCollector{}
	handler := testHandler(source, time.Second, nil)
	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
		if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok","contract_version":"2"}` || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("response = %#v body=%q", response.Result(), response.Body.String())
		}
	}
	if source.calls != 0 {
		t.Fatalf("health triggered %d collections", source.calls)
	}
}

func TestMetricsSuccessEncodesFixedFamilies(t *testing.T) {
	maxMemory, rss, fragmentation := uint64(100), uint64(42), 1.25
	maxClients := uint64(100)
	source := &fakeCollector{snapshot: Snapshot{UptimeSeconds: 5, ConnectedClients: 2, UsedMemoryBytes: 42, CommandsProcessedTotal: 11, KeyspaceHitsTotal: 7, KeyspaceMissesTotal: 3, CPUUserSeconds: 1.5, CPUSystemSeconds: .5, DB: 2, DBKeys: 4, DBExpiringKeys: 1, MaxMemoryBytes: &maxMemory, MaxClients: &maxClients, UsedMemoryRSSBytes: &rss, MemoryFragmentation: &fragmentation, CommandStats: map[string]CommandStats{"get": {CallsTotal: 3, DurationUsecTotal: 500000, RejectedTotal: 1, FailedTotal: 2}}, CommandLatencyStats: map[string]CommandLatencyStats{"get": {PercentilesUsec: map[float64]float64{0.5: 5, 0.99: 7.5}}}}}
	response := httptest.NewRecorder()
	testHandler(source, time.Second, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || source.calls != 1 || response.Header().Get("Content-Type") != metricsContentType || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d calls=%d headers=%v", response.Code, source.calls, response.Header())
	}
	for _, sample := range []string{"gopulse_redis_up 1", "gopulse_redis_commands_processed_total 11", `gopulse_redis_cpu_seconds_total{mode="user"} 1.5`, `gopulse_redis_cpu_seconds_total{mode="system"} 0.5`, `gopulse_redis_db_keys{db="2"} 4`, "gopulse_redis_maxmemory_bytes 100", "gopulse_redis_max_clients 100", "gopulse_redis_used_memory_rss_bytes 42", "gopulse_redis_memory_fragmentation_ratio 1.25", `gopulse_redis_command_calls_total{command="get"} 3`, `gopulse_redis_command_duration_seconds_total{command="get"} 0.5`, `gopulse_redis_command_latency_seconds_count{command="get"} 3`, `gopulse_redis_command_latency_seconds_sum{command="get"} 0.5`} {
		if !strings.Contains(body, sample) {
			t.Fatalf("missing %q in:\n%s", sample, body)
		}
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("metrics response cannot be parsed: %v", err)
	}
	latency := families["gopulse_redis_command_latency_seconds"]
	if latency == nil || len(latency.Metric) != 2 {
		t.Fatalf("unexpected latency family: %#v", latency)
	}
	for _, metric := range latency.Metric {
		labels := make(map[string]string, len(metric.Label))
		for _, label := range metric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		if labels["command"] != "get" {
			t.Fatalf("unexpected latency labels: %v", labels)
		}
		if labels["quantile"] == "0.5" && metric.Gauge.GetValue() != 5e-6 {
			t.Fatalf("unexpected p50: %v", metric)
		}
	}
}

func TestMetricsCommandLatencySharesSummaryContract(t *testing.T) {
	source := &fakeCollector{snapshot: Snapshot{
		CommandStats:        map[string]CommandStats{"get": {CallsTotal: 4, DurationUsecTotal: 120000}},
		CommandLatencyStats: map[string]CommandLatencyStats{"get": {PercentilesUsec: map[float64]float64{0.5: 3, 0.999: 9}}},
	}}
	response := httptest.NewRecorder()
	testHandler(source, time.Second, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(response.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	latency := families["gopulse_redis_command_latency_seconds"]
	if latency == nil || latency.GetType() != dto.MetricType_GAUGE || len(latency.Metric) != 2 {
		t.Fatalf("unexpected latency family: %#v", latency)
	}
	if families["gopulse_redis_command_latency_seconds_count"].GetMetric()[0].GetCounter().GetValue() != 4 || families["gopulse_redis_command_latency_seconds_sum"].GetMetric()[0].GetGauge().GetValue() != .12 {
		t.Fatalf("summary count/sum mismatch: %#v", families)
	}
}

func TestMetricsCommandStatsShareFamilies(t *testing.T) {
	source := &fakeCollector{snapshot: Snapshot{CommandStats: map[string]CommandStats{
		"get": {CallsTotal: 3, DurationUsecTotal: 500000, RejectedTotal: 1, FailedTotal: 2},
		"set": {CallsTotal: 5, DurationUsecTotal: 250000, RejectedTotal: 0, FailedTotal: 1},
	}}}
	response := httptest.NewRecorder()
	testHandler(source, time.Second, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("metrics response cannot be parsed: %v", err)
	}
	for name, want := range map[string]map[string]float64{
		"gopulse_redis_command_calls_total":            {"get": 3, "set": 5},
		"gopulse_redis_command_duration_seconds_total": {"get": 0.5, "set": 0.25},
		"gopulse_redis_command_rejected_total":         {"get": 1, "set": 0},
		"gopulse_redis_command_failed_total":           {"get": 2, "set": 1},
	} {
		t.Run(name, func(t *testing.T) {
			for _, declaration := range []string{"# HELP ", "# TYPE "} {
				if count := strings.Count(body, declaration+name+" "); count != 1 {
					t.Fatalf("%s declaration count = %d, want 1", declaration, count)
				}
			}
			family := families[name]
			if family == nil || family.GetType() != dto.MetricType_COUNTER || len(family.Metric) != len(want) {
				t.Fatalf("unexpected metric family: %v", family)
			}
			seen := make(map[string]bool)
			for _, metric := range family.Metric {
				if len(metric.Label) != 1 || metric.Label[0].GetName() != "command" {
					t.Fatalf("unexpected command labels: %v", metric.Label)
				}
				command := metric.Label[0].GetValue()
				value, ok := want[command]
				if !ok || seen[command] || metric.Counter == nil || metric.Counter.GetValue() != value {
					t.Fatalf("unexpected command sample: %v", metric)
				}
				seen[command] = true
			}
		})
	}
}

func TestMetricsIncludesReplicationAndPersistenceAndOmitsDisabledAOFDetails(t *testing.T) {
	linkUp := true
	aofEnabled := false
	source := &fakeCollector{snapshot: Snapshot{
		Replication: ReplicationSnapshot{
			Role: "master", ConnectedReplicas: uint64Ptr(1), MasterReplOffset: uint64Ptr(44),
			Replicas: []ReplicaSnapshot{{Index: 0, Offset: 42, LagSeconds: 1, LinkUp: true}},
		},
		Persistence: PersistenceSnapshot{
			RDBLastSaveTimestamp: uint64Ptr(1700000000), RDBLastBGSAVESuccess: &linkUp, AOFEnabled: &aofEnabled,
		},
	}}
	response := httptest.NewRecorder()
	testHandler(source, time.Second, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, sample := range []string{
		`gopulse_redis_replication_role{role="master"} 1`,
		"gopulse_redis_replication_connected_replicas 1",
		"gopulse_redis_replication_master_repl_offset 44",
		`gopulse_redis_replica_offset{replica="0"} 42`,
		`gopulse_redis_replica_link_up{replica="0"} 1`,
		"gopulse_redis_rdb_last_save_timestamp_seconds 1.7e+09",
		"gopulse_redis_rdb_last_bgsave_success 1",
		"gopulse_redis_aof_enabled 0",
	} {
		if !strings.Contains(body, sample) {
			t.Fatalf("missing %q in:\n%s", sample, body)
		}
	}
	if strings.Contains(body, "aof_current_size_bytes") || strings.Contains(body, "aof_last_write_success") {
		t.Fatalf("disabled AOF details were exported:\n%s", body)
	}
}

func TestMetricsFailureReturnsOnlyUpZeroAndCanRecover(t *testing.T) {
	source := &fakeCollector{err: &Failure{Reason: ReasonRedisUnavailable}}
	handler := testHandler(source, time.Second, nil)
	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if failed.Code != http.StatusServiceUnavailable || !strings.Contains(failed.Body.String(), "gopulse_redis_up 0") || strings.Contains(failed.Body.String(), "uptime_seconds") {
		t.Fatalf("failed response: %d %s", failed.Code, failed.Body.String())
	}
	source.err = nil
	source.snapshot = Snapshot{DB: 0, UptimeSeconds: 99}
	recovered := httptest.NewRecorder()
	handler.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), "gopulse_redis_uptime_seconds 99") {
		t.Fatalf("recovered response: %d %s", recovered.Code, recovered.Body.String())
	}
}

func TestRequestBoundaries(t *testing.T) {
	handler := testHandler(&fakeCollector{err: errors.New("unused")}, time.Second, nil)
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

func testHandler(source Collector, timeout time.Duration, _ any) http.Handler {
	e := newExporter(&config.Config{ScrapeTimeout: timeout}, &Client{Source: source})
	return e.Handler()
}
