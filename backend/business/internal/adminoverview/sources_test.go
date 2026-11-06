package adminoverview

import (
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"testing"
	"time"
)

type pointsStub struct{ at time.Time }

func (p pointsStub) AlertPoints(context.Context, string, map[string]string, time.Time, time.Time) ([]metricquery.Point, error) {
	return []metricquery.Point{{Timestamp: p.at.Format(time.RFC3339Nano), Value: 7}}, nil
}
func TestFreshAndStaleExternalMetricSamples(t *testing.T) {
	now := time.Now().UTC()
	for _, age := range []time.Duration{time.Second, 2 * time.Minute} {
		got := sample(context.Background(), pointsStub{now.Add(-age)}, "gopulse_redis_connected_clients", nil, now)
		if got.Value == nil || *got.Value != 7 {
			t.Fatal("lost metric")
		}
		if age > freshness {
			if got.Status != "unknown" || got.ReasonCode != "stale" {
				t.Fatal("stale data healthy")
			}
		} else if got.Status != "healthy" {
			t.Fatal("fresh data unavailable")
		}
	}
}
