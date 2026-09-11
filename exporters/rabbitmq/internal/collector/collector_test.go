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
		case "/api/vhosts///connections", "/api/vhosts///channels":
			w.Write([]byte(`[]`))
		case "/api/queues//":
			w.Write([]byte(`[{"vhost":"/","consumers":1,"messages_ready":2,"messages_unacknowledged":0}]`))
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), Origin: server.URL, Username: "metrics", Password: "secret"}
	body, err := Collect(context.Background(), client)
	if err != nil || strings.Count(body, "# TYPE ") != 9 || strings.Count(body, "\n") != 19 || !strings.Contains(body, "gopulse_rabbitmq_published_total 3\n") || !strings.Contains(body, "gopulse_rabbitmq_acked_total 0\n") {
		t.Fatal("invalid snapshot", err)
	}
	failed = true
	if body, err := Collect(context.Background(), client); err != ErrUnavailable || body != "" {
		t.Fatal("failed snapshot leaked")
	}
}
