package search

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

const testCursorSecret = "test-search-cursor-secret-at-least-32-bytes"

type fakeSearcher struct {
	generation  string
	pointInTime string
	hits        []Hit
	resolveErr  error
	openErr     error
	searchErr   error
	closed      []string
}

func (fake *fakeSearcher) ResolveGeneration(context.Context) (string, error) {
	if fake.resolveErr != nil {
		return "", fake.resolveErr
	}
	return fake.generation, nil
}

func (fake *fakeSearcher) OpenPointInTime(context.Context, string) (string, error) {
	if fake.openErr != nil {
		return "", fake.openErr
	}
	return fake.pointInTime, nil
}

func (fake *fakeSearcher) Search(_ context.Context, generation, pointInTime, _ string, _ int, _ *Hit) (SearchResult, error) {
	if fake.searchErr != nil {
		return SearchResult{}, fake.searchErr
	}
	return SearchResult{Generation: generation, PointInTime: pointInTime + "-next", Hits: fake.hits}, nil
}

func (fake *fakeSearcher) ClosePointInTime(_ context.Context, pointInTime string) error {
	fake.closed = append(fake.closed, pointInTime)
	return nil
}

type fakeHydrator struct{ records []post.Post }

func (fake *fakeHydrator) FindMany(_ context.Context, _ uint64, identifiers []uint64) ([]post.Post, error) {
	byID := make(map[uint64]post.Post, len(fake.records))
	for _, record := range fake.records {
		byID[record.ID] = record
	}
	result := make([]post.Post, 0, len(identifiers))
	for _, identifier := range identifiers {
		result = append(result, byID[identifier])
	}
	return result, nil
}

func TestParseOptionsAndCursorBindQueryGenerationAndPointInTime(t *testing.T) {
	generation := PhysicalIndexPrefix + "20260902t120000z-test"
	token, err := EncodeCursor(Cursor{
		QueryDigest: digestQuery("帖子"), Generation: generation, PointInTime: "pit-test", ExpiresAt: time.Now().Add(time.Minute).Unix(),
		After: Hit{PostID: 9, Score: 1.25, CreatedAt: "2026-09-02T04:00:00Z", ShardDoc: 7},
	}, testCursorSecret)
	if err != nil {
		t.Fatal(err)
	}
	options, err := ParseOptions(url.Values{"q": {"  帖子  "}, "limit": {"10"}, "cursor": {token}})
	if err != nil {
		t.Fatalf("ParseOptions() error = %v", err)
	}
	if options.Query != "帖子" || options.Limit != 10 || options.Cursor == nil || options.Cursor.Generation != generation || options.Cursor.PointInTime != "pit-test" {
		t.Fatalf("options = %#v", options)
	}
	if _, err := ParseOptions(url.Values{"q": {"other"}, "cursor": {token}}); err == nil {
		t.Fatal("cross-query cursor was accepted")
	}
	if _, err := ParseOptions(url.Values{"q": {"ok"}, "index": {"private"}}); err == nil {
		t.Fatal("unknown search parameter was accepted")
	}
}

