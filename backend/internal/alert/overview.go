package alert

import (
	"context"
	"time"
)

type Recovery struct {
	RuleID      uint64    `json:"rule_id"`
	Revision    uint64    `json:"revision"`
	Severity    string    `json:"severity"`
	RecoveredAt time.Time `json:"recovered_at"`
}
type Summary struct {
	Recoveries        []Recovery `json:"recoveries"`
	WarningFiring     int64      `json:"warning_firing"`
	CriticalFiring    int64      `json:"critical_firing"`
	Pending           int64      `json:"pending"`
	Unknown           int64      `json:"unknown"`
	RecentlyRecovered int64      `json:"recently_recovered"`
	EvaluatorEnabled  bool       `json:"evaluator_enabled"`
	LastSuccessAt     *time.Time `json:"last_success_at"`
}

func (p *Repository) Overview(ctx context.Context, now time.Time, enabled bool) (Summary, error) {
	s := Summary{EvaluatorEnabled: enabled, Recoveries: []Recovery{}}
	err := p.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(severity='warning' AND status='firing'),0),COALESCE(SUM(severity='critical' AND status='firing'),0),COALESCE(SUM(status='recovered' AND recovered_at>=?),0) FROM alert_incidents`, now.Add(-15*time.Minute)).Scan(&s.WarningFiring, &s.CriticalFiring, &s.RecentlyRecovered)
	if err != nil {
		return s, unavailable()
	}
	err = p.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(s.state='pending'),0),COALESCE(SUM(r.enabled=1 AND s.data_status IN ('unknown','stale')),0),MAX(s.last_success_at) FROM alert_rule_states s JOIN alert_rules r ON r.id=s.rule_id WHERE r.deleted_at IS NULL`).Scan(&s.Pending, &s.Unknown, &s.LastSuccessAt)
	if err != nil {
		return s, unavailable()
	}
	rows, err := p.db.QueryContext(ctx, `SELECT rule_id,revision,severity,recovered_at FROM alert_incidents WHERE status='recovered' AND recovered_at>=? ORDER BY recovered_at DESC,id DESC LIMIT 5`, now.Add(-15*time.Minute))
	if err != nil {
		return s, unavailable()
	}
	defer rows.Close()
	for rows.Next() {
		var r Recovery
		if err = rows.Scan(&r.RuleID, &r.Revision, &r.Severity, &r.RecoveredAt); err != nil {
			return s, unavailable()
		}
		s.Recoveries = append(s.Recoveries, r)
	}
	if rows.Err() != nil {
		return s, unavailable()
	}
	return s, nil
}
