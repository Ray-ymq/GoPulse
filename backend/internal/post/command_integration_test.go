//go:build integration

package post

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

func TestIntegrationPostCommandConcurrentReplayConflictOwnershipAndDeletedResult(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	release := integrationtest.AcquirePostFactsLock(t, database)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	state, err := businessstate.NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := state.Ensure(ctx)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	baseline, err := state.Budget(ctx, environment.DataEpoch)
	if err != nil {
		t.Fatalf("Budget() baseline error = %v", err)
	}
	if err := state.ActivateV2(ctx, "phase-24-02-integration"); err != nil {
		t.Fatalf("ActivateV2() error = %v", err)
	}
	current, err := state.Current(ctx)
	if err != nil || current.Mode != businessstate.ModeV2 || current.Status != businessstate.StatusReady {
		t.Fatalf("current environment=%#v error=%v", current, err)
	}

	suffix := time.Now().UTC().Format("150405000000")
	first, err := user.NewMySQLRepository(database).Create(ctx, "command_"+suffix, "hash")
	if err != nil {
		t.Fatalf("create first user: %v", err)
	}
	second, err := user.NewMySQLRepository(database).Create(ctx, "command_other_"+suffix, "hash")
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_application_results WHERE data_epoch=? AND event_id IN (SELECT event_id FROM business_commands WHERE user_id IN (?, ?))`, environment.DataEpoch, first.ID, second.ID)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_event_requirements WHERE data_epoch=? AND event_id IN (SELECT event_id FROM business_commands WHERE user_id IN (?, ?))`, environment.DataEpoch, first.ID, second.ID)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_outbox WHERE JSON_UNQUOTE(JSON_EXTRACT(payload, '$.actor_id')) IN (?, ?)`, first.ID, second.ID)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM business_commands WHERE data_epoch=? AND user_id IN (?, ?)`, environment.DataEpoch, first.ID, second.ID)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM posts WHERE author_id IN (?, ?)`, first.ID, second.ID)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM users WHERE id IN (?, ?)`, first.ID, second.ID)
		_, _ = database.ExecContext(cleanupCtx, `UPDATE business_async_budget SET open_requirements=?, retained_groups=?, retained_bytes=?, parked_messages=?, parked_bytes=? WHERE data_epoch=?`, baseline.OpenRequirements, baseline.RetainedGroups, baseline.RetainedBytes, baseline.ParkedMessages, baseline.ParkedBytes, environment.DataEpoch)
	}()

	events, err := outbox.NewRepository(database, outbox.Options{RetentionAccounting: state, EnableV2: true})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewMySQLRepository(database, RepositoryOptions{Outbox: events, BusinessState: state})
	service := NewService(repository)
	request := CommandRequest{DataEpoch: environment.DataEpoch, IdempotencyKey: "123e4567-e89b-12d3-a456-426614174010"}
	input := CreateInput{Title: "concurrent title", Content: "same body"}
	const callers = 10
	results := make(chan struct {
		result CreateResult
		err    error
	}, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, createErr := service.CreateWithCommand(ctx, first.ID, input, request)
			results <- struct {
				result CreateResult
				err    error
			}{result: result, err: createErr}
		}()
	}
	wait.Wait()
	close(results)
	var postID uint64
	var commandID, eventID string
	replayed := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent command error = %v cause=%v", result.err, errors.Unwrap(result.err))
		}
		if result.result.Post == nil || result.result.Meta.PostID == 0 {
			t.Fatalf("concurrent command result=%#v", result.result)
		}
		if postID == 0 {
			postID, commandID, eventID = result.result.Meta.PostID, result.result.Meta.CommandID, result.result.Meta.EventID
		} else if result.result.Meta.PostID != postID || result.result.Meta.CommandID != commandID || result.result.Meta.EventID != eventID {
			t.Fatalf("unstable replay result=%#v expected post_id=%d command_id=%s event_id=%s", result.result.Meta, postID, commandID, eventID)
		}
		if result.result.Meta.Replayed {
			replayed++
		}
	}
	if replayed != callers-1 {
		t.Fatalf("replayed=%d, want %d", replayed, callers-1)
	}
	assertCommandCounts(t, ctx, database, environment.DataEpoch, first.ID, postID, eventID, 1, 1, 1, 1)
	var payload []byte
	if err := database.QueryRowContext(ctx, `SELECT payload FROM business_outbox WHERE event_id=?`, eventID).Scan(&payload); err != nil {
		t.Fatalf("read v2 outbox: %v", err)
	}
	envelope, err := bus.Decode(payload)
	if err != nil || envelope.SchemaVersion != bus.SchemaVersionV2 || envelope.DataEpoch != environment.DataEpoch || envelope.CommandID != commandID {
		t.Fatalf("v2 outbox envelope=%#v error=%v payload=%s", envelope, err, payload)
	}

	_, err = service.CreateWithCommand(ctx, first.ID, CreateInput{Title: "different", Content: input.Content}, request)
	if appError, ok := apperror.As(err); !ok || appError.Code != apperror.CodeIdempotencyConflict {
		t.Fatalf("same-user conflict=%v", err)
	}
	otherResult, err := service.CreateWithCommand(ctx, second.ID, input, request)
	if err != nil || otherResult.Post == nil || otherResult.Meta.PostID == postID {
		t.Fatalf("cross-user result=%#v error=%v", otherResult, err)
	}
	assertCommandCounts(t, ctx, database, environment.DataEpoch, second.ID, otherResult.Meta.PostID, otherResult.Meta.EventID, 1, 1, 1, 1)

	verified, err := service.CommandResult(ctx, first.ID, request)
	if err != nil || verified.Post == nil || !verified.Meta.Replayed {
		t.Fatalf("command result=%#v error=%v", verified, err)
	}
	if err := repository.Delete(ctx, postID, first.ID); err != nil {
		t.Fatalf("delete committed post: %v", err)
	}
	deletedResult, err := service.CommandResult(ctx, first.ID, request)
	if err != nil || deletedResult.Post != nil || deletedResult.Meta.ResourceState != "unavailable" || deletedResult.Meta.PostID != postID {
		t.Fatalf("deleted command result=%#v error=%v", deletedResult, err)
	}
}

func assertCommandCounts(t *testing.T, ctx context.Context, database *sql.DB, epoch string, userID, postID uint64, eventID string, wantCommands, wantPosts, wantOutbox, wantRequirements int) {
	t.Helper()
	var commands, posts, outboxRows, requirements int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_commands WHERE data_epoch=? AND user_id=? AND post_id=?`, epoch, userID, postID).Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE id=?`, postID).Scan(&posts); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_outbox WHERE event_id=?`, eventID).Scan(&outboxRows); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_event_requirements WHERE data_epoch=? AND event_id=?`, epoch, eventID).Scan(&requirements); err != nil {
		t.Fatal(err)
	}
	if commands != wantCommands || posts != wantPosts || outboxRows != wantOutbox || requirements != wantRequirements {
		t.Fatalf("counts commands=%d posts=%d outbox=%d requirements=%d, want %d/%d/%d/%d", commands, posts, outboxRows, requirements, wantCommands, wantPosts, wantOutbox, wantRequirements)
	}
}