func TestServiceHydratesInHitOrderAndInvalidatesOldGeneration(t *testing.T) {
	generation := PhysicalIndexPrefix + "20260902t120000z-current"
	hits := []Hit{
		{PostID: 2, Score: 2, CreatedAt: "2026-09-02T05:00:00Z", ShardDoc: 2},
		{PostID: 1, Score: 1, CreatedAt: "2026-09-02T04:00:00Z", ShardDoc: 1},
	}
	searcher := &fakeSearcher{generation: generation, pointInTime: "pit-current", hits: hits}
	service := NewService(searcher, &fakeHydrator{records: []post.Post{{ID: 1}, {ID: 2}}}, testCursorSecret)
	page, err := service.Search(context.Background(), 7, Options{Query: "GoPulse", Limit: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(page.Posts) != 1 || page.Posts[0].ID != 2 || page.NextCursor == nil {
		t.Fatalf("page = %#v", page)
	}
	cursor, err := DecodeCursor(*page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.PointInTime != "pit-current-next" || !verifyCursor(cursor, service.cursorKey) {
		t.Fatalf("cursor = %#v", cursor)
	}
	searcher.generation = PhysicalIndexPrefix + "20260902t130000z-rebuilt"
	_, err = service.Search(context.Background(), 7, Options{Query: "GoPulse", Limit: 1, Cursor: &cursor})
	appError, ok := apperror.As(err)
	if !ok || appError.Code != apperror.CodeValidationFailed {
		t.Fatalf("error = %v, want validation_failed", err)
	}
	if len(searcher.closed) != 1 || searcher.closed[0] != cursor.PointInTime {
		t.Fatalf("closed PITs = %#v", searcher.closed)
	}
}

func TestServiceRejectsTamperedCursorAndExpiredPointInTime(t *testing.T) {
	generation := PhysicalIndexPrefix + "20260902t120000z-current"
	searcher := &fakeSearcher{generation: generation, pointInTime: "pit-current", hits: []Hit{{PostID: 2, Score: 2, CreatedAt: "2026-09-02T05:00:00Z", ShardDoc: 2}}}
	service := NewService(searcher, &fakeHydrator{records: []post.Post{{ID: 2}}}, testCursorSecret)
	service.now = func() time.Time { return time.Unix(2_000_000_000, 0) }

	token, err := EncodeCursor(Cursor{
		QueryDigest: digestQuery("GoPulse"), Generation: generation, PointInTime: "pit-current", ExpiresAt: service.now().Add(time.Minute).Unix(),
		After: Hit{PostID: 9, Score: 1.25, CreatedAt: "2026-09-02T04:00:00Z", ShardDoc: 7},
	}, testCursorSecret)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	original, err := DecodeCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	original.After.Score = 99
	payload, err := marshalCursor(original)
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := DecodeCursor(base64.RawURLEncoding.EncodeToString(payload) + "." + parts[1])
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Search(context.Background(), 7, Options{Query: "GoPulse", Limit: 1, Cursor: &tampered})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)

	expired := tampered
	expired.signature = nil
	expired.After.Score = 1.25
	expired.ExpiresAt = service.now().Add(-time.Second).Unix()
	expiredToken, err := EncodeCursor(expired, testCursorSecret)
	if err != nil {
		t.Fatal(err)
	}
	expired, err = DecodeCursor(expiredToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Search(context.Background(), 7, Options{Query: "GoPulse", Limit: 1, Cursor: &expired})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
}

func TestServiceMapsMissingPointInTimeToValidationFailure(t *testing.T) {
	generation := PhysicalIndexPrefix + "20260902t120000z-current"
	cursorToken, err := EncodeCursor(Cursor{
		QueryDigest: digestQuery("term"), Generation: generation, PointInTime: "pit-expired", ExpiresAt: time.Now().Add(time.Minute).Unix(),
		After: Hit{PostID: 2, Score: 1, CreatedAt: "2026-09-02T04:00:00Z", ShardDoc: 1},
	}, testCursorSecret)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := DecodeCursor(cursorToken)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(&fakeSearcher{generation: generation, searchErr: ErrPointInTimeExpired}, &fakeHydrator{}, testCursorSecret)
	_, err = service.Search(context.Background(), 1, Options{Query: "term", Limit: 20, Cursor: &cursor})
	assertApplicationCode(t, err, apperror.CodeValidationFailed)
}

func TestServiceMapsSearchDependencyFailureToUnavailable(t *testing.T) {
	service := NewService(&fakeSearcher{resolveErr: errors.New("http://secret:9200 index body")}, &fakeHydrator{}, testCursorSecret)
	_, err := service.Search(context.Background(), 1, Options{Query: "term", Limit: 20})
	appError, ok := apperror.As(err)
	if !ok || appError.Code != apperror.CodeSearchUnavailable || appError.Message == "" {
		t.Fatalf("error = %v", err)
	}
}

func TestDocumentContractContainsOnlyRebuildFields(t *testing.T) {
	document := Document{PostID: 1, Title: "title", Content: "content", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	mapping := string(Mapping)
	for _, field := range []string{"post_id", "title", "content", "created_at", "updated_at", `"dynamic":"strict"`} {
		if !contains(mapping, field) {
			t.Fatalf("mapping missing %s", field)
		}
	}
	for _, forbidden := range []string{"author", "like_count", "comment_count", "liked_by_me"} {
		if contains(mapping, forbidden) {
			t.Fatalf("mapping contains %s", forbidden)
		}
	}
}

func assertApplicationCode(t *testing.T, err error, code apperror.Code) {
	t.Helper()
	appError, ok := apperror.As(err)
	if !ok || appError.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
