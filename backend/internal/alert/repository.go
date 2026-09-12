package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/go-sql-driver/mysql"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db} }

const ruleColumns = `r.id,r.name,r.enabled,r.severity,r.source,r.selector,r.reducer,r.operator,r.threshold,r.window_seconds,r.for_seconds,r.revision,r.created_by,r.updated_by,r.created_at,r.updated_at,s.state,s.data_status,s.pending_since,s.active_incident_id,s.observed_value,s.last_evaluated_at,s.last_success_at,s.error_code`
const ruleFrom = ` FROM alert_rules r JOIN alert_rule_states s ON s.rule_id=r.id `

type scanner interface{ Scan(...any) error }

func scanRule(row scanner) (Rule, error) {
	var r Rule
	var selector []byte
	var w, f int
	var enabled bool
	var threshold float64
	e := row.Scan(&r.ID, &r.Name, &enabled, &r.Severity, &r.Source, &selector, &r.Reducer, &r.Operator, &threshold, &w, &f, &r.Revision, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &r.State.State, &r.State.DataStatus, &r.State.PendingSince, &r.State.ActiveIncidentID, &r.State.LastValue, &r.State.LastEvaluatedAt, &r.State.LastSuccessAt, &r.State.ErrorCode)
	r.Enabled = &enabled
	r.Threshold = &threshold
	r.Window = durationName(w)
	r.For = durationName(f)
	if e == nil {
		e = json.Unmarshal(selector, &r.Selector)
	}
	return r, e
}
func durationName(s int) string {
	switch s {
	case 0:
		return "0s"
	case 60:
		return "1m"
	case 300:
		return "5m"
	case 900:
		return "15m"
	}
	return ""
}
func seconds(s string) int { d, _ := time.ParseDuration(s); return int(d.Seconds()) }
func dbError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, sql.ErrNoRows) {
		return apperror.New(apperror.CodeAlertNotFound, "alert rule was not found")
	}
	var m *mysql.MySQLError
	if errors.As(e, &m) && (m.Number == 1062 || m.Number == 1213 || m.Number == 1205) {
		return apperror.New(apperror.CodeAlertConflict, "alert rule conflicts with the current state")
	}
	return unavailable()
}
func (p *Repository) Get(ctx context.Context, id uint64) (Rule, error) {
	r, e := scanRule(p.db.QueryRowContext(ctx, "SELECT "+ruleColumns+ruleFrom+"WHERE r.id=? AND r.deleted_at IS NULL", id))
	return r, dbError(e)
}
func audit(ctx context.Context, tx *sql.Tx, r Rule, actor uint64, action, requestID, reason string, incident uint64) error {
	d, e := user.RuleAuditDetails(action, r.ID, r.Revision, r.Severity, r.Source)
	resource := r.ID
	if incident != 0 {
		d, e = user.AlertAuditDetails(action, r.ID, r.Revision, r.Severity, r.Source, reason)
		resource = incident
	}
	if e != nil {
		return e
	}
	return user.AppendAlertAudit(ctx, tx, actor, action, resource, requestID, d)
}
func closeIncident(ctx context.Context, tx *sql.Tx, r Rule, reason, requestID string, actor uint64, now time.Time) error {
	if r.State.ActiveIncidentID == nil {
		return nil
	}
	id := *r.State.ActiveIncidentID
	_, e := tx.ExecContext(ctx, `UPDATE alert_incidents SET status='closed',closed_at=?,resolution_reason=? WHERE id=? AND status='firing'`, now, reason, id)
	if e != nil {
		return e
	}
	return audit(ctx, tx, r, actor, "alert.close", requestID, reason, id)
}

