package search

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
)

const cacheTestEpoch = "123e4567-e89b-12d3-a456-426614174000"

type fakeResultRedis struct {
	values  map[string]string
	getErr  error
	setErr  error
	gets    int
	sets    int
	ttl     time.Duration
	timeout bool
}

func (redis *fakeResultRedis) Get(ctx context.Context, key string) (string, error) {
	redis.gets++
	if redis.timeout {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return redis.values[key], redis.getErr
}

func (redis *fakeResultRedis) Set(_ context.Context, key, value string, ttl time.Duration) error {
	redis.sets++
	redis.ttl = ttl
	if redis.setErr != nil {
		return redis.setErr
	}
	redis.values[key] = value
	return nil
}

func cachedSearchFixture(count int) (*Service, *fakeSearcher, *fakeHydrator, *fakeResultRedis) {
	searcher := &fakeSearcher{generation: PhysicalIndexPrefix + "cached", pointInTime: "pit"}
	hydrator := &fakeHydrator{}
	for i := range count {
		id := uint64(count - i)
		searcher.hits = append(searcher.hits, Hit{PostID: id, Score: 1, CreatedAt: "2026-09-08T00:00:00Z"})
		hydrator.records = append(hydrator.records, post.Post{ID: id, Content: "original"})
	}
	redis := &fakeResultRedis{values: make(map[string]string)}
	service := NewService(searcher, hydrator, testCursorSecret).
		WithEpochProvider(&fakeEpochProvider{epoch: cacheTestEpoch}).WithResultCache(redis, time.Millisecond)
	return service, searcher, hydrator, redis
}

func TestSearchResultCacheStoresOnlyOrderedIDsAndHydratesHits(t *testing.T) {
	service, searcher, hydrator, redis := cachedSearchFixture(5)
	ctx := context.Background()
	options := Options{Query: "term", Limit: 2}
	key := resultCacheKey(cacheTestEpoch, searcher.generation, digestQuery(options.Query), 1, 0, options.Limit)
	page, err := service.Search(ctx, 1, options)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].ID != 5 || page.Posts[1].ID != 4 || page.NextCursor == nil {
		t.Fatalf("first page = %+v, err = %v", page, err)
	}
	if redis.sets != 1 || redis.ttl != 30*time.Second || redis.values[key] != `{"post_ids":[5,4,3]}` {
		t.Fatalf("sets = %d, TTL = %s, value = %q", redis.sets, redis.ttl, redis.values[key])
	}
	hydrator.records[0].Content = "updated"
	options.Query = "  term  "
	page, err = service.Search(ctx, 2, options)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].Content != "updated" || page.Posts[0].ID != 5 || page.Posts[1].ID != 4 {
		t.Fatalf("cached page = %+v, err = %v", page, err)
	}
	if searcher.resolves != 2 || !slices.Equal(searcher.offsets, []int{0}) || redis.gets != 2 || redis.sets != 1 {
		t.Fatalf("generation checks = %d, search offsets = %v, cache reads/writes = %d/%d", searcher.resolves, searcher.offsets, redis.gets, redis.sets)
	}

	hydrator.records = hydrator.records[1:]
	page, err = service.Search(ctx, 2, options)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].ID != 4 || page.Posts[1].ID != 3 {
		t.Fatalf("refilled page = %+v, err = %v", page, err)
	}
	cursor := decodePageCursor(t, page)
	if cursor.Offset != 3 || !slices.Equal(searcher.offsets, []int{0, 3}) || redis.sets != 1 {
		t.Fatalf("cursor offset = %d, search offsets = %v, cache writes = %d", cursor.Offset, searcher.offsets, redis.sets)
	}
}

