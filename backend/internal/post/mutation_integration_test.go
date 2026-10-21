//go:build integration

package post

import (
	"context"
	"database/sql"
	"errors"
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
	var baselineRequirements int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM business_event_requirements WHERE data_epoch=?", environment.DataEpoch).Scan(&baselineRequirements); err != nil {
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
	charged, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec("DELETE FROM post_search_tombstones WHERE data_epoch=? AND post_id=?", environment.DataEpoch, record.ID)
	}()
	limitedState, err := businessstate.NewRepository(db, businessstate.WithOpenRequirementsLimit(baseline.OpenRequirements+1))
	if err != nil {
		t.Fatal(err)
	}
	limitedRepo := NewMySQLRepository(db, RepositoryOptions{Outbox: events, BusinessState: limitedState})
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation+"_budget_rollback", func(t *testing.T) {
			if operation == "update" {
				_, err = limitedRepo.Update(ctx, record.ID, actor.ID, CreateInput{Title: "rejected", Content: "rejected"})
			} else {
				err = limitedRepo.Delete(ctx, record.ID, actor.ID)
			}
			if !errors.Is(err, businessstate.ErrBudgetExceeded) {
				t.Fatalf("预算已满时应回滚: %v", err)
			}
			var title, content string
			var revision uint64
			if err := db.QueryRowContext(ctx, "SELECT title,content,content_revision FROM posts WHERE id=?", record.ID).Scan(&title, &content, &revision); err != nil || title != "original" || content != "body" || revision != 1 {
				t.Fatalf("超限后帖子发生变化: title=%s content=%s revision=%d err=%v", title, content, revision, err)
			}
			for _, query := range []struct {
				sql  string
				want int
			}{
				{"SELECT COUNT(*) FROM business_outbox WHERE JSON_EXTRACT(payload,'$.post_id')=?", 1},
				{"SELECT COUNT(*) FROM business_event_requirements WHERE event_id IN (SELECT event_id FROM business_outbox WHERE JSON_EXTRACT(payload,'$.post_id')=?)", 1},
				{"SELECT COUNT(*) FROM post_search_tombstones WHERE post_id=?", 0},
			} {
				var count int
				if err := db.QueryRowContext(ctx, query.sql, record.ID).Scan(&count); err != nil || count != query.want {
					t.Fatalf("超限后留下了部分写入: count=%d want=%d err=%v", count, query.want, err)
				}
			}
			var requirements int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM business_event_requirements WHERE data_epoch=?", environment.DataEpoch).Scan(&requirements); err != nil {
				t.Fatal(err)
			}
			budget, err := state.Budget(ctx, environment.DataEpoch)
			if err != nil || budget.OpenRequirements != charged.OpenRequirements || budget.RetainedGroups != charged.RetainedGroups || budget.RetainedBytes != charged.RetainedBytes || requirements != baselineRequirements+1 {
				t.Fatalf("超限后预算或需求不一致: budget=%+v requirements=%d err=%v", budget, requirements, err)
			}
		})
	}
	for index, operation := range []string{"update", "delete"} {
		t.Run(operation+"_outbox_before_budget_lock", func(t *testing.T) {
			inserted, release := make(chan struct{}), make(chan struct{})
			gatedRepo := NewMySQLRepository(db, RepositoryOptions{Outbox: &pausedMutationOutbox{Writer: events, inserted: inserted, release: release}, BusinessState: state})
			finished := make(chan error, 1)
			go func() {
				if operation == "update" {
					_, err := gatedRepo.Update(ctx, record.ID, actor.ID, CreateInput{Title: "edited", Content: "body 2"})
					finished <- err
				} else {
					finished <- gatedRepo.Delete(ctx, record.ID, actor.ID)
				}
			}()
			defer func() {
				close(release)
				if err := <-finished; err != nil {
					t.Errorf("事务未能提交: %v", err)
				}
				budget, err := state.Budget(ctx, environment.DataEpoch)
				if err != nil || budget.OpenRequirements != baseline.OpenRequirements+uint64(index)+2 {
					t.Errorf("提交后预算不一致: budget=%+v err=%v", budget, err)
				}
			}()
			select {
			case <-inserted:
			case <-ctx.Done():
				t.Fatal("未到达 Outbox 写入位置")
			}
			probe, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Rollback()
			var open uint64
			if err := probe.QueryRowContext(ctx, "SELECT open_requirements FROM business_async_budget WHERE data_epoch=? FOR UPDATE NOWAIT", environment.DataEpoch).Scan(&open); err != nil {
				t.Fatalf("Outbox 写入期间预算行已被锁住: %v", err)
			}
			if open != baseline.OpenRequirements+uint64(index)+1 {
				t.Fatalf("提交前预算发生变化: open=%d", open)
			}
		})
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

type pausedMutationOutbox struct {
	outbox.Writer
	inserted chan<- struct{}
	release  <-chan struct{}
}

func (writer *pausedMutationOutbox) Insert(ctx context.Context, executor outbox.Executor, event bus.Envelope) error {
	if err := writer.Writer.Insert(ctx, executor, event); err != nil {
		return err
	}
	close(writer.inserted)
	select {
	case <-writer.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
