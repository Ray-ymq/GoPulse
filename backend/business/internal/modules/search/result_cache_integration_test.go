//go:build integration

package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/user"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform/redisclient"
	"github.com/Ray-ymq/GoPulse/backend/internal/testkit/integrationtest"
)

type searchCountPerformer struct {
	Performer
	searches int
}

func (performer *searchCountPerformer) Perform(ctx context.Context, request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/_search") {
		performer.searches++
	}
	return performer.Performer.Perform(ctx, request)
}

func TestIntegrationSearchResultCache(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := integrationtest.AcquirePostFactsLock(t, db)
	defer release()

	state, err := businessstate.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := state.CurrentEpoch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := user.NewMySQLRepository(db).Create(ctx, "srcache_"+time.Now().Format("150405000000"), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec("DELETE FROM posts WHERE author_id=?", actor.ID); err != nil {
			t.Error(err)
		}
		if _, err := db.Exec("DELETE FROM users WHERE id=?", actor.ID); err != nil {
			t.Error(err)
		}
	}()
	es, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatal(err)
	}
	counted := &searchCountPerformer{Performer: es}
	index := NewElasticsearchRepository(counted)
	generation, err := newPhysicalIndexName()
	if err != nil {
		t.Fatal(err)
	}
	if err := index.CreateIndexForEpoch(ctx, generation, epoch); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := index.DeleteIndex(context.Background(), generation); err != nil {
			t.Error(err)
		}
	}()

	const term = "searchcacheintegration"
	const postCount = 14
	posts := post.NewMySQLRepository(db)
	documents := make([]Document, 0, postCount)
	expected := make([]uint64, 0, postCount)
	baseTime := time.Now().UTC().Truncate(time.Second)
	for i := range postCount {
		record, err := posts.Create(ctx, actor.ID, term, term)
		if err != nil {
			t.Fatal(err)
		}
		rankTime := baseTime.Add(time.Duration(-i) * time.Second)
		expected = append(expected, record.ID)
		documents = append(documents, Document{
			PostID: record.ID, Title: term, Content: term, CreatedAt: rankTime, UpdatedAt: rankTime,
			DataEpoch: epoch, ContentRevision: record.ContentRevision,
		})
	}
	if err := index.BulkIndex(ctx, generation, documents); err != nil {
		t.Fatal(err)
	}
	if err := index.Refresh(ctx, generation); err != nil {
		t.Fatal(err)
	}
	old, err := index.SwitchAlias(ctx, generation)
	if err != nil {
		t.Fatal(err)
	}
	if len(old) == 1 {
		defer func() {
			if _, err := index.SwitchAlias(context.Background(), old[0]); err != nil {
				t.Error(err)
			}
		}()
	}

	redis := redisclient.NewRedis(cfg.Redis)
	defer redis.Close()
	service := NewService(index, posts, cfg.Auth.JWTSecret).WithEpochProvider(state).WithResultCache(redis, cfg.Redis.OperationTimeout)
	digest := digestQuery(term)
	key := resultCacheKey(epoch, generation, digest, 1, 0, 2)
	options := Options{Query: term, Limit: 2}
	run := func(opts Options, ids ...uint64) Page {
		t.Helper()
		page, err := service.Search(ctx, actor.ID, opts)
		if err != nil {
			t.Fatal(err)
		}
		actual := make([]uint64, len(page.Posts))
		for i, record := range page.Posts {
			actual[i] = record.ID
		}
		if !slices.Equal(actual, ids) {
			t.Fatalf("post IDs = %v, want %v", actual, ids)
		}
		return page
	}

	run(options, expected[:2]...)
	value, err := redis.Get(ctx, key)
	wantValue := fmt.Sprintf(`{"post_ids":[%d,%d,%d]}`, expected[0], expected[1], expected[2])
	if err != nil || value != wantValue || counted.searches != 1 {
		t.Fatalf("cache value = %q, err = %v, search count = %d", value, err, counted.searches)
	}
	if ttl, err := redis.TTL(ctx, key); err != nil || ttl < 29*time.Second || ttl > 30*time.Second {
		t.Fatalf("cache TTL = %s, err = %v", ttl, err)
	}
	if _, err := redis.Eval(ctx, `return redis.call("EXPIRE", KEYS[1], 20)`, []string{key}); err != nil {
		t.Fatal(err)
	}
	options.Query = "  " + term + "  "
	run(options, expected[:2]...)
	if counted.searches != 1 {
		t.Fatalf("search count after cache hit = %d", counted.searches)
	}
	if ttl, err := redis.TTL(ctx, key); err != nil || ttl <= 0 || ttl > 20*time.Second {
		t.Fatalf("cache TTL after hit = %s, err = %v", ttl, err)
	}
	options.Query = term

	empty := Options{Query: term + "absent", Limit: 2}
	run(empty)
	emptyKey := resultCacheKey(epoch, generation, digestQuery(empty.Query), 1, 0, empty.Limit)
	if _, err := redis.Get(ctx, emptyKey); !errors.Is(err, redisclient.ErrRedisKeyNotFound) {
		t.Fatalf("empty cache read error = %v", err)
	}

	paging := Options{Query: term, Limit: 1}
	for i, id := range expected {
		page := run(paging, id)
		if i == postCount-1 {
			if page.NextCursor != nil {
				t.Fatal("last page has a continuation")
			}
			break
		}
		cursor := decodePageCursor(t, page)
		if i == 9 && (cursor.Page != 11 || cursor.Offset != 10 || cursor.PointInTime != "") {
			t.Fatalf("page 10 cursor = %+v", cursor)
		}
		if i == 10 && (cursor.PointInTime == "" || cursor.After.PostID != id) {
			t.Fatalf("page 11 cursor = %+v", cursor)
		}
		paging.Cursor = &cursor
	}
	if _, err := redis.Get(ctx, resultCacheKey(epoch, generation, digest, 10, 9, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := redis.Get(ctx, resultCacheKey(epoch, generation, digest, 11, 10, 1)); !errors.Is(err, redisclient.ErrRedisKeyNotFound) {
		t.Fatalf("page 11 cache read error = %v", err)
	}

	const updated = "current post content from mysql"
	if _, err := db.ExecContext(ctx, "UPDATE posts SET content=?, content_revision=content_revision+1 WHERE id=?", updated, expected[1]); err != nil {
		t.Fatal(err)
	}
	before := counted.searches
	page := run(options, expected[:2]...)
	if page.Posts[1].Content != updated || counted.searches != before {
		t.Fatalf("edited content = %q, additional searches = %d", page.Posts[1].Content, counted.searches-before)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM posts WHERE id=?", expected[0]); err != nil {
		t.Fatal(err)
	}
	page = run(options, expected[1:3]...)
	cursor := decodePageCursor(t, page)
	if page.Posts[0].Content != updated || cursor.Offset != 3 || counted.searches != before+1 {
		t.Fatalf("edited content = %q, offset = %d, additional searches = %d", page.Posts[0].Content, cursor.Offset, counted.searches-before)
	}
	options.Cursor = &cursor
	run(options, expected[3:5]...)

	failingRedisConfig := cfg.Redis
	failingRedisConfig.Password = "incorrect-integration-password"
	failingRedis := redisclient.NewRedis(failingRedisConfig)
	defer failingRedis.Close()
	service.WithResultCache(failingRedis, cfg.Redis.OperationTimeout)
	before = counted.searches
	run(Options{Query: term, Limit: 3}, expected[1:4]...)
	if counted.searches <= before {
		t.Fatal("Redis failure did not fall back to Elasticsearch")
	}
}