func TestSearchResultCacheKeyIsolatesEveryInput(t *testing.T) {
	const generation = PhysicalIndexPrefix + "generation"
	digest := digestQuery("term")
	const want = "gopulse:search:posts:v1:epoch:" + cacheTestEpoch + ":index:" + generation + ":q:599f7703547e9c8e:page:2:offset:20:limit:20"
	if key := resultCacheKey(cacheTestEpoch, generation, digest, 2, 20, 20); key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
	keys := []string{
		resultCacheKey("123e4567-e89b-12d3-a456-426614174001", generation, digest, 2, 20, 20),
		resultCacheKey(cacheTestEpoch, generation+"-new", digest, 2, 20, 20),
		resultCacheKey(cacheTestEpoch, generation, digestQuery("other"), 2, 20, 20),
		resultCacheKey(cacheTestEpoch, generation, digest, 3, 20, 20),
		resultCacheKey(cacheTestEpoch, generation, digest, 2, 21, 20),
		resultCacheKey(cacheTestEpoch, generation, digest, 2, 20, 21),
	}
	for _, key := range keys {
		if key == want {
			t.Fatalf("key collided: %q", key)
		}
	}
}

func TestSearchResultCacheFallsBackAndSkipsEmptyWrites(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		value   string
		getErr  error
		setErr  error
		timeout bool
	}{
		{name: "empty results"},
		{name: "invalid JSON", count: 1, value: "malformed"},
		{name: "unknown field", count: 1, value: `{"post_ids":[1],"title":"unexpected"}`},
		{name: "invalid ID", count: 1, value: `{"post_ids":[0]}`},
		{name: "duplicate ID", count: 1, value: `{"post_ids":[1,1]}`},
		{name: "too many IDs", count: 1, value: `{"post_ids":[1,2,3]}`},
		{name: "Redis error", count: 1, getErr: errors.New("redis unavailable"), setErr: errors.New("redis unavailable")},
		{name: "Redis timeout", count: 1, timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, searcher, _, redis := cachedSearchFixture(test.count)
			redis.getErr, redis.setErr, redis.timeout = test.getErr, test.setErr, test.timeout
			key := resultCacheKey(cacheTestEpoch, searcher.generation, digestQuery("term"), 1, 0, 1)
			redis.values[key] = test.value
			options := Options{Query: "term", Limit: 1}
			page, err := service.Search(context.Background(), 1, options)
			if err != nil || len(page.Posts) != test.count || len(searcher.offsets) != 1 || redis.sets != test.count {
				t.Fatalf("page = %+v, err = %v, search offsets = %v, writes = %d", page, err, searcher.offsets, redis.sets)
			}
		})
	}
}

func TestSearchResultCacheStopsAtPageTen(t *testing.T) {
	service, searcher, _, redis := cachedSearchFixture(12)
	ctx := context.Background()
	options := Options{Query: "term", Limit: 1}
	for number := 1; number <= 12; number++ {
		page, err := service.Search(ctx, 1, options)
		if err != nil || len(page.Posts) != 1 || page.Posts[0].ID != uint64(13-number) {
			t.Fatalf("page %d = %+v, err = %v", number, page, err)
		}
		if number == 12 {
			if page.NextCursor != nil {
				t.Fatal("last page has a continuation")
			}
			break
		}
		cursor := decodePageCursor(t, page)
		if number <= 10 && (cursor.Page != number+1 || cursor.Offset != number || cursor.PointInTime != "") {
			t.Fatalf("page %d returned cursor %+v", number, cursor)
		}
		if number == 11 && (cursor.PointInTime == "" || cursor.After.PostID != 2) {
			t.Fatalf("PIT cursor = %+v", cursor)
		}
		options.Cursor = &cursor
	}
	if redis.gets != 10 || redis.sets != 10 || len(searcher.offsets) != 10 || searcher.pitOpens != 1 ||
		!slices.Equal(searcher.pitOffsets, []int{10}) || searcher.pitSearches != 1 || len(searcher.closed) != 1 {
		t.Fatalf("reads/writes = %d/%d, live offsets = %v, PIT opens = %d, PIT offsets = %v, PIT continuations = %d, closed = %v",
			redis.gets, redis.sets, searcher.offsets, searcher.pitOpens, searcher.pitOffsets, searcher.pitSearches, searcher.closed)
	}
}

