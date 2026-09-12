// Package adminoverview owns the fixed, partial management dashboard contract.
package adminoverview

import (
	"context"
	"sync"
	"time"
)

// Section never contains upstream errors, queries, credentials or addresses.
type Section struct {
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observed_at"`
	ReasonCode string     `json:"reason_code"`
	Items      any        `json:"items"`
}
type Snapshot struct {
	GeneratedAt time.Time `json:"generated_at"`
	Status      string    `json:"status"`
	Components  Section   `json:"components"`
	KeyMetrics  Section   `json:"key_metrics"`
	Logs        Section   `json:"logs"`
	Events      Section   `json:"events"`
	Plugins     Section   `json:"plugins"`
	Alerts      Section   `json:"alerts"`
}
type Loader func(context.Context, time.Time) Section
type Service struct{ Components, KeyMetrics, Logs, Events, Plugins, Alerts Loader }

// Six workers is a fixed concurrency ceiling; every loader shares cancellation
// and gets a shorter sub-budget than the complete HTTP request.
func (s *Service) Overview(ctx context.Context) Snapshot {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	now := time.Now().UTC()
	out := Snapshot{GeneratedAt: now, Status: "healthy"}
	targets := []*Section{&out.Components, &out.KeyMetrics, &out.Logs, &out.Events, &out.Plugins, &out.Alerts}
	loaders := []Loader{s.Components, s.KeyMetrics, s.Logs, s.Events, s.Plugins, s.Alerts}
	var wg sync.WaitGroup
	for i, load := range loaders {
		wg.Add(1)
		go func(i int, load Loader) {
			defer wg.Done()
			sub, stop := context.WithTimeout(ctx, 2500*time.Millisecond)
			defer stop()
			*targets[i] = Section{Status: "unavailable", ReasonCode: "upstream_unavailable", Items: []any{}}
			if load != nil {
				*targets[i] = load(sub, now)
			}
		}(i, load)
	}
	wg.Wait()
	available := 0
	for _, section := range targets {
		if section.Status != "unavailable" {
			available++
		}
		if section.Status != "healthy" {
			out.Status = "degraded"
		}
	}
	if available == 0 {
		out.Status = "unavailable"
	}
	return out
}
