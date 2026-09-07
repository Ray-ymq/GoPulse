//go:build integration

package user_test

import (
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"testing"
	"time"
)

func TestIntegrationProfileFreshAcrossCachedPostReads(t *testing.T) {
	cfg := integrationtest.Environment(t)
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	unlock := integrationtest.AcquirePostFactsLock(t, db)
	defer unlock()
	ctx := context.Background()
	users := user.NewMySQLRepository(db)
	token := time.Now().Format("150405000000")
	a, err := users.Create(ctx, "read_a_"+token, "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", a.ID)
	b, err := users.Create(ctx, "read_b_"+token, "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", b.ID)
	defer db.ExecContext(ctx, "DELETE FROM posts WHERE author_id IN (?, ?)", a.ID, b.ID)
	redis := platform.NewRedis(cfg.Redis)
	defer redis.Close()
	cache := rediscache.NewPostDetailRepository(redis, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
	repo := post.NewMySQLRepository(db)
	service := post.NewService(repo, cache)
	first, err := service.Create(ctx, a.ID, post.CreateInput{Title: "profile first", Content: "content"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Invalidate(ctx, first.ID)
	second, err := service.Create(ctx, a.ID, post.CreateInput{Title: "profile second", Content: "content"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Create(ctx, b.ID, post.CreateInput{Title: "other author", Content: "content"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Detail(ctx, first.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	cached, hit, err := cache.Get(ctx, first.ID)
	if err != nil || !hit || cached.Author.DisplayName != a.Username {
		t.Fatalf("cache not warmed: %v %v %#v", hit, err, cached)
	}
	if _, err = users.UpdateProfile(ctx, a.ID, "最新名称", "简介"); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(ctx, first.ID, b.ID)
	if err != nil || detail.Author.DisplayName != "最新名称" {
		t.Fatalf("cached detail %#v %v", detail, err)
	}
	page, err := service.List(ctx, b.ID, post.ListOptions{AuthorID: a.ID, Limit: 1})
	if err != nil || len(page.Posts) != 1 || page.Posts[0].ID != second.ID || page.NextCursor == nil {
		t.Fatalf("author page %#v %v", page, err)
	}
	cursor, err := post.DecodeCursor(*page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.List(ctx, b.ID, post.ListOptions{AuthorID: a.ID, Limit: 1, Cursor: &cursor})
	if err != nil || len(next.Posts) != 1 || next.Posts[0].ID != first.ID || next.Posts[0].Author.DisplayName != "最新名称" || next.NextCursor != nil {
		t.Fatalf("author next %#v %v", next, err)
	}
	hydrated, err := repo.FindMany(ctx, b.ID, []uint64{first.ID, second.ID})
	if err != nil || len(hydrated) != 2 {
		t.Fatalf("search hydration %v %v", hydrated, err)
	}
	for _, record := range hydrated {
		if record.Author.DisplayName != "最新名称" {
			t.Fatal("stale search author")
		}
	}
}
