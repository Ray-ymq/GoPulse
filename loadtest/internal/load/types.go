// Package load runs the Phase 18 open-loop mixed workload.
package load

import (
	"sort"
	"time"
)

const ReportSchemaVersion = "gopulse.phase18.load.v1"

type Category string

const (
	CategoryRead         Category = "read"
	CategorySearch       Category = "search"
	CategoryNotification Category = "notification_bookmark_read"
	CategoryContentWrite Category = "content_write"
	CategoryInteraction  Category = "interaction_write"
	CategorySession      Category = "identity_session"
)

type CounterSummary struct {
	Requests        uint64 `json:"requests"`
	Succeeded       uint64 `json:"succeeded"`
	ExplicitRejects uint64 `json:"explicit_rejects"`
	Timeouts        uint64 `json:"timeouts"`
	Errors          uint64 `json:"errors"`
}

func (summary *CounterSummary) Add(other CounterSummary) {
	summary.Requests += other.Requests
	summary.Succeeded += other.Succeeded
	summary.ExplicitRejects += other.ExplicitRejects
	summary.Timeouts += other.Timeouts
	summary.Errors += other.Errors
}

type LatencySummary struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type RouteReport struct {
	Category Category          `json:"category"`
	Method   string            `json:"method"`
	Template string            `json:"template"`
	Counts   CounterSummary    `json:"counts"`
	Statuses map[string]uint64 `json:"statuses"`
	Latency  LatencySummary    `json:"latency"`
}

type PhaseReport struct {
	Name              string                      `json:"name"`
	TargetRPS         float64                     `json:"target_rps"`
	DurationSeconds   float64                     `json:"duration_seconds"`
	ScheduledSlots    uint64                      `json:"scheduled_slots"`
	DroppedSlots      uint64                      `json:"dropped_slots"`
	MaxScheduleLagMS  float64                     `json:"max_schedule_lag_ms"`
	CompletedRequests uint64                      `json:"completed_requests"`
	Counts            CounterSummary              `json:"counts"`
	LatencyByCategory map[Category]LatencySummary `json:"latency_by_category"`
	CountsByCategory  map[Category]CounterSummary `json:"counts_by_category"`
}

type ProcessStats struct {
	RSSBytes       uint64 `json:"rss_bytes"`
	Goroutines     int    `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
}

type Report struct {
	SchemaVersion   string                 `json:"schema_version"`
	Seed            uint64                 `json:"seed"`
	StartedAt       time.Time              `json:"started_at"`
	FinishedAt      time.Time              `json:"finished_at"`
	SteadyTargetRPS float64                `json:"steady_target_rps"`
	BurstTargetRPS  float64                `json:"burst_target_rps"`
	VirtualUsers    int                    `json:"virtual_users"`
	Phases          []PhaseReport          `json:"phases"`
	Routes          map[string]RouteReport `json:"routes"`
	Total           CounterSummary         `json:"total"`
	LoadProcess     ProcessStats           `json:"load_process"`
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	index := int(fraction*float64(len(ordered)-1) + 0.999999)
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func summarize(values []float64) LatencySummary {
	if len(values) == 0 {
		return LatencySummary{}
	}
	maximum := values[0]
	for _, value := range values[1:] {
		if value > maximum {
			maximum = value
		}
	}
	return LatencySummary{
		P50MS: percentile(values, .50), P95MS: percentile(values, .95),
		P99MS: percentile(values, .99), MaxMS: maximum,
	}
}
