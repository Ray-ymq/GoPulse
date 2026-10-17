package metriccontract

import (
	"math"
	"strconv"
	"testing"
)

func TestSampleLimitCountsDistinctSeries(t *testing.T) {
	samples := make([]Sample, MaxSamples+1)
	for i := range samples {
		samples[i] = Sample{Name: "gopulse_kafka_new_lag", Kind: "gauge", Labels: map[string]string{"partition": strconv.Itoa(i)}, Value: 1}
	}
	if err := Validate("kafka", samples[:MaxSamples]); err != nil {
		t.Fatal("valid bounded snapshot rejected", err)
	}
	if err := Validate("kafka", samples); err == nil {
		t.Fatal("sample limit ignored")
	}
}

func TestSummaryRequiresMatchingCountAndSum(t *testing.T) {
	samples := []Sample{
		{Name: "gopulse_mysql_query_seconds", Kind: "gauge", Labels: map[string]string{"quantile": "0.5", "node": "db-2"}, Value: .2},
		{Name: "gopulse_mysql_query_seconds_count", Kind: "counter", Labels: map[string]string{"node": "db-2"}, Value: 3},
		{Name: "gopulse_mysql_query_seconds_sum", Kind: "gauge", Labels: map[string]string{"node": "db-2"}, Value: .7},
	}
	if err := Validate("mysql", samples); err != nil {
		t.Fatal(err)
	}
	if err := Validate("mysql", samples[:2]); err == nil {
		t.Fatal("incomplete summary accepted")
	}
}

func TestNewMetricsRetainDimensionsWithoutRegistration(t *testing.T) {
	samples := []Sample{
		{Name: "gopulse_business_worker_new_depth", Kind: "gauge", Labels: map[string]string{"queue": "jobs", "node": "worker-2"}, Value: 7},
		{Name: "gopulse_business_worker_new_depth", Kind: "gauge", Labels: map[string]string{"queue": "emails", "node": "worker-2"}, Value: 3},
	}
	if err := Validate("business-worker", samples); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Sample){
		func(s *Sample) { s.Name = "gopulse_redis_new_depth" },
		func(s *Sample) { s.Labels["source"] = "redis" },
		func(s *Sample) { s.Kind = "counter"; s.Value = -1 },
		func(s *Sample) { s.Value = math.Inf(1) },
		func(s *Sample) { s.Labels["__name__"] = "injected" },
	} {
		s := Sample{Name: samples[0].Name, Kind: "gauge", Labels: map[string]string{"queue": "jobs"}, Value: 7}
		mutate(&s)
		if err := Validate("business-worker", []Sample{s}); err == nil {
			t.Fatal("invalid metric accepted", s)
		}
	}
	if err := Validate("business-worker", []Sample{samples[0], samples[0]}); err == nil {
		t.Fatal("duplicate metric accepted")
	}
}

func TestHistogramIntegrityDoesNotDependOnFixedBuckets(t *testing.T) {
	samples := []Sample{
		{Name: "gopulse_mysql_query_seconds_bucket", Kind: "counter", Labels: map[string]string{"le": "0.3", "node": "db-2"}, Value: 2},
		{Name: "gopulse_mysql_query_seconds_bucket", Kind: "counter", Labels: map[string]string{"le": "+Inf", "node": "db-2"}, Value: 3},
		{Name: "gopulse_mysql_query_seconds_count", Kind: "counter", Labels: map[string]string{"node": "db-2"}, Value: 3},
		{Name: "gopulse_mysql_query_seconds_sum", Kind: "gauge", Labels: map[string]string{"node": "db-2"}, Value: .7},
	}
	if err := Validate("mysql", samples); err != nil {
		t.Fatal(err)
	}
	samples[0].Value = 4
	if err := Validate("mysql", samples); err == nil {
		t.Fatal("non-cumulative histogram accepted")
	}
	samples[0].Value = 2
	if err := Validate("mysql", samples[:3]); err == nil {
		t.Fatal("incomplete histogram accepted")
	}
}

func TestUnitOverridesCoverExporterDimensions(t *testing.T) {
	for name, want := range map[string]string{
		"gopulse_redis_blocked_clients":                         "count",
		"gopulse_redis_replication_role":                        "state",
		"gopulse_redis_replica_offset":                          "bytes",
		"gopulse_redis_aof_enabled":                             "boolean",
		"gopulse_mysql_buffer_pool_pages":                       "count",
		"gopulse_mysql_active_transactions":                     "count",
		"gopulse_mysql_redo_lsn_current":                        "bytes",
		"gopulse_rabbitmq_queue_messages":                       "count",
		"gopulse_rabbitmq_queue_memory_bytes":                   "bytes",
		"gopulse_rabbitmq_node_io_operations_total":             "count",
		"gopulse_kafka_consumer_partition_lag":                  "count",
		"gopulse_kafka_partition_offline":                       "boolean",
		"gopulse_kafka_request_duration_seconds_total":          "seconds",
		"gopulse_kafka_network_processor_idle_ratio":            "none",
		"gopulse_kafka_jvm_heap_used_bytes":                     "bytes",
		"gopulse_elasticsearch_thread_pool_queue":               "count",
		"gopulse_elasticsearch_cache_hits_total":                "count",
		"gopulse_elasticsearch_merge_duration_seconds_total":    "seconds",
		"gopulse_victoriametrics_cache_entries":                 "count",
		"gopulse_victoriametrics_query_duration_seconds_total":  "seconds",
		"gopulse_victoriametrics_process_resident_memory_bytes": "bytes",
	} {
		if got := Unit(name, "gauge"); got != want {
			t.Fatalf("Unit(%q)=%q want %q", name, got, want)
		}
	}
}