// The bootstrap singleton is a persistent admission mutex for the global limit.
// Every mutation takes it first, before rule/state locks, including actor recheck.
func authorize(ctx context.Context, tx *sql.Tx, actor uint64) error {
	var id uint64
	if e := tx.QueryRowContext(ctx, `SELECT user_id FROM bootstrap_super_admin WHERE singleton=1 FOR UPDATE`).Scan(&id); e != nil {
		return e
	}
	var role string
	if e := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=? FOR UPDATE`, actor).Scan(&role); e != nil {
		return e
	}
	if role != "super_admin" {
		return apperror.New(apperror.CodePermissionDenied, "permission denied")
	}
	return nil
}
func (p *Repository) Mutate(ctx context.Context, id, actor uint64, action, requestID string, in Input) (Rule, error) {
	create := action == "create"
	if create || action == "update" {
		if e := Validate(in, create); e != nil {
			return Rule{}, e
		}
	} else if in.Revision == 0 {
		return Rule{}, validation()
	}
	tx, e := p.db.BeginTx(ctx, nil)
	if e != nil {
		return Rule{}, unavailable()
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, actor); e != nil {
		if a, ok := apperror.As(e); ok {
			return Rule{}, a
		}
		return Rule{}, dbError(e)
	}
	now := time.Now().UTC()
	r := Rule{Input: in, ID: id, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if create {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_rules WHERE deleted_at IS NULL`).Scan(&n); e != nil {
			return Rule{}, dbError(e)
		}
		if n >= 32 {
			return Rule{}, apperror.New(apperror.CodeAlertRuleLimit, "alert rule limit reached")
		}
		r.Revision = 1
		b, _ := json.Marshal(in.Selector)
		res, err := tx.ExecContext(ctx, `INSERT INTO alert_rules(name,enabled,severity,source,selector,reducer,operator,threshold,window_seconds,for_seconds,revision,created_by,updated_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,1,?,?,?,?)`, in.Name, *in.Enabled, in.Severity, in.Source, b, in.Reducer, in.Operator, *in.Threshold, seconds(in.Window), seconds(in.For), actor, actor, now, now)
		if err != nil {
			return Rule{}, dbError(err)
		}
		v, _ := res.LastInsertId()
		r.ID = uint64(v)
		state := "normal"
		if !*in.Enabled {
			state = "disabled"
		}
		r.State = State{State: state, DataStatus: "unknown"}
		_, e = tx.ExecContext(ctx, `INSERT INTO alert_rule_states(rule_id,state,next_evaluation_at) VALUES(?,?,?)`, r.ID, state, now)
	} else {
		r, e = scanRule(tx.QueryRowContext(ctx, "SELECT "+ruleColumns+ruleFrom+"WHERE r.id=? AND r.deleted_at IS NULL FOR UPDATE", id))
		if e != nil {
			return Rule{}, dbError(e)
		}
		if r.Revision != in.Revision {
			return Rule{}, apperror.New(apperror.CodeAlertConflict, "alert revision conflict")
		}
		reason := "rule_updated"
		if action == "disable" {
			reason = "rule_disabled"
		}
		if action == "delete" {
			reason = "rule_deleted"
		}
		if action != "enable" {
			if e = closeIncident(ctx, tx, r, reason, requestID, actor, now); e != nil {
				return Rule{}, dbError(e)
			}
		}
		if action == "update" {
			in.Enabled = r.Enabled
			r.Input = in
		} else if action == "enable" || action == "disable" || action == "delete" {
			b := action == "enable"
			r.Enabled = &b
		} else {
			return Rule{}, validation()
		}
		r.Revision++
		r.UpdatedBy = actor
		r.UpdatedAt = now
		b, _ := json.Marshal(r.Selector)
		var deleted any
		if action == "delete" {
			deleted = now
		}
		_, e = tx.ExecContext(ctx, `UPDATE alert_rules SET name=?,enabled=?,severity=?,source=?,selector=?,reducer=?,operator=?,threshold=?,window_seconds=?,for_seconds=?,revision=?,updated_by=?,updated_at=?,deleted_at=? WHERE id=?`, r.Name, *r.Enabled, r.Severity, r.Source, b, r.Reducer, r.Operator, *r.Threshold, seconds(r.Window), seconds(r.For), r.Revision, actor, now, deleted, id)
		state := "normal"
		if !*r.Enabled {
			state = "disabled"
		}
		if action == "enable" && r.State.State == "firing" {
			state = "firing"
		} else {
			r.State = State{State: state, DataStatus: "unknown"}
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE alert_rule_states SET state=?,data_status=?,pending_since=?,active_incident_id=?,observed_value=?,last_evaluated_at=?,last_success_at=?,error_code=?,lease_owner='',lease_until=NULL,next_evaluation_at=? WHERE rule_id=?`, r.State.State, r.State.DataStatus, r.State.PendingSince, r.State.ActiveIncidentID, r.State.LastValue, r.State.LastEvaluatedAt, r.State.LastSuccessAt, r.State.ErrorCode, now, id)
		}
	}
	if e != nil {
		return Rule{}, dbError(e)
	}
	if e = audit(ctx, tx, r, actor, "rule."+action, requestID, "", 0); e != nil {
		return Rule{}, dbError(e)
	}
	return r, dbError(tx.Commit())
}
