package alert

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"math"
	"sync"
	"time"
)

type Samples interface {
	AlertPoints(context.Context, string, map[string]string, time.Time, time.Time) ([]metricquery.Point, error)
}
type Counts interface {
	AlertCount(context.Context, map[string]string, time.Time, time.Time) (int64, error)
}
type Scheduler struct {
	logs    Counts
	events  Counts
	repo    *Repository
	samples Samples
}

func NewScheduler(repo *Repository, samples Samples) *Scheduler {
	return &Scheduler{repo: repo, samples: samples}
}

func (s *Scheduler) WithCounts(logs, events Counts) *Scheduler {
	s.logs = logs
	s.events = events
	return s
}

// A bounded round completes before the next tick; failures are deliberately local.
func (s *Scheduler) Run(ctx context.Context) {
	defer func() { _ = recover() }()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.round(ctx)
			var jitter [1]byte
			_, _ = rand.Read(jitter[:])
			timer.Reset(30*time.Second + time.Duration(jitter[0])*time.Millisecond)
		}
	}
}
func (s *Scheduler) round(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	rows, e := s.repo.db.QueryContext(ctx, `SELECT r.id FROM alert_rules r JOIN alert_rule_states s ON s.rule_id=r.id WHERE r.enabled=1 AND r.deleted_at IS NULL AND s.next_evaluation_at<=UTC_TIMESTAMP(6) AND (s.lease_until IS NULL OR s.lease_until<=UTC_TIMESTAMP(6)) ORDER BY r.id LIMIT 32`)
	if e != nil {
		return
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
		if rows.Scan(&id) != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	jobs := make(chan uint64)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				func() { defer func() { _ = recover() }(); s.evaluate(ctx, id) }()
			}
		}()
	}
	for _, id := range ids {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- id:
		}
	}
	close(jobs)
	wg.Wait()
}
func (s *Scheduler) evaluate(ctx context.Context, id uint64) {
	ctx, cancelEvaluation := context.WithTimeout(ctx, 8*time.Second)
	defer cancelEvaluation()
	if ctx.Err() != nil {
		return
	}
	var token [16]byte
	if _, e := rand.Read(token[:]); e != nil {
		return
	}
	owner := hex.EncodeToString(token[:])
	res, e := s.repo.db.ExecContext(ctx, `UPDATE alert_rule_states s JOIN alert_rules r ON r.id=s.rule_id SET s.lease_owner=?,s.lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 SECOND) WHERE s.rule_id=? AND r.enabled=1 AND r.deleted_at IS NULL AND s.next_evaluation_at<=UTC_TIMESTAMP(6) AND (s.lease_until IS NULL OR s.lease_until<=UTC_TIMESTAMP(6))`, owner, id)
	if e != nil {
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return
	}
	r, e := s.repo.Get(ctx, id)
	if e != nil {
		return
	}
	cutoff := time.Now().UTC().Add(-15 * time.Second)
	v, known := s.value(ctx, r, cutoff)
	if ctx.Err() != nil {
		return
	}
	_ = s.repo.apply(ctx, r, owner, cutoff, v, known)
}
func (p *Repository) apply(ctx context.Context, claimed Rule, owner string, cutoff time.Time, value float64, known bool) error {
	tx, e := p.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := scanRule(tx.QueryRowContext(ctx, "SELECT "+ruleColumns+ruleFrom+"WHERE r.id=? AND r.deleted_at IS NULL FOR UPDATE", claimed.ID))
	if e != nil {
		return e
	}
	var actual string
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT lease_owner,COALESCE(lease_until>UTC_TIMESTAMP(6),FALSE) FROM alert_rule_states WHERE rule_id=?`, r.ID).Scan(&actual, &valid)
	if e != nil {
		return e
	}
	if actual != owner || !valid || r.Revision != claimed.Revision || !*r.Enabled {
		return nil
	}
	now := time.Now().UTC()
	st := r.State
	st.LastEvaluatedAt = &cutoff
	st.ErrorCode = ""
	st.DataStatus = "ok"
	if !known {
		st.ErrorCode = r.Source + "_unknown"
		st.DataStatus = "unknown"
		if st.State == "firing" {
			st.DataStatus = "stale"
		} else {
			st.State = "normal"
			st.PendingSince = nil
		}
	} else {
		st.LastSuccessAt = &cutoff
		st.LastValue = &value
		if !condition(r, value) {
			if st.ActiveIncidentID != nil {
				id := *st.ActiveIncidentID
				_, e = tx.ExecContext(ctx, `UPDATE alert_incidents SET status='recovered',recovered_at=?,last_evaluated_at=?,observed_value=?,evaluation_count=evaluation_count+1 WHERE id=?`, cutoff, cutoff, value, id)
				if e == nil {
					e = audit(ctx, tx, r, 0, "alert.recover", "", "", id)
				}
			}
			st.State = "normal"
			st.PendingSince = nil
			st.ActiveIncidentID = nil
		} else if st.State == "firing" {
			_, e = tx.ExecContext(ctx, `UPDATE alert_incidents SET last_triggered_at=?,last_evaluated_at=?,observed_value=?,evaluation_count=evaluation_count+1 WHERE id=?`, cutoff, cutoff, value, *st.ActiveIncidentID)
		} else {
			// A restart gap is not evidence of continuous true evaluations.
			if st.PendingSince == nil || r.State.LastEvaluatedAt == nil || cutoff.Sub(*r.State.LastEvaluatedAt) > 65*time.Second {
				st.PendingSince = &cutoff
			}
			st.State = "pending"
			duration, _ := time.ParseDuration(r.For)
			if cutoff.Sub(*st.PendingSince) >= duration {
				object, _ := json.Marshal(r.Selector)
				var res interface{ LastInsertId() (int64, error) }
				res, e = tx.ExecContext(ctx, `INSERT INTO alert_incidents(rule_id,revision,name,severity,source,object,status,first_triggered_at,last_triggered_at,last_evaluated_at,observed_value,evaluation_count) VALUES(?,?,?,?,?,?,'firing',?,?,?,?,1)`, r.ID, r.Revision, r.Name, r.Severity, r.Source, object, cutoff, cutoff, cutoff, value)
				if e == nil {
					id, _ := res.LastInsertId()
					uid := uint64(id)
					st.ActiveIncidentID = &uid
					st.State = "firing"
					st.PendingSince = nil
					e = audit(ctx, tx, r, 0, "alert.trigger", "", "", uid)
				}
			}
		}
	}
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE alert_rule_states SET state=?,data_status=?,pending_since=?,active_incident_id=?,observed_value=?,last_evaluated_at=?,last_success_at=?,error_code=?,lease_owner='',lease_until=NULL,next_evaluation_at=? WHERE rule_id=?`, st.State, st.DataStatus, st.PendingSince, st.ActiveIncidentID, st.LastValue, st.LastEvaluatedAt, st.LastSuccessAt, st.ErrorCode, now.Add(30*time.Second), r.ID)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Recover at the adapter boundary so a panic still persists a source-local unknown
// and releases the lease through the ordinary state/audit transaction.
func (s *Scheduler) value(ctx context.Context, r Rule, cutoff time.Time) (v float64, known bool) {
	defer func() {
		if recover() != nil {
			v = 0
			known = false
		}
	}()
	in := r.Input
	in.Enabled = nil
	if Validate(in, false) != nil {
		return 0, false
	}
	w, e := time.ParseDuration(r.Window)
	if e != nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if r.Source == "metrics" {
		points, e := s.samples.AlertPoints(ctx, r.Selector.Metric, r.Selector.Labels, cutoff.Add(-w), cutoff)
		if e != nil {
			return 0, false
		}
		return Reduce(points, r.Reducer, cutoff)
	}
	adapter := s.logs
	if r.Source == "events" {
		adapter = s.events
	}
	if adapter == nil {
		return 0, false
	}
	n, e := adapter.AlertCount(ctx, r.Selector.Labels, cutoff.Add(-w), cutoff)
	v = float64(n)
	return v, e == nil && n >= 0 && n <= 9007199254740991 && !math.IsNaN(v)
}
