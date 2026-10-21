package adminoverview

import (
	"context"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/componentstatus"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
)

type Metric struct {
	ID         string     `json:"id"`
	Value      *float64   `json:"value"`
	Unit       string     `json:"unit"`
	ObservedAt *time.Time `json:"observed_at"`
	Status     string     `json:"status"`
	ReasonCode string     `json:"reason_code"`
}

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

type Component struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observed_at"`
	ReasonCode string     `json:"reason_code"`
}

// ComponentStatus uses the same direct probe snapshot as the dedicated admin
// endpoint so the overview cannot report a different liveness result.
func ComponentStatus(service *componentstatus.Service) Loader {
	return func(_ context.Context, now time.Time) Section {
		if service == nil {
			return Section{Status: "unavailable", ObservedAt: &now, ReasonCode: "upstream_unavailable", Items: []componentstatus.Status{}}
		}
		items := service.Snapshot()
		status, reason := "healthy", "ok"
		if len(items) == 0 {
			status, reason = "unavailable", "no_targets"
		} else {
			for _, item := range items {
				if item.Status != "healthy" {
					status, reason = "degraded", "partial_data"
					break
				}
			}
		}
		return Section{Status: status, ObservedAt: &now, ReasonCode: reason, Items: items}
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
		for _, source := range []string{"redis", "mysql", "kafka", "elasticsearch", "rabbitmq", "victoriametrics", "node"} {
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
