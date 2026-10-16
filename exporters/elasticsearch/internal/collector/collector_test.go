package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Unit evidence for primary-only aggregation, not a substitute for the pending
// real Elasticsearch yellow/red completion gate.
func TestPrimarySnapshotFailureAndRecovery(t *testing.T) {
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write: %s", r.Method)
		}
		switch r.URL.RequestURI() {
		case "/_cluster/health":
			w.Write([]byte(`{"status":"yellow","timed_out":false,"number_of_nodes":1,"number_of_data_nodes":1,"active_primary_shards":2,"active_shards":2,"relocating_shards":0,"initializing_shards":0,"unassigned_shards":2,"number_of_pending_tasks":0}`))
		case "/_stats/docs,store?level=cluster":
			if failed {
				w.Write([]byte(`{"_shards":{"total":4,"successful":2,"failed":0},"_all":{"primaries":{"docs":{},"store":{"size_in_bytes":1024}}}}`))
				return
			}
			w.Write([]byte(`{"_shards":{"total":4,"successful":2,"failed":0},"_all":{"primaries":{"docs":{"count":7},"store":{"size_in_bytes":1024}},"total":{"docs":{"count":14},"store":{"size_in_bytes":2048}}}}`))
		case "/_nodes/stats/jvm,thread_pool,indices,fs":
			w.Write([]byte(`{"nodes":{"node-1":{"jvm":{"mem":{"heap_used_in_bytes":100,"heap_max_in_bytes":200},"gc":{"collectors":{"young":{"collection_count":2,"collection_time_in_millis":10}}}},"thread_pool":{"search":{"active":1,"queue":2,"completed":3,"rejected":4}},"indices":{"search":{"query_total":5,"query_time_in_millis":20,"query_current":1,"fetch_total":6,"fetch_time_in_millis":30,"fetch_current":2},"indexing":{"index_total":7,"index_time_in_millis":40,"index_current":3,"index_failed":1}},"fs":{"total":{"total_in_bytes":1000,"free_in_bytes":600,"available_in_bytes":500}}}}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := &Client{HTTP: server.Client(), Origin: server.URL}
	for _, broken := range []bool{false, true, false} {
		failed = broken
		body, err := Collect(context.Background(), c)
		if broken {
			if err != ErrUnavailable || body != "" {
				t.Fatalf("incomplete snapshot: %q %v", body, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"gopulse_elasticsearch_up 1\n", "gopulse_elasticsearch_cluster_health_status{status=\"yellow\"} 1\n", "gopulse_elasticsearch_documents 7\n", "gopulse_elasticsearch_store_size_bytes 1024\n"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q: %s", want, body)
			}
		}
		for _, want := range []string{
			`gopulse_elasticsearch_jvm_heap_used_bytes{node_id="node-1"} 100`,
			`gopulse_elasticsearch_jvm_gc_collection_time_seconds_total{gc="young",node_id="node-1"} 0.01`,
			`gopulse_elasticsearch_search_operations_total{node_id="node-1",operation="query"} 5`,
			`gopulse_elasticsearch_indexing_failed_total{node_id="node-1"} 1`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q in %s", want, body)
			}
		}
	}
}
