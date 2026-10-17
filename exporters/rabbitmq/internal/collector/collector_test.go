package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVhostSnapshotAndFailure(t *testing.T) {
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failed {
			w.WriteHeader(401)
			w.Write([]byte(`secret upstream response`))
			return
		}
		switch r.URL.Path {
		case "/api/vhosts":
			w.Write([]byte(`[{"name":"other","message_stats":{"publish":99}},{"name":"/","message_stats":{"publish":3}}]`))
		case "/api/vhosts///connections":
			w.Write([]byte(`[{"vhost":"/","state":"blocked","recv_oct":100,"send_oct":200},{"vhost":"/","state":"running","recv_oct":50,"send_oct":80}]`))
		case "/api/vhosts///channels":
			w.Write([]byte(`[]`))
		case "/api/queues//":
			w.Write([]byte(`[{"name":"events","vhost":"/","consumers":1,"messages_ready":2,"messages_unacknowledged":0,"consumer_utilisation":0.5,"memory":128,"message_bytes":256,"message_stats":{"publish":3,"deliver_get":2,"ack":1,"redeliver":1}}]`))
		case "/api/nodes":
			w.Write([]byte(`[{"name":"rabbit@node","mem_used":10,"mem_limit":20,"disk_free":30,"disk_free_limit":40,"fd_used":2,"fd_total":100,"proc_used":3,"proc_total":200,"mem_alarm":false,"disk_free_alarm":false,"io_read_count":3,"io_read_avg_time":10,"io_write_count":4,"io_write_avg_time":20}]`))
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Origin: server.URL, Username: "metrics", Password: "secret"}
	body, err := Collect(context.Background(), client)
	if err != nil || !strings.Contains(body, "gopulse_rabbitmq_published_total 3\n") || !strings.Contains(body, "gopulse_rabbitmq_acked_total 0\n") || !strings.Contains(body, "gopulse_rabbitmq_connections_blocked 1\n") || !strings.Contains(body, "gopulse_rabbitmq_connection_received_bytes_total 150\n") || !strings.Contains(body, "gopulse_rabbitmq_connection_sent_bytes_total 280\n") || !strings.Contains(body, `gopulse_rabbitmq_queue_messages{vhost="/",queue="events",state="ready"} 2`) || !strings.Contains(body, `gopulse_rabbitmq_queue_memory_bytes{vhost="/",queue="events"} 128`) || !strings.Contains(body, `gopulse_rabbitmq_queue_message_bytes{vhost="/",queue="events"} 256`) || !strings.Contains(body, `gopulse_rabbitmq_queue_messages_total{vhost="/",queue="events",operation="redeliver"} 1`) || !strings.Contains(body, `gopulse_rabbitmq_node_memory_used_bytes{node_id="rabbit@node"} 10`) || !strings.Contains(body, `gopulse_rabbitmq_node_io_operations_total{node_id="rabbit@node",operation="read"} 3`) || !strings.Contains(body, `gopulse_rabbitmq_node_io_average_duration_seconds{node_id="rabbit@node",operation="read"} 1e-05`) {
		t.Fatalf("invalid snapshot: %v\n%s", err, body)
	}
	failed = true
	if body, err := Collect(context.Background(), client); err != ErrUnavailable || body != "" {
		t.Fatal("failed snapshot leaked")
	}
}
