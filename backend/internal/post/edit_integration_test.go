//go:build integration

package post_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

type failedEditOutbox struct{}

func (failedEditOutbox) Insert(context.Context, outbox.Executor, bus.Envelope) error {
	return errors.New("outbox unavailable")
}

type staleEditCache struct{ projection post.PublicProjection }

func (c *staleEditCache) Get(context.Context, uint64) (post.PublicProjection, bool, error) {
	return c.projection, true, nil
}
func (c *staleEditCache) Set(context.Context, post.PublicProjection) error {
	return errors.New("redis unavailable")
}
func (c *staleEditCache) Invalidate(context.Context, uint64) error {
	return errors.New("redis unavailable")
}

func TestIntegrationEditTransactionNoopAndStaleCache(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx := context.Background()
	actor, err := user.NewMySQLRepository(db).Create(ctx, "edit_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM users WHERE id=?", actor.ID)
	events, err := outbox.NewRepository(db, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repo := post.NewMySQLRepositoryWithOutbox(db, events)
	record, err := repo.Create(ctx, actor.ID, "oldword", "old content")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM posts WHERE id=?", record.ID)
	defer db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload, '$.post_id')=?", record.ID)
	projection, err := repo.FindPublicByID(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	reads := post.NewService(repo, &staleEditCache{projection: projection})
	updated, err := reads.Update(ctx, record.ID, actor.ID, post.CreateInput{Title: " newword ", Content: " new content "})
	if err != nil || updated.Title != "newword" || updated.ContentRevision != 2 || updated.EditedAt == nil {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	for i := 0; i < 2; i++ {
		got, err := reads.Detail(ctx, record.ID, actor.ID)
		if err != nil || got.Content != "new content" || got.ContentRevision != 2 {
			t.Fatalf("stale cache accepted: %+v %v", got, err)
		}
	}
	same, err := reads.Update(ctx, record.ID, actor.ID, post.CreateInput{Title: "newword", Content: "new content"})
	if err != nil || same.ContentRevision != 2 || !same.EditedAt.Equal(*updated.EditedAt) {
		t.Fatalf("noop=%+v %v", same, err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM business_outbox WHERE JSON_EXTRACT(payload, '$.post_id')=? AND event_type='post.updated'", record.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("events=%d %v", count, err)
	}
	_, err = reads.Update(ctx, record.ID, actor.ID+1, post.CreateInput{Title: "unauthorized", Content: "content"})
	if e, ok := apperror.As(err); !ok || e.Code != apperror.CodePermissionDenied {
		t.Fatalf("authorization=%v", err)
	}
	_, err = reads.Update(ctx, 0, actor.ID, post.CreateInput{Title: "missing", Content: "content"})
	if e, ok := apperror.As(err); !ok || e.Code != apperror.CodePostNotFound {
		t.Fatalf("missing=%v", err)
	}
	failing := post.NewMySQLRepositoryWithOutbox(db, failedEditOutbox{})
	if err := failing.Update(ctx, record.ID, actor.ID, post.CreateInput{Title: "rollback", Content: "rollback"}); err == nil {
		t.Fatal("missing rollback failure")
	}
	got, err := repo.FindPublicByID(ctx, record.ID)
	if err != nil || got.ContentRevision != 2 || got.Title != "newword" {
		t.Fatalf("rollback=%+v %v", got, err)
	}
}
