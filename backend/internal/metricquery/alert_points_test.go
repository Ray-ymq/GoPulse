package metricquery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAlertOriginalSamplesAndEmpty(t *testing.T) {
	now := time.UnixMilli(1800000000000).UTC()
	empty := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/export" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.Form.Get("match[]") != `gopulse_redis_cpu_seconds_total{source="redis",target_id="redis-exporter-local",mode="user"}` {
			t.Errorf("selector %s", r.Form.Get("match[]"))
		}
		if empty {
			return
		}
		fmt.Fprintf(w, `{"metric":{"__name__":"gopulse_redis_cpu_seconds_total","source":"redis","target_id":"redis-exporter-local","mode":"user"},"values":[8,2],"timestamps":[%d,%d]}`, now.Add(-15*time.Second).UnixMilli(), now.UnixMilli())
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "test", "secret", time.Second)
	points, e := client.AlertPoints(context.Background(), "gopulse_redis_cpu_seconds_total", map[string]string{"mode": "user"}, now.Add(-time.Minute), now)
	if e != nil || len(points) != 2 || points[1].Value != 2 {
		t.Fatalf("%v %v", points, e)
	}
	empty = true
	points, e = client.AlertPoints(context.Background(), "gopulse_redis_cpu_seconds_total", map[string]string{"mode": "user"}, now.Add(-time.Minute), now)
	if e != nil || len(points) != 0 {
		t.Fatalf("empty %v %v", points, e)
	}
}
