//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
)

func TestIntegrationSearchReindexRestorePreparation(t *testing.T) {
	integrationtest.Environment(t)
	cfg, err := config.LoadReindex()
	if err != nil {
		t.Fatalf("LoadReindex() error = %v", err)
	}
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	state, err := businessstate.NewRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	before, err := state.Ensure(ctx)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if before.Status != businessstate.StatusReady {
		t.Fatalf("initial business environment status=%q, want ready", before.Status)
	}
	oldEpoch := before.DataEpoch
	operation := strings.Repeat("a", 64)
	newEpoch := "423e4567-e89b-42d3-a456-426614174099"
	failedOperation := strings.Repeat("b", 64)
	failedEpoch := "523e4567-e89b-42d3-a456-426614174099"

	username := fmt.Sprintf("R24_%d", time.Now().UTC().UnixNano()%1_000_000_000_000_000)
	result, err := database.ExecContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, "$2a$10$integration-placeholder")
	if err != nil {
		t.Fatalf("insert restore user: %v", err)
	}
	authorID, err := result.LastInsertId()
	if err != nil || authorID == 0 {
		t.Fatalf("restore user LastInsertId()=%d error=%v", authorID, err)
	}
	createdAt := time.Date(2026, time.October, 1, 8, 0, 0, 123456, time.UTC)
	result, err = database.ExecContext(ctx, `
		INSERT INTO posts (author_id, title, content, created_at, updated_at, content_revision)
		VALUES (?, ?, ?, ?, ?, 1)`, authorID, "restore phase 24 fixture", "restore epoch fixture", createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert restore post: %v", err)
	}
	postID, err := result.LastInsertId()
	if err != nil || postID == 0 {
		t.Fatalf("restore post LastInsertId()=%d error=%v", postID, err)
	}
	beforeDigest := restoreFactDigest(t, ctx, database, postID)
	beforeCount := restorePostCount(t, ctx, database)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := database.ExecContext(cleanupCtx, `DELETE FROM posts WHERE id=?`, postID); cleanupErr != nil {
			t.Errorf("cleanup restore post: %v", cleanupErr)
		}
		if _, cleanupErr := database.ExecContext(cleanupCtx, `DELETE FROM users WHERE id=?`, authorID); cleanupErr != nil {
			t.Errorf("cleanup restore user: %v", cleanupErr)
		}
		if _, cleanupErr := database.ExecContext(cleanupCtx, `UPDATE business_environment SET data_epoch=?, status='ready', restore_operation_id=NULL, updated_at=UTC_TIMESTAMP(6) WHERE singleton=1`, oldEpoch); cleanupErr != nil {
			t.Errorf("reset business environment after restore test: %v", cleanupErr)
		}
		if _, cleanupErr := database.ExecContext(cleanupCtx, `DELETE FROM business_async_budget WHERE data_epoch IN (?, ?)`, newEpoch, failedEpoch); cleanupErr != nil {
			t.Errorf("cleanup restore budgets: %v", cleanupErr)
		}
		client, clientErr := platform.NewElasticsearch(cfg.Elasticsearch)
		if clientErr != nil {
			t.Errorf("create cleanup Elasticsearch client: %v", clientErr)
			return
		}
		reindexer := search.NewReindexer(search.NewReindexStore(database), search.NewElasticsearchRepository(client), cfg.Elasticsearch.ReindexBatch)
		if _, reindexErr := reindexer.RunForEpoch(cleanupCtx, false, oldEpoch); reindexErr != nil {
			t.Errorf("restore old search generation during cleanup: %v", reindexErr)
		}
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := runWithOptions(cfg, reindexOptions{restoreOperation: operation, expectedOldEpoch: oldEpoch, newEpoch: newEpoch}, logger); err != nil {
		t.Fatalf("runWithOptions(restore) error = %v", err)
	}
	after, err := state.Current(ctx)
	if err != nil {
		t.Fatalf("Current() after restore error = %v", err)
	}
	if after.Status != businessstate.StatusReady || after.DataEpoch != newEpoch || after.RestoreOperationID != "" {
		t.Fatalf("business environment after restore=%#v", after)
	}
	if got := restoreFactDigest(t, ctx, database, postID); got != beforeDigest {
		t.Fatalf("restored MySQL fact digest=%s, want %s", got, beforeDigest)
	}
	if got := restorePostCount(t, ctx, database); got != beforeCount {
		t.Fatalf("restored MySQL post count=%d, want %d", got, beforeCount)
	}

	client, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatal(err)
	}
	searchClient := search.NewElasticsearchRepository(client)
	generation, err := searchClient.ResolveGenerationForEpoch(ctx, newEpoch)
	if err != nil {
		t.Fatalf("ResolveGenerationForEpoch() error = %v", err)
	}
	if epoch, epochErr := searchClient.IndexEpoch(ctx, generation); epochErr != nil || epoch != newEpoch {
		t.Fatalf("restored search generation epoch=%q error=%v, want %q", epoch, epochErr, newEpoch)
	}
	if count, countErr := searchClient.Count(ctx, generation); countErr != nil || count != beforeCount {
		t.Fatalf("restored search count=%d error=%v, want %d", count, countErr, beforeCount)
	}
	oldEvent, err := bus.NewV2PostDeleted(time.Now().UTC(), oldEpoch, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision, decisionErr := state.CheckEnvelope(ctx, oldEvent); decisionErr != nil || decision != bus.AdmissionPark {
		t.Fatalf("old epoch admission=%v error=%v, want park", decision, decisionErr)
	}
	newEvent, err := bus.NewV2PostLiked(time.Now().UTC(), newEpoch, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decision, decisionErr := state.CheckEnvelope(ctx, newEvent); decisionErr != nil || decision != bus.AdmissionAccept {
		t.Fatalf("new epoch admission=%v error=%v, want accept", decision, decisionErr)
	}

	failedCfg := cfg
	failedCfg.Elasticsearch.URL = "http://127.0.0.1:1"
	if err := runWithOptions(failedCfg, reindexOptions{restoreOperation: failedOperation, expectedOldEpoch: newEpoch, newEpoch: failedEpoch}, logger); err == nil {
		t.Fatal("failed restore preparation error = nil")
	}
	pending, err := state.Current(ctx)
	if err != nil {
		t.Fatalf("Current() after failed restore error = %v", err)
	}
	if pending.Status != businessstate.StatusRestoring || pending.DataEpoch != failedEpoch || pending.RestoreOperationID != failedOperation {
		t.Fatalf("failed restore environment=%#v, want restoring pending epoch", pending)
	}
}

func restorePostCount(t *testing.T, ctx context.Context, database *sql.DB) uint64 {
	t.Helper()
	var count uint64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts`).Scan(&count); err != nil {
		t.Fatalf("count restore posts: %v", err)
	}
	return count
}

func restoreFactDigest(t *testing.T, ctx context.Context, database *sql.DB, postID int64) string {
	t.Helper()
	var authorID, revision uint64
	var title, content string
	var createdAt, updatedAt time.Time
	if err := database.QueryRowContext(ctx, `SELECT author_id, title, content, created_at, updated_at, content_revision FROM posts WHERE id=?`, postID).Scan(&authorID, &title, &content, &createdAt, &updatedAt, &revision); err != nil {
		t.Fatalf("read restore post: %v", err)
	}
	value := fmt.Sprintf("%d|%s|%s|%s|%s|%d", authorID, title, content, createdAt.UTC().Format(time.RFC3339Nano), updatedAt.UTC().Format(time.RFC3339Nano), revision)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
