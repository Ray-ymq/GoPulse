package search

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
)

type fakeSearcher struct {
	generation string
	hits       []Hit
	err        error
}

func (fake *fakeSearcher) ResolveGeneration(context.Context) (string, error) {
	if fake.err != nil {
		return "", fake.err
	}
	return fake.generation, nil
}
func (fake *fakeSearcher) Search(_ context.Context, generation, _ string, _ int, _ *Hit) (SearchResult, error) {
	if fake.err != nil {
		return SearchResult{}, fake.err
	}
	return SearchResult{Generation: generation, Hits: fake.hits}, nil
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

func TestParseOptionsAndCursorBindQueryAndGeneration(t *testing.T) {
	generation := PhysicalIndexPrefix + "20260902t120000z-test"
	token, err := EncodeCursor(Cursor{QueryDigest: digestQuery("帖子"), Generation: generation, After: Hit{PostID: 9, Score: 1.25, CreatedAt: "2026-09-02T04:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	options, err := ParseOptions(url.Values{"q": {"  帖子  "}, "limit": {"10"}, "cursor": {token}})
	if err != nil {
		t.Fatalf("ParseOptions() error = %v", err)
	}
	if options.Query != "帖子" || options.Limit != 10 || options.Cursor == nil || options.Cursor.Generation != generation {
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
		{PostID: 2, Score: 2, CreatedAt: "2026-09-02T05:00:00Z"},
		{PostID: 1, Score: 1, CreatedAt: "2026-09-02T04:00:00Z"},
	}
	service := NewService(&fakeSearcher{generation: generation, hits: hits}, &fakeHydrator{records: []post.Post{{ID: 1}, {ID: 2}}})
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
	service.searcher = &fakeSearcher{generation: PhysicalIndexPrefix + "20260902t130000z-rebuilt"}
	_, err = service.Search(context.Background(), 7, Options{Query: "GoPulse", Limit: 1, Cursor: &cursor})
	appError, ok := apperror.As(err)
	if !ok || appError.Code != apperror.CodeValidationFailed {
		t.Fatalf("error = %v, want validation_failed", err)
	}
}

func TestServiceMapsSearchDependencyFailureToUnavailable(t *testing.T) {
	service := NewService(&fakeSearcher{err: errors.New("http://secret:9200 index body")}, &fakeHydrator{})
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

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
