//go:build integration

package businessstate_test

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationRetentionExpiresOnceWithoutExtraPeriod(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	state, _ := businessstate.NewRepository(db)
	state.WithClock(func() time.Time { return now })
	environment, err := state.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatal(err)
	}
	event, _ := bus.NewV2PostUpdated(now, environment.DataEpoch, 1, 999, 2)
	commandID, _ := bus.NewUUID()
	defer func() {
		for _, table := range []string{"business_outbox", "business_application_results", "business_event_failures", "business_event_requirements", "business_commands"} {
			db.Exec("DELETE FROM "+table+" WHERE event_id=?", event.EventID)
		}
		db.Exec("UPDATE business_async_budget SET open_requirements=?,retained_groups=?,retained_bytes=? WHERE data_epoch=?", baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, environment.DataEpoch)
	}()
	events, _ := outbox.NewRepository(db, outbox.Options{Clock: func() time.Time { return now }, RetentionAccounting: state})
	if err := events.Insert(ctx, db, event); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE business_outbox SET status='published',published_at=? WHERE event_id=?", now, event.EventID); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterRequirement(ctx, businessstate.Requirement{DataEpoch: event.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch, RoutingContractRevision: businessstate.SearchRoutingContractV1, ApplicableObjectVersion: 2, PayloadBytes: 128}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO business_commands(data_epoch,command_id,user_id,command_type,request_digest,event_id,status,committed_at,retained_until,created_at,updated_at) VALUES(?,?,1,'post.create',?,?,'succeeded',?,?,?,?)`, event.DataEpoch, commandID, make([]byte, 32), event.EventID, now, now.Add(businessstate.RetentionPeriod), now, now); err != nil {
		t.Fatal(err)
	}
	if err := state.RecordApplication(ctx, businessstate.Application{DataEpoch: event.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch, RoutingContractRevision: businessstate.SearchRoutingContractV1, ProjectionGeneration: "retention-test", Result: "applied", ActualVersion: 2, CompletedAt: now}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(businessstate.RetentionPeriod - time.Microsecond)
	if count, err := events.CleanupPublished(ctx, now.Add(-businessstate.RetentionPeriod), 500); err != nil || count != 0 {
		t.Fatalf("early cleanup=%d err=%v", count, err)
	}
	if count, err := state.CleanupExpired(ctx, 500); err != nil || count != 0 {
		t.Fatalf("early evidence cleanup=%d err=%v", count, err)
	}
	now = now.Add(2 * time.Microsecond)
	if count, err := events.CleanupPublished(ctx, now.Add(-businessstate.RetentionPeriod), 500); err != nil || count != 1 {
		t.Fatalf("expired cleanup=%d err=%v", count, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := events.CleanupPublished(ctx, now.Add(-businessstate.RetentionPeriod), 500); err != nil {
			t.Fatal(err)
		}
		if _, err := state.CleanupExpired(ctx, 500); err != nil {
			t.Fatal(err)
		}
	}
	budget, err := state.Budget(ctx, event.DataEpoch)
	if err != nil || budget.OpenRequirements != baseline.OpenRequirements || budget.RetainedGroups != baseline.RetainedGroups || budget.RetainedBytes != baseline.RetainedBytes {
		t.Fatalf("budget=%+v baseline=%+v err=%v", budget, baseline, err)
	}
	for _, table := range []string{"business_commands", "business_application_results", "business_event_requirements"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE event_id=?", event.EventID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("table=%s remaining=%d err=%v", table, count, err)
		}
	}
}
