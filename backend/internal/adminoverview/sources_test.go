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
func TestFreshAndStaleKeyMetrics(t *testing.T) {
	now := time.Now().UTC()
	for _, age := range []time.Duration{time.Second, 2 * time.Minute} {
		s := KeyMetrics(pointsStub{now.Add(-age)})(context.Background(), now)
		items := s.Items.([]Metric)
		if len(items) != 6 || *items[0].Value != 7 {
			t.Fatal("lost fixed metric samples")
		}
		if age > freshness {
			if s.Status != "degraded" || items[0].Status != "unknown" || items[0].ReasonCode != "stale" {
				t.Fatal("stale data reported healthy")
			}
		} else if s.Status != "healthy" {
			t.Fatal("fresh data unavailable")
		}
	}
}
