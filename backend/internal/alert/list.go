package alert

import (
	"context"
	"encoding/json"
	"time"
)

type Query struct {
	Kind     string    `json:"kind"`
	Status   string    `json:"status"`
	Source   string    `json:"source"`
	Severity string    `json:"severity"`
	RuleID   uint64    `json:"rule"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Limit    int       `json:"limit"`
	Before   uint64    `json:"before"`
	Time     time.Time `json:"time"`
	Rank     int       `json:"rank"`
}

func (p *Repository) ListRules(ctx context.Context, q Query) ([]Rule, error) {
	query := "SELECT " + ruleColumns + ruleFrom + "WHERE r.deleted_at IS NULL"
	args := []any{}
	if q.Before > 0 {
		query += " AND r.id<?"
		args = append(args, q.Before)
	}
	if q.Status != "" {
		query += " AND s.state=?"
		args = append(args, q.Status)
	}
	if q.Source != "" {
		query += " AND r.source=?"
		args = append(args, q.Source)
	}
	query += " ORDER BY r.id DESC LIMIT ?"
	args = append(args, q.Limit+1)
	rows, e := p.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, e := scanRule(rows)
		if e != nil {
			return nil, unavailable()
		}
		out = append(out, r)
	}
	return out, dbError(rows.Err())
}
func (p *Repository) ListIncidents(ctx context.Context, q Query) ([]Incident, error) {
	query := `SELECT id,rule_id,revision,name,severity,source,object,status,first_triggered_at,last_triggered_at,last_evaluated_at,recovered_at,closed_at,resolution_reason,observed_value,evaluation_count,COALESCE((SELECT data_status FROM alert_rule_states WHERE active_incident_id=alert_incidents.id),'historical') FROM alert_incidents WHERE 1=1`
	args := []any{}
	if q.Kind == "current" {
		query += " AND status='firing'"
	} else {
		query += " AND first_triggered_at>=? AND first_triggered_at<=?"
		args = append(args, q.Start, q.End)
	}
	for _, f := range []struct{ k, v string }{{"status", q.Status}, {"severity", q.Severity}, {"source", q.Source}} {
		if f.v != "" {
			query += " AND " + f.k + "=?"
			args = append(args, f.v)
		}
	}
	if q.RuleID > 0 {
		query += " AND rule_id=?"
		args = append(args, q.RuleID)
	}
	if q.Before > 0 {
		if q.Kind == "current" {
			query += " AND ((severity='critical')<? OR ((severity='critical')=? AND (first_triggered_at>? OR (first_triggered_at=? AND id>?))))"
			args = append(args, q.Rank, q.Rank, q.Time, q.Time, q.Before)
		} else {
			query += " AND (first_triggered_at<? OR (first_triggered_at=? AND id<?))"
			args = append(args, q.Time, q.Time, q.Before)
		}
	}
	if q.Kind == "current" {
		query += " ORDER BY (severity='critical') DESC,first_triggered_at,id"
	} else {
		query += " ORDER BY first_triggered_at DESC,id DESC"
	}
	query += " LIMIT ?"
	args = append(args, q.Limit+1)
	rows, e := p.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		var r Incident
		var object []byte
		e = rows.Scan(&r.ID, &r.RuleID, &r.Revision, &r.Name, &r.Severity, &r.Source, &object, &r.Status, &r.FirstTriggeredAt, &r.LastTriggeredAt, &r.LastEvaluatedAt, &r.RecoveredAt, &r.ClosedAt, &r.ResolutionReason, &r.LastValue, &r.EvaluationCount, &r.DataStatus)
		if e != nil {
			return nil, unavailable()
		}
		if json.Unmarshal(object, &r.Object) != nil {
			return nil, unavailable()
		}
		out = append(out, r)
	}
	return out, dbError(rows.Err())
}
