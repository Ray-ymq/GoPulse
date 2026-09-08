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

func TestIntegrationPermanentDeleteAtomicityAndStaleCache(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx := context.Background()
	actor, err := user.NewMySQLRepository(db).Create(ctx, "delete_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM users WHERE id=?", actor.ID)
	events, _ := outbox.NewRepository(db, outbox.Options{})
	repo := post.NewMySQLRepositoryWithOutbox(db, events)
	record, err := repo.Create(ctx, actor.ID, "deleteword", "private body")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM business_outbox WHERE JSON_EXTRACT(payload, '$.post_id')=?", record.ID)
	defer func() {
		for _, table := range []string{"notifications", "post_bookmarks", "post_likes", "comments"} {
			db.Exec("DELETE FROM "+table+" WHERE post_id=?", record.ID)
		}
		db.Exec("DELETE FROM posts WHERE id=?", record.ID)
	}()
	projection, _ := repo.FindPublicByID(ctx, record.ID)
	for _, query := range []string{
		"INSERT INTO comments(post_id, author_id, content, created_at) VALUES (?, ?, 'comment', NOW(6))",
		"INSERT INTO post_likes(post_id, user_id, created_at) VALUES (?, ?, NOW(6))",
		"INSERT INTO post_bookmarks(post_id, user_id) VALUES (?, ?)",
	} {
		if _, err := db.Exec(query, record.ID, actor.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO notifications(source_event_id,type,recipient_id,actor_id,post_id,created_at) VALUES (UUID(),'post.liked',?,?,?,NOW(6))", actor.ID, actor.ID, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO notifications(source_event_id,type,recipient_id,actor_id,post_id,comment_id,created_at) SELECT UUID(),'comment.created',?,?,post_id,id,NOW(6) FROM comments WHERE post_id=?", actor.ID, actor.ID, record.ID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM notifications WHERE recipient_id=?", actor.ID)
	if err := repo.Delete(ctx, record.ID, actor.ID+1); !errors.Is(err, post.ErrPermissionDenied) {
		t.Fatalf("authorization=%v", err)
	}
	failing := post.NewMySQLRepositoryWithOutbox(db, failedEditOutbox{})
	if err := failing.Delete(ctx, record.ID, actor.ID); err == nil {
		t.Fatal("expected outbox rollback")
	}
	var activeNotifications int
	if err := db.QueryRow("SELECT COUNT(*) FROM notifications WHERE post_id=?", record.ID).Scan(&activeNotifications); err != nil || activeNotifications != 2 {
		t.Fatalf("rollback notifications=%d %v", activeNotifications, err)
	}
	for _, table := range []string{"posts", "comments", "post_likes", "post_bookmarks"} {
		key := "post_id"
		if table == "posts" {
			key = "id"
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+key+"=?", record.ID).Scan(&n); err != nil || n != 1 {
			t.Fatalf("rollback %s=%d %v", table, n, err)
		}
	}
	reads := post.NewService(repo, &staleEditCache{projection: projection})
	if err := reads.Delete(ctx, record.ID, actor.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, record.ID, actor.ID); !errors.Is(err, post.ErrNotFound) {
		t.Fatalf("retry=%v", err)
	}
	if _, err := reads.Detail(ctx, record.ID, actor.ID); err == nil {
		t.Fatal("stale cache exposed deleted post")
	}
	for _, table := range []string{"posts", "comments", "post_likes", "post_bookmarks"} {
		key := "post_id"
		if table == "posts" {
			key = "id"
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+key+"=?", record.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("delete %s=%d %v", table, n, err)
		}
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM notifications WHERE recipient_id=? AND post_id IS NULL", actor.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("tombstone=%d %v", n, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM business_outbox WHERE event_type='post.deleted' AND JSON_EXTRACT(payload,'$.post_id')=?", record.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("event=%d %v", n, err)
	}
}
