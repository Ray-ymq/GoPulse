//go:build integration

package post_test

import (
	"context"
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"strings"
	"sync"
	"testing"
	"time"
)

type bookmarkCache struct {
	projection post.PublicProjection
	hit        bool
}

func (c *bookmarkCache) Get(context.Context, uint64) (post.PublicProjection, bool, error) {
	return c.projection, c.hit, nil
}
func (c *bookmarkCache) Set(_ context.Context, p post.PublicProjection) error {
	c.projection = p
	c.hit = true
	return nil
}
func (*bookmarkCache) Invalidate(context.Context, uint64) error { return nil }
func TestIntegrationBookmarksPrivateFactsAndReadPaths(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()
	ctx := context.Background()
	users := user.NewMySQLRepository(db)
	suffix := time.Now().Format("150405.000000")
	a, err := users.Create(ctx, "bookmark_a_"+suffix, "hash")
	if err != nil {
		t.Fatal(err)
	}
	b, err := users.Create(ctx, "bookmark_b_"+suffix, "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Exec("DELETE FROM posts WHERE author_id IN (?,?)", a.ID, b.ID)
		db.Exec("DELETE FROM users WHERE id IN (?,?)", a.ID, b.ID)
	}()
	repo := post.NewMySQLRepository(db)
	cache := &bookmarkCache{}
	reads := post.NewService(repo, cache)
	writes := bookmark.NewService(bookmark.NewMySQLRepository(db), reads)
	p, err := repo.Create(ctx, a.ID, "bookmark first", "content")
	if err != nil {
		t.Fatal(err)
	}
	q, err := repo.Create(ctx, a.ID, "bookmark second", "content")
	if err != nil {
		t.Fatal(err)
	}
	var beforeEvents int
	if err = db.QueryRow("SELECT COUNT(*) FROM business_outbox").Scan(&beforeEvents); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- writes.Bookmark(ctx, p.ID, b.ID) }()
	}
	wg.Wait()
	close(failures)
	for err = range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = writes.Bookmark(ctx, q.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 7, 1, 2, 3, 0, time.UTC)
	if _, err = db.Exec("UPDATE post_bookmarks SET created_at=? WHERE user_id=?", stamp, b.ID); err != nil {
		t.Fatal(err)
	}
	if err = writes.Bookmark(ctx, p.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	var actual time.Time
	if err = db.QueryRow("SELECT COUNT(*),MIN(created_at) FROM post_bookmarks WHERE user_id=?", b.ID).Scan(&count, &actual); err != nil || count != 2 || !actual.Equal(stamp) {
		t.Fatalf("idempotence count=%d stamp=%v err=%v", count, actual, err)
	}
	first, err := reads.List(ctx, b.ID, post.ListOptions{Bookmarks: true, Limit: 1})
	if err != nil || len(first.Posts) != 1 || first.Posts[0].ID != q.ID || first.NextCursor == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	boundary, err := post.DecodeCursor(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reads.List(ctx, b.ID, post.ListOptions{Bookmarks: true, Limit: 1, Cursor: &boundary})
	if err != nil || len(second.Posts) != 1 || second.Posts[0].ID != p.ID || second.NextCursor != nil {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	empty, err := reads.List(ctx, a.ID, post.ListOptions{Bookmarks: true, Limit: 20})
	if err != nil || len(empty.Posts) != 0 {
		t.Fatal("other viewer list leaked", err)
	}
	if _, err = db.Exec("INSERT INTO user_follows(follower_id,followed_id) VALUES (?,?)", b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, options := range []post.ListOptions{{Limit: 50}, {Limit: 50, AuthorID: a.ID}, {Limit: 50, Following: true}} {
		page, err := reads.List(ctx, b.ID, options)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, record := range page.Posts {
			if record.ID == p.ID {
				found = true
				if !record.BookmarkedByMe {
					t.Fatal("list missing viewer state")
				}
			}
		}
		if !found {
			t.Fatal("missing post")
		}
	}
	hits, err := repo.FindMany(ctx, b.ID, []uint64{p.ID, q.ID})
	if err != nil || len(hits) != 2 || !hits[0].BookmarkedByMe || !hits[1].BookmarkedByMe {
		t.Fatal("search hydration", err)
	}
	for _, viewer := range []uint64{b.ID, a.ID, 0, b.ID} {
		record, err := reads.Detail(ctx, p.ID, viewer)
		if err != nil || record.BookmarkedByMe != (viewer == b.ID) {
			t.Fatalf("detail viewer=%d record=%+v err=%v", viewer, record, err)
		}
	}
	payload, _ := json.Marshal(cache.projection)
	for _, forbidden := range []string{"bookmarked_by_me", "liked_by_me", "following"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatal("private cache field", string(payload))
		}
	}
	for range 2 {
		if err = writes.Unbookmark(ctx, p.ID, b.ID); err != nil {
			t.Fatal(err)
		}
	}
	record, err := reads.Detail(ctx, p.ID, b.ID)
	if err != nil || record.BookmarkedByMe {
		t.Fatal("cached detail stale", err)
	}
	for _, operation := range []func(context.Context, uint64, uint64) error{writes.Bookmark, writes.Unbookmark} {
		err = operation(ctx, ^uint64(0), b.ID)
		if app, ok := apperror.As(err); !ok || app.Code != apperror.CodePostNotFound {
			t.Fatalf("missing post: %v", err)
		}
	}
	if _, err = db.Exec("DELETE FROM posts WHERE id=?", q.ID); err != nil {
		t.Fatal(err)
	}
	empty, err = reads.List(ctx, b.ID, post.ListOptions{Bookmarks: true, Limit: 20})
	if err != nil || len(empty.Posts) != 0 {
		t.Fatal("deleted post visible", err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM post_bookmarks WHERE user_id=?", b.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("cascade failed", err)
	}
	if _, err = db.Exec("INSERT INTO post_bookmarks(post_id,user_id) VALUES (?,?)", p.ID, ^uint64(0)); err == nil {
		t.Fatal("orphan user accepted")
	}
	var afterEvents int
	if err = db.QueryRow("SELECT COUNT(*) FROM business_outbox").Scan(&afterEvents); err != nil || afterEvents != beforeEvents {
		t.Fatal("bookmark emitted events", err)
	}
}
