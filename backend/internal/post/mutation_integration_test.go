//go:build integration

package post

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

func TestIntegrationMutationSearchRequirements(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, _ := businessstate.NewRepository(db)
	environment, err := state.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.ActivateV2(ctx, "mutation-test"); err != nil {
		t.Fatal(err)
	}
	actor, err := user.NewMySQLRepository(db).Create(ctx, "mutation_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, table := range []string{"business_application_results", "business_event_failures", "business_event_requirements"} {
			_, _ = db.Exec("DELETE FROM "+table+" WHERE event_id IN (SELECT event_id FROM business_outbox WHERE JSON_EXTRACT(payload,'$.actor_id')=?)", actor.ID)
		}
		_, _ = db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload,'$.actor_id')=?", actor.ID)
		_, _ = db.Exec("DELETE FROM business_commands WHERE user_id=?", actor.ID)
		_, _ = db.Exec("DELETE FROM posts WHERE author_id=?", actor.ID)
		_, _ = db.Exec("DELETE FROM users WHERE id=?", actor.ID)
		_, _ = db.Exec("UPDATE business_async_budget SET open_requirements=?, retained_groups=?, retained_bytes=?, parked_messages=?, parked_bytes=? WHERE data_epoch=?", baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, baseline.ParkedMessages, baseline.ParkedBytes, environment.DataEpoch)
		_, _ = db.Exec("UPDATE business_environment SET mode=?,v1_compatibility_enabled=? WHERE singleton=1", environment.Mode, environment.V1Compatibility)
	}()
	events, _ := outbox.NewRepository(db, outbox.Options{EnableV2: true, RetentionAccounting: state})
	repo := NewMySQLRepository(db, RepositoryOptions{Outbox: events, BusinessState: state})
	record, err := repo.Create(ctx, actor.ID, "original", "body")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Update(ctx, record.ID, actor.ID, CreateInput{Title: "edited", Content: "body 2"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, record.ID, actor.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT payload FROM business_outbox WHERE JSON_EXTRACT(payload,'$.post_id')=? ORDER BY id", record.ID)
	if err != nil {
		t.Fatal(err)
	}
	var envelopes []bus.Envelope
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var event bus.Envelope
		event, err = bus.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(envelopes) != 3 {
		t.Fatalf("events=%d", len(envelopes))
	}
	for _, event := range envelopes {
		decision, err := state.CheckEnvelopeForConsumer(ctx, event, businessstate.ConsumerSearch)
		if err != nil || decision != businessstate.AdmissionAccept {
			t.Fatalf("event %s admission=%v err=%v", event.EventType, decision, err)
		}
		app := businessstate.Application{DataEpoch: event.DataEpoch, EventID: event.EventID, Consumer: businessstate.ConsumerSearch, RoutingContractRevision: businessstate.SearchRoutingContractV1, ProjectionGeneration: "test-generation", Result: "applied", ActualVersion: 3}
		for i := 0; i < 2; i++ {
			if err := state.RecordApplication(ctx, app); err != nil {
				t.Fatal(err)
			}
		}
	}
	budget, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil || budget.OpenRequirements != baseline.OpenRequirements {
		t.Fatalf("budget=%+v err=%v", budget, err)
	}
}
