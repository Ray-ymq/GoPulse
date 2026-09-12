package adminoverview

import (
	"context"
	"fmt"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type freshUp struct{ at time.Time }

func (p freshUp) AlertPoints(context.Context, string, map[string]string, time.Time, time.Time) ([]metricquery.Point, error) {
	return []metricquery.Point{{Timestamp: p.at.Format(time.RFC3339Nano), Value: 1}}, nil
}
func TestPluginLifecycleOutranksFreshSample(t *testing.T) {
	now := time.Now().UTC()
	at := now.Add(-time.Second).Format(time.RFC3339Nano)
	for _, state := range []string{"running", "failed"} {
		t.Run(state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"data":[{"id":"redis-exporter","name":"Redis Exporter","version":"1.12.7","kind":"metrics-exporter","source":"redis","desired_state":"running","observed_state":%q,"installed_at":%q,"updated_at":%q,"started_at":%q,"last_scrape_at":%q,"last_success_at":%q}]}`, state, at, at, at, at, at)
			}))
			defer server.Close()
			client, err := exporterplugin.NewClient(server.URL, strings.Repeat("x", 32), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			s := Plugins(client, freshUp{now.Add(-time.Second)})(context.Background(), now)
			p := s.Items.([]Plugin)[0]
			want, reason := "healthy", "ok"
			if state == "failed" {
				want, reason = "degraded", "process_failed"
			}
			if p.Status != want || p.ReasonCode != reason || p.Observed != state || p.Desired != "running" || p.Up == nil || *p.Up != 1 {
				t.Fatalf("unexpected plugin: %+v", p)
			}
		})
	}
}
