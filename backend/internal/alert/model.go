package alert

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"math"
	"time"
	"unicode"
	"unicode/utf8"
)

type Selector struct {
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels"`
}
type Input struct {
	Name      string   `json:"name"`
	Enabled   *bool    `json:"enabled,omitempty"`
	Severity  string   `json:"severity"`
	Source    string   `json:"source"`
	Selector  Selector `json:"selector"`
	Reducer   string   `json:"reducer"`
	Operator  string   `json:"operator"`
	Threshold *float64 `json:"threshold"`
	Window    string   `json:"window"`
	For       string   `json:"for"`
	Revision  uint64   `json:"revision,omitempty"`
}
type State struct {
	State            string     `json:"state"`
	DataStatus       string     `json:"data_status"`
	PendingSince     *time.Time `json:"pending_since"`
	ActiveIncidentID *uint64    `json:"active_incident_id"`
	LastValue        *float64   `json:"last_value"`
	LastEvaluatedAt  *time.Time `json:"last_evaluated_at"`
	LastSuccessAt    *time.Time `json:"last_success_at"`
	ErrorCode        string     `json:"error_code"`
}
type Rule struct {
	Input
	ID        uint64    `json:"id"`
	CreatedBy uint64    `json:"created_by"`
	UpdatedBy uint64    `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	State     State     `json:"evaluation"`
}
type Incident struct {
	ID               uint64     `json:"id"`
	RuleID           uint64     `json:"rule_id"`
	Revision         uint64     `json:"revision"`
	Name             string     `json:"name"`
	Severity         string     `json:"severity"`
	Source           string     `json:"source"`
	Object           Selector   `json:"object"`
	Status           string     `json:"status"`
	FirstTriggeredAt time.Time  `json:"first_triggered_at"`
	LastTriggeredAt  time.Time  `json:"last_triggered_at"`
	LastEvaluatedAt  time.Time  `json:"last_evaluated_at"`
	RecoveredAt      *time.Time `json:"recovered_at"`
	ClosedAt         *time.Time `json:"closed_at"`
	ResolutionReason string     `json:"resolution_reason"`
	LastValue        *float64   `json:"last_value"`
	EvaluationCount  uint64     `json:"evaluation_count"`
}

func validation() error { return apperror.New(apperror.CodeValidationFailed, "invalid alert request") }
func unavailable() error {
	return apperror.New(apperror.CodeAlertsUnavailable, "alerts are temporarily unavailable")
}
func member(v string, choices ...string) bool {
	for _, c := range choices {
		if v == c {
			return true
		}
	}
	return false
}
func Validate(in Input, create bool) error {
	if len(in.Name) < 1 || len(in.Name) > 80 || !utf8.ValidString(in.Name) || !member(in.Severity, "warning", "critical") || !member(in.Source, "metrics", "logs", "events") || in.Threshold == nil || math.IsNaN(*in.Threshold) || math.IsInf(*in.Threshold, 0) || math.Abs(*in.Threshold) > 1e15 || !member(in.Operator, "gt", "gte", "lt", "lte", "eq", "neq") || !member(in.Window, "1m", "5m", "15m") || !member(in.For, "0s", "1m", "5m") {
		return validation()
	}
	visible := false
	for _, r := range in.Name {
		if unicode.IsControl(r) {
			return validation()
		}
		if !unicode.IsSpace(r) {
			visible = true
		}
	}
	if !visible {
		return validation()
	}
	w, _ := time.ParseDuration(in.Window)
	f, _ := time.ParseDuration(in.For)
	if f > w || create && (in.Enabled == nil || in.Revision != 0) || !create && (in.Enabled != nil || in.Revision == 0) {
		return validation()
	}

	if in.Source != "metrics" {
		if in.Selector.Metric != "" || in.Reducer != "count" {
			return validation()
		}
		valid := false
		if in.Source == "logs" {
			valid = logquery.ValidateAlertSelector(in.Selector.Labels)
		} else {
			valid = eventquery.ValidateAlertSelector(in.Selector.Labels)
		}
		if !valid {
			return validation()
		}
		return nil
	}
	for _, d := range metricquery.AlertCatalog() {
		if d.Metric != in.Selector.Metric {
			continue
		}
		if in.Selector.Labels == nil || len(in.Selector.Labels) != len(d.Keys) || !member(in.Reducer, d.Reducers...) {
			return validation()
		}
		for _, tuple := range d.Tuples {
			match := true
			for i, k := range d.Keys {
				if in.Selector.Labels[k] != tuple[i] {
					match = false
				}
			}
			if match {
				return nil
			}
		}
		return validation()
	}
	return validation()
}

// Reduce never invents a zero for absent data; counters need two real samples.
func Reduce(points []metricquery.Point, reducer string, cutoff time.Time) (float64, bool) {
	if len(points) == 0 {
		return 0, false
	}
	last := points[len(points)-1]
	t, e := time.Parse(time.RFC3339Nano, last.Timestamp)
	if e != nil || t.Before(cutoff.Add(-90*time.Second)) || t.After(cutoff) {
		return 0, false
	}
	v := points[0].Value
	switch reducer {
	case "last":
		v = last.Value
	case "min", "max", "avg":
		for i, p := range points {
			if reducer == "min" && p.Value < v || reducer == "max" && p.Value > v {
				v = p.Value
			}
			if reducer == "avg" && i > 0 {
				v += p.Value
			}
		}
		if reducer == "avg" {
			v /= float64(len(points))
		}
	case "increase":
		if len(points) < 2 {
			return 0, false
		}
		v = 0
		for i := 1; i < len(points); i++ {
			delta := points[i].Value - points[i-1].Value
			if delta < 0 {
				delta = points[i].Value
			}
			if delta < 0 {
				return 0, false
			}
			v += delta
		}
	default:
		return 0, false
	}
	return v, !math.IsNaN(v) && !math.IsInf(v, 0)
}
func condition(r Rule, v float64) bool {
	t := *r.Threshold
	switch r.Operator {
	case "gt":
		return v > t
	case "gte":
		return v >= t
	case "lt":
		return v < t
	case "lte":
		return v <= t
	case "eq":
		return v == t
	case "neq":
		return v != t
	}
	return false
}
