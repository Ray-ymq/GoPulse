package collector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixture() string {
	var b strings.Builder
	for _, m := range mappings {
		for _, v := range m.values {
			fmt.Fprintf(&b, "%s{%s=%q} 1\n", m.upstream, m.label, v)
		}
	}
	return b.String() + "gopulse_fake_recursion 999\nvm_http_requests_total{path=\"/metrics\"} 999\nvm_rows{type=\"indexdb/file\"} 999\n"
}
func TestSnapshotContract(t *testing.T) {
	good := fixture()
	body, err := Snapshot(good)
	if err != nil || strings.Count(body, "# TYPE ") != 9 {
		t.Fatalf("incomplete snapshot: %v", err)
	}
	for _, want := range []string{"rows_inserted_total 16\n", "query_requests_total 2\n", "active_timeseries 1\n", "storage_rows 3\n", "storage_size_bytes 7\n", "active_merges 5\n", "storage_rows_deleted_total 3\n"} {
		if !strings.Contains(body, "gopulse_victoriametrics_"+want) {
			t.Fatal(want)
		}
	}
	if strings.Contains(body, "{") || strings.Contains(body, "999") || strings.Contains(body, "retention") {
		t.Fatal("unmanaged output")
	}
	selected := "vm_rows_deleted_total{type=\"storage/small\"} 1\n"
	for name, bad := range map[string]string{
		"missing":   strings.Replace(good, selected, "", 1),
		"duplicate": good + selected,
		"nonfinite": strings.Replace(good, selected, strings.Replace(selected, " 1", " NaN", 1), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if body, err := Snapshot(bad); err == nil || body != "" {
				t.Fatal("partial or invalid snapshot accepted")
			}
		})
	}
}

func TestSnapshotIncludesBoundedWriteAndCacheDetails(t *testing.T) {
	body, err := Snapshot(fixture() + "vm_rows_ignored_total{reason=\"big_timestamp\"} 2\n" + "vm_slow_row_inserts_total 3\n" + "vm_rows_per_insert_count{type=\"promremotewrite\"} 4\n" + "vm_rows_per_insert_sum{type=\"promremotewrite\"} 10\n" + "vm_http_requests_total{path=\"/api/v1/write\"} 11\n" + "vm_http_request_errors_total{path=\"/api/v1/query\"} 12\n" + "vmselect_request_duration_seconds_sum 13.5\n" + "vm_concurrent_select_current 2\n" + "vm_concurrent_select_capacity 8\n" + "vm_concurrent_select_limit_reached_total 3\n" + "vm_concurrent_select_limit_timeout_total 1\n" + "vm_cache_size_bytes{type=\"storage/tsid\"} 4\n" + "vm_cache_size_max_bytes{type=\"storage/tsid\"} 8\n" + "vm_cache_requests_total{type=\"storage/tsid\"} 10\n" + "vm_cache_misses_total{type=\"storage/tsid\"} 1\n" + "vm_cache_entries{type=\"storage/tsid\"} 5\n" + "process_cpu_seconds_total 14.5\n" + "process_resident_memory_bytes 100\n" + "go_memstats_heap_alloc_bytes 200\n" + "go_memstats_heap_sys_bytes 300\n" + "go_gc_duration_seconds_count 4\n" + "go_gc_duration_seconds_sum 0.5\n" + "go_goroutines 6\n" + "go_threads 7\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`gopulse_victoriametrics_rows_ignored_total{reason="big_timestamp"} 2`,
		"gopulse_victoriametrics_slow_row_inserts_total 3",
		`gopulse_victoriametrics_write_batches_total{type="promremotewrite"} 4`,
		`gopulse_victoriametrics_write_batch_rows_total{type="promremotewrite"} 10`,
		`gopulse_victoriametrics_write_requests_total{path="/api/v1/write"} 11`,
		`gopulse_victoriametrics_query_errors_total{path="/api/v1/query"} 12`,
		"gopulse_victoriametrics_query_duration_seconds_total 13.5",
		"gopulse_victoriametrics_active_queries 2",
		"gopulse_victoriametrics_query_capacity 8",
		"gopulse_victoriametrics_query_limit_reached_total 3",
		"gopulse_victoriametrics_query_limit_timeout_total 1",
		`gopulse_victoriametrics_cache_size_bytes{type="storage/tsid"} 4`,
		`gopulse_victoriametrics_cache_misses_total{type="storage/tsid"} 1`,
		"gopulse_victoriametrics_process_cpu_seconds_total 14.5",
		"gopulse_victoriametrics_process_resident_memory_bytes 100",
		"gopulse_victoriametrics_go_heap_alloc_bytes 200",
		"gopulse_victoriametrics_go_heap_sys_bytes 300",
		"gopulse_victoriametrics_go_gc_cycles_total 4",
		"gopulse_victoriametrics_go_gc_pause_seconds_total 0.5",
		"gopulse_victoriametrics_go_goroutines 6",
		"gopulse_victoriametrics_go_threads 7",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}
func TestAuthenticatedFailureAndRecovery(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if r.URL.RequestURI() != "/metrics" || r.Method != "GET" {
			t.Error("unexpected request")
		}
		if !ok || u != "user" || p != "private-canary" || fail {
			w.WriteHeader(401)
			_, _ = w.Write([]byte("private-canary"))
			return
		}
		_, _ = w.Write([]byte(fixture()))
	}))
	defer server.Close()
	c := &Client{HTTP: server.Client(), Origin: server.URL, Username: "user", Password: "private-canary"}
	for _, broken := range []bool{false, true, false} {
		fail = broken
		body, err := Collect(context.Background(), c)
		if broken {
			if err != ErrUnavailable || body != "" {
				t.Fatal("unsafe failure")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}
