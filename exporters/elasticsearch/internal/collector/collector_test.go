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
		if strings.Count(body, "# TYPE ") != 12 {
			t.Fatal("incomplete family set")
		}
	}
}
