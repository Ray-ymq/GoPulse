//go:build integration

package post_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

func TestIntegrationDetailQueryCountAndEpochGuard(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx := context.Background()
	author, viewer, id := createReadCostFixture(t, ctx, db)
	defer cleanupReadCostFixture(t, db, author, viewer, id)
	state, _ := businessstate.NewRepository(db)
	counter := &readCostDatabase{database: db}
	repo := post.NewMySQLRepository(counter, post.RepositoryOptions{BusinessState: state})
	redis := platform.NewRedis(cfg.Redis)
	defer redis.Close()
	service := post.NewService(repo).WithBodyCache(rediscache.NewPostBodyRepository(redis, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout), nil)
	defer service.Shutdown(ctx)
	for _, want := range []int{3, 2} {
		counter.reset()
		if _, err := service.Detail(ctx, id, viewer); err != nil {
			t.Fatal(err)
		}
		calls, _, _ := counter.calls()
		if calls != want {
			t.Fatalf("SQL count=%d want=%d", calls, want)
		}
		t.Logf("detail SQL count=%d", calls)
	}
	read, err := repo.ReadContext(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	key := post.BodyKey{Epoch: read.Epoch, PostID: id, ContentRevision: read.ContentRevision}
	if _, err := db.Exec("UPDATE posts SET content_revision=content_revision+1 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBody(ctx, key); !errors.Is(err, post.ErrReadContextChanged) {
		t.Fatalf("old revision=%v", err)
	}
	key.Epoch = "123e4567-e89b-12d3-a456-426614174999"
	key.ContentRevision++
	if _, err := repo.FindBody(ctx, key); !errors.Is(err, post.ErrReadContextChanged) {
		t.Fatalf("old epoch=%v", err)
	}
	key.Epoch = read.Epoch
	restoredEpoch := "123e4567-e89b-12d3-a456-426614174999"
	operation := strings.Repeat("b", 64)
	if err := state.BeginRestore(ctx, operation, read.Epoch, restoredEpoch); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec("UPDATE business_environment SET data_epoch=?,status='ready',restore_operation_id=NULL WHERE singleton=1", read.Epoch)
		_, _ = db.Exec("DELETE FROM business_async_budget WHERE data_epoch=?", restoredEpoch)
	}()
	if _, err := repo.FindBody(ctx, key); !errors.Is(err, post.ErrReadContextChanged) {
		t.Fatalf("restoring epoch accepted old body: %v", err)
	}
	if err := state.MarkReady(ctx, operation, restoredEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBody(ctx, key); !errors.Is(err, post.ErrReadContextChanged) {
		t.Fatalf("restored epoch accepted old body: %v", err)
	}
	if _, err := db.Exec("UPDATE business_environment SET data_epoch=?,status='ready',restore_operation_id=NULL WHERE singleton=1", read.Epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM posts WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBody(ctx, key); !errors.Is(err, post.ErrNotFound) {
		t.Fatalf("deleted=%v", err)
	}
}
