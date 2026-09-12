package alert

import (
	"context"
	"database/sql"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"github.com/go-sql-driver/mysql"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Explicitly gated to the verifier's disposable database. These controlled-clock
// repository checks supplement, never replace, the real Metrics acceptance.
func TestOwnedMySQLStateAndLease(t *testing.T) {
	dsn := os.Getenv("ALERT_TEST_DSN")
	if dsn == "" {
		t.Skip("run by verify-alerts.sh in its owned MySQL database")
	}
	project := os.Getenv("ALERT_TEST_PROJECT")
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !regexp.MustCompile(`^gopulse-p1401-[a-f0-9]{12}$`).MatchString(project) || cfg.DBName != "gopulse_"+strings.TrimPrefix(project, "gopulse-p1401-") {
		t.Fatal("refusing unowned database")
	}
	db, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal("open owned database")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var actor uint64
	if e = db.QueryRowContext(ctx, `SELECT user_id FROM bootstrap_super_admin WHERE singleton=1`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	repo := NewRepository(db)
	in := validInput()
	in.Name = "controlled-state"
	in.For = "1m"
	*in.Enabled = false
	r, e := repo.Mutate(ctx, 0, actor, "create", "", in)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		_, _ = db.Exec(`DELETE FROM management_audit_events WHERE (resource_type='alert' AND resource_id IN (SELECT CAST(id AS CHAR) FROM alert_incidents WHERE rule_id=?)) OR (resource_type='rule' AND resource_id=?)`, r.ID, r.ID)
		_, _ = db.Exec(`DELETE FROM alert_rule_states WHERE rule_id=?`, r.ID)
		_, _ = db.Exec(`DELETE FROM alert_incidents WHERE rule_id=?`, r.ID)
		_, _ = db.Exec(`DELETE FROM alert_rules WHERE id=?`, r.ID)
	}()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE alert_rules SET enabled=1 WHERE id=?`, r.ID)
	exec(`UPDATE alert_rule_states SET state='normal',next_evaluation_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR) WHERE rule_id=?`, r.ID)
	now := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Microsecond)
	get := func() Rule {
		t.Helper()
		r, e := repo.Get(ctx, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	apply := func(at time.Time, v float64, known bool) {
		t.Helper()
		claimed := get()
		exec(`UPDATE alert_rule_states SET lease_owner='owned',lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 SECOND) WHERE rule_id=?`, r.ID)
		if e := repo.apply(ctx, claimed, "owned", at, v, known); e != nil {
			t.Fatal(e)
		}
		exec(`UPDATE alert_rule_states SET next_evaluation_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 HOUR) WHERE rule_id=?`, r.ID)
	}
	apply(now, 0, true)
	if get().State.State != "pending" {
		t.Fatal("expected pending")
	}
	apply(now.Add(30*time.Second), 0, false)
	if get().State.State != "normal" || get().State.PendingSince != nil {
		t.Fatal("unknown did not break pending")
	}
	apply(now.Add(60*time.Second), 0, true)
	apply(now.Add(90*time.Second), 0, true)
	apply(now.Add(120*time.Second), 0, true)
	firing := get()
	if firing.State.State != "firing" || firing.State.ActiveIncidentID == nil {
		t.Fatal("continuous true did not fire")
	}
	// A committed claim cannot be replayed, and an expired owner cannot commit.
	if e := repo.apply(ctx, firing, "owned", now.Add(121*time.Second), 1, true); e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE alert_rule_states SET lease_owner='expired',lease_until=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE rule_id=?`, r.ID)
	if e := repo.apply(ctx, firing, "expired", now.Add(122*time.Second), 1, true); e != nil {
		t.Fatal(e)
	}
	if get().State.State != "firing" {
		t.Fatal("stale lease committed recovery")
	}
	apply(now.Add(150*time.Second), 0, false)
	if get().State.DataStatus != "stale" {
		t.Fatal("firing unknown not stale")
	}
	apply(now.Add(180*time.Second), 0, true)
	var n int
	if e = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_incidents WHERE rule_id=?`, r.ID).Scan(&n); e != nil || n != 1 {
		t.Fatal("duplicate incident", e, n)
	}
	_, e = db.ExecContext(ctx, `INSERT INTO alert_incidents(rule_id,revision,name,severity,source,object,status,first_triggered_at,last_triggered_at,last_evaluated_at,observed_value,evaluation_count) SELECT rule_id,revision,name,severity,source,object,status,first_triggered_at,last_triggered_at,last_evaluated_at,observed_value,evaluation_count FROM alert_incidents WHERE rule_id=?`, r.ID)
	if e == nil {
		t.Fatal("database accepted duplicate active incident")
	}
	apply(now.Add(210*time.Second), 1, true)
	if get().State.State != "normal" || get().State.ActiveIncidentID != nil {
		t.Fatal("false did not recover")
	}
	var status string
	if e = db.QueryRowContext(ctx, `SELECT status FROM alert_incidents WHERE rule_id=?`, r.ID).Scan(&status); e != nil || status != "recovered" {
		t.Fatal("incident not recovered")
	}
	exec(`UPDATE alert_rule_states SET lease_owner='expired',lease_until=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND),next_evaluation_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE rule_id=?`, r.ID)
	NewScheduler(repo, unavailableSamples{}).evaluate(ctx, r.ID)
	var owner string
	var until sql.NullTime
	if e = db.QueryRowContext(ctx, `SELECT lease_owner,lease_until FROM alert_rule_states WHERE rule_id=?`, r.ID).Scan(&owner, &until); e != nil || owner != "" || until.Valid || get().State.LastEvaluatedAt.Before(now.Add(210*time.Second)) {
		t.Fatal("expired lease was not reclaimed and committed", e)
	}
	t.Log("expired claim reclaimed by scheduler; pending -> unknown/normal -> pending -> firing -> stale -> continued -> recovered; expired/replayed leases rejected; unique active invariant enforced")
}

type unavailableSamples struct{}

func (unavailableSamples) AlertPoints(context.Context, string, map[string]string, time.Time, time.Time) ([]metricquery.Point, error) {
	return nil, nil
}
