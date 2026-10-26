package componentstatus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceKeepsInstancesIndependentAndExpiresResults(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	current := now
	service, err := New(Config{
		Targets: []Target{
			{Component: "backend", Instance: "backend-1", Probe: func(context.Context) error { return nil }},
			{Component: "backend", Instance: "backend-2", Probe: func(context.Context) error { return errors.New("down") }},
		},
		Now: func() time.Time { return current }, TTL: time.Second, Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows := service.Snapshot()
	if rows[0].Status != "healthy" || rows[1].Status != "unavailable" || rows[1].ReasonCode != "connection_failed" {
		t.Fatalf("rows=%+v", rows)
	}
	current = current.Add(2 * time.Second)
	rows = service.Snapshot()
	if rows[0].Status != "unknown" || rows[0].ReasonCode != "stale" {
		t.Fatalf("stale rows=%+v", rows)
	}
}

func TestServiceDoesNotExceedConcurrencyLimit(t *testing.T) {
	var active, maximum atomic.Int64
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	probe := func(context.Context) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return nil
	}
	service, err := New(Config{Targets: []Target{{Component: "a", Instance: "1", Probe: probe}, {Component: "b", Instance: "1", Probe: probe}, {Component: "c", Instance: "1", Probe: probe}, {Component: "d", Instance: "1", Probe: probe}}, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = service.Refresh(context.Background()) }()
	for range 2 {
		<-entered
	}
	close(release)
	deadline := time.After(time.Second)
	for len(entered) > 0 {
		select {
		case <-entered:
		case <-deadline:
			t.Fatal("refresh did not finish")
		}
	}
	if maximum.Load() > 2 {
		t.Fatalf("maximum concurrency=%d", maximum.Load())
	}
}
