package adminoverview

import (
	"context"
	"testing"
	"time"
)

func TestPartialSnapshot(t *testing.T) {
	healthy := func(_ context.Context, now time.Time) Section {
		return Section{Status: "healthy", ObservedAt: &now, ReasonCode: "ok", Items: []any{}}
	}
	s := Service{Alerts: healthy}
	got := s.Overview(context.Background())
	if got.Status != "degraded" || got.Alerts.Status != "healthy" || got.Logs.Status != "unavailable" {
		t.Fatalf("unexpected partial snapshot: %+v", got)
	}
}
func TestCancelledSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loader := func(ctx context.Context, _ time.Time) Section {
		<-ctx.Done()
		return Section{Status: "unavailable", ReasonCode: "upstream_unavailable", Items: []any{}}
	}
	s := Service{Components: loader, KeyMetrics: loader, Logs: loader, Events: loader, Plugins: loader, Alerts: loader}
	if got := s.Overview(ctx); got.Status != "unavailable" {
		t.Fatalf("status: %s", got.Status)
	}
}

func TestFanoutBudget(t *testing.T) {
	loader := func(ctx context.Context, _ time.Time) Section {
		<-ctx.Done()
		return Section{Status: "unavailable", ReasonCode: "upstream_unavailable", Items: []any{}}
	}
	s := Service{Components: loader, KeyMetrics: loader, Logs: loader, Events: loader, Plugins: loader, Alerts: loader}
	start := time.Now()
	s.Overview(context.Background())
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("fanout exceeded total budget: %v", elapsed)
	}
}
