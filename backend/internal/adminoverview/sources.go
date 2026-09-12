package adminoverview

import (
	"context"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

type Metric struct {
	ID         string     `json:"id"`
	Value      *float64   `json:"value"`
	Unit       string     `json:"unit"`
	ObservedAt *time.Time `json:"observed_at"`
	Status     string     `json:"status"`
	ReasonCode string     `json:"reason_code"`
}

var componentIDs = []string{"backend", "business-worker", "search-indexer", "monitor", "router", "marshaller"}
var keyNames = []string{"outbox_pending", "messages_in_flight", "retrying", "event_queue_length", "buffered_records", "retrying"}

const freshness = 90 * time.Second

func sample(ctx context.Context, samples alert.Samples, metric string, labels map[string]string, now time.Time) Metric {
	m := Metric{ID: metric, Unit: "count", Status: "unknown", ReasonCode: "missing"}
	points, err := samples.AlertPoints(ctx, metric, labels, now.Add(-15*time.Minute), now)
	if err != nil {
		m.Status = "unavailable"
		m.ReasonCode = "upstream_unavailable"
		return m
	}
	if len(points) == 0 {
		return m
	}
	p := points[len(points)-1]
	t, err := time.Parse(time.RFC3339Nano, p.Timestamp)
	if err != nil {
		return m
	}
	m.Value = &p.Value
	m.ObservedAt = &t
	m.Status = "healthy"
	m.ReasonCode = "ok"
	if now.Sub(t) > freshness {
		m.Status = "unknown"
		m.ReasonCode = "stale"
	}
	return m
}
func section(items any, now time.Time, statuses ...string) Section {
	s := Section{Status: "healthy", ObservedAt: &now, ReasonCode: "ok", Items: items}
	for _, v := range statuses {
		if v != "healthy" {
			s.Status = "degraded"
			s.ReasonCode = "partial_data"
		}
	}
	return s
}
func KeyMetrics(samples alert.Samples) Loader {
	return func(ctx context.Context, now time.Time) Section {
		items := []Metric{}
		statuses := []string{}
		for i, id := range componentIDs {
			m := sample(ctx, samples, componentmetrics.Prefix(id)+keyNames[i], map[string]string{}, now)
			items = append(items, m)
			statuses = append(statuses, m.Status)
		}
		return section(items, now, statuses...)
	}
}

type Component struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observed_at"`
	ReasonCode string     `json:"reason_code"`
}

func Components(samples alert.Samples) Loader {
	return func(ctx context.Context, now time.Time) Section {
		items := []Component{}
		statuses := []string{}
		for _, id := range componentIDs {
			m := sample(ctx, samples, "gopulse_monitor_last_scrape_success_timestamp_seconds", map[string]string{"scraped_producer_kind": "component", "scraped_target_id": componentmetrics.Target(id)}, now)
			c := Component{ID: id, Status: m.Status, ObservedAt: m.ObservedAt, ReasonCode: m.ReasonCode}
			if m.Value != nil {
				t := time.Unix(int64(*m.Value), 0).UTC()
				c.ObservedAt = &t
				if now.Sub(t) > freshness || t.After(now) {
					c.Status = "unknown"
					c.ReasonCode = "stale"
				}
			}
			spec, _ := componentmetrics.Catalog(id)
			for _, f := range spec.Families {
				if f.Name != componentmetrics.Prefix(id)+"dependency_up" {
					continue
				}
				for _, tuple := range f.Tuples {
					dep := sample(ctx, samples, f.Name, map[string]string{"dependency": tuple[0]}, now)
					if dep.Status != "healthy" {
						c.Status = "unknown"
						c.ReasonCode = dep.ReasonCode
					} else if dep.Value != nil && *dep.Value != 1 && c.Status == "healthy" {
						c.Status = "degraded"
						c.ReasonCode = "dependency_down"
					}
				}
			}
			items = append(items, c)
			statuses = append(statuses, c.Status)
		}
		return section(items, now, statuses...)
	}
}

// Counts uses the fixed internal count repository rather than widening the
// public rule selector vocabulary for dashboard-only global severity counts.
type Counter func(context.Context, string, time.Time, time.Time) (int64, error)
type Count struct {
	Severity string `json:"severity"`
	Value    *int64 `json:"value"`
	Status   string `json:"status"`
}

func Counts(query Counter) Loader {
	return func(ctx context.Context, now time.Time) Section {
		items := []Count{}
		statuses := []string{}
		for _, severity := range []string{"info", "warn", "error"} {
			n, err := query(ctx, severity, now.Add(-15*time.Minute), now)
			c := Count{Severity: severity, Status: "healthy"}
			if err != nil {
				c.Status = "unavailable"
			} else {
				c.Value = &n
			}
			items = append(items, c)
			statuses = append(statuses, c.Status)
		}
		return section(items, now, statuses...)
	}
}

type Plugin struct {
	ID         string     `json:"id"`
	Installed  *bool      `json:"installed"`
	Desired    string     `json:"desired"`
	Observed   string     `json:"observed"`
	Up         *float64   `json:"up"`
	ObservedAt *time.Time `json:"observed_at"`
	Status     string     `json:"status"`
	ReasonCode string     `json:"reason_code"`
}

func Plugins(client *exporterplugin.Client, samples alert.Samples) Loader {
	return func(ctx context.Context, now time.Time) Section {
		rows, err := client.List(ctx)
		items := []Plugin{}
		statuses := []string{}
		for _, source := range []string{"redis", "mysql", "kafka", "elasticsearch", "rabbitmq", "victoriametrics"} {
			id := source + "-exporter"
			m := sample(ctx, samples, "gopulse_"+source+"_up", map[string]string{}, now)
			p := Plugin{ID: id, Desired: "unknown", Observed: "unknown", Up: m.Value, ObservedAt: m.ObservedAt, Status: m.Status, ReasonCode: m.ReasonCode}
			if err != nil {
				p.Status = "unknown"
				p.ReasonCode = "monitor_unavailable"
			} else {
				installed := false
				p.Installed = &installed
				for _, r := range rows {
					if r.ID == id {
						installed = true
						p.Desired = r.DesiredState
						p.Observed = r.ObservedState
						p.ObservedAt = r.LastSuccessAt
						if r.LastSuccessAt == nil || now.Sub(*r.LastSuccessAt) > freshness {
							p.Status = "unknown"
							p.ReasonCode = "stale"
						}
						// Confirmed process failure outranks even fresh successful samples.
						if r.ObservedState == "failed" {
							p.Status = "degraded"
							p.ReasonCode = "process_failed"
						}
					}
				}
				if !installed {
					p.Status = "unknown"
					p.ReasonCode = "not_installed"
				}
			}
			if p.Status == "healthy" && p.Up != nil && *p.Up != 1 {
				p.Status = "degraded"
				p.ReasonCode = "dependency_down"
			}
			items = append(items, p)
			statuses = append(statuses, p.Status)
		}
		return section(items, now, statuses...)
	}
}