func TestOffsetCursorBindsLimitAndExpiresAfterTwoMinutes(t *testing.T) {
	service, _, _, redis := cachedSearchFixture(3)
	ctx := context.Background()
	now := time.Unix(2_000_000_000, 0)
	service.now = func() time.Time { return now }
	page, err := service.Search(ctx, 1, Options{Query: "term", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := decodePageCursor(t, page)
	if got := cursor.ExpiresAt - now.Unix(); got != 120 {
		t.Fatalf("cursor lifetime = %d", got)
	}
	_, err = service.Search(ctx, 1, Options{Query: "term", Limit: 2, Cursor: &cursor})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
	now = now.Add(2 * time.Minute)
	_, err = service.Search(ctx, 1, Options{Query: "term", Limit: 1, Cursor: &cursor})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
	if redis.gets != 1 {
		t.Fatalf("invalid cursors reached Redis: reads = %d", redis.gets)
	}
}

func decodePageCursor(t *testing.T, page Page) Cursor {
	t.Helper()
	if page.NextCursor == nil {
		t.Fatal("page has no continuation")
	}
	cursor, err := DecodeCursor(*page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	return cursor
}

func TestSearchResultCacheRefillsShortPageAfterDeletion(t *testing.T) {
	service, searcher, hydrator, _ := cachedSearchFixture(3)
	searcher.hits = searcher.hits[:2]
	ctx := context.Background()
	options := Options{Query: "term", Limit: 3}
	if _, err := service.Search(ctx, 1, options); err != nil {
		t.Fatal(err)
	}
	hydrator.records = hydrator.records[1:]
	searcher.hits = append(searcher.hits, Hit{PostID: 1})
	page, err := service.Search(ctx, 1, options)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].ID != 2 || page.Posts[1].ID != 1 || page.NextCursor != nil {
		t.Fatalf("refilled page = %+v, err = %v", page, err)
	}
	if !slices.Equal(searcher.offsets, []int{0, 2}) {
		t.Fatalf("search offsets = %v", searcher.offsets)
	}
}

func TestLegacyPITCursorContinuesWithoutRedis(t *testing.T) {
	const token = "eyJ2IjoyLCJxIjoiNTk5Zjc3MDM1NDdlOWM4ZTk4MGY0NGQ3ZDE5MmQ5NzhiOWU3MDM4NmFmMTgxMmIyMDQyYmVlYWY0YmEwM2RmYiIsImVwb2NoIjoiMTIzZTQ1NjctZTg5Yi0xMmQzLWE0NTYtNDI2NjE0MTc0MDAwIiwiZ2VuZXJhdGlvbiI6ImdvcHVsc2UtcG9zdC1zZWFyY2gtdjEtY2FjaGVkIiwicGl0IjoiZXhpc3RpbmctcGl0IiwiZXhwaXJlc19hdCI6MjAwMDAwMDA2MCwic2NvcmUiOjEsImNyZWF0ZWRfYXQiOiIyMDI2LTA5LTA4VDAwOjAwOjAwWiIsInBvc3RfaWQiOjMsInNoYXJkX2RvYyI6MH0.O8294o3A4NZTuoyQKUMwSDnTm_AxoJv0_I_26iZsyZo"
	cursor, err := DecodeCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	service, searcher, _, redis := cachedSearchFixture(3)
	service.now = func() time.Time { return time.Unix(2_000_000_000, 0) }
	page, err := service.Search(context.Background(), 1, Options{Query: "term", Limit: 2, Cursor: &cursor})
	if err != nil || len(page.Posts) != 2 || page.Posts[0].ID != 2 || page.Posts[1].ID != 1 || page.NextCursor != nil {
		t.Fatalf("legacy PIT page = %+v, err = %v", page, err)
	}
	if redis.gets != 0 || redis.sets != 0 || len(searcher.offsets) != 0 || searcher.pitOpens != 0 ||
		searcher.pitSearches != 1 || !slices.Equal(searcher.closed, []string{"existing-pit-next"}) {
		t.Fatalf("reads/writes = %d/%d, offset searches = %v, PIT opens = %d, continuations = %d, closed = %v",
			redis.gets, redis.sets, searcher.offsets, searcher.pitOpens, searcher.pitSearches, searcher.closed)
	}
}
