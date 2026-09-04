package logquery

import (
	"context"
	"net/url"
	"testing"
	"time"
)

type fakeRepository struct{ opens, searches, closes int }

func (r *fakeRepository) OpenPointInTime(context.Context) (string, error) {
	r.opens++
	return "pit-1", nil
}
func (r *fakeRepository) Search(_ context.Context, pit string, filters Filters, limit int, after *Sort) (SearchResult, error) {
	r.searches++
	id := uint64(7)
	return SearchResult{PIT: pit, Hits: []Hit{{Entry: Entry{Timestamp: "2026-09-04T11:59:00Z", Level: "info", Service: "backend", Module: "post", Message: "post created", UserID: &id}, Sort: Sort{Timestamp: "2026-09-04T11:59:00Z", ShardDoc: 1}}}}, nil
}
func (r *fakeRepository) ClosePointInTime(context.Context, string) error { r.closes++; return nil }

func TestParseAndQueryUsesBoundedDefaults(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	options, err := ParseOptions(url.Values{"request_id": {"0123456789abcdef0123456789abcdef"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if options.Limit != 50 || options.Filters.From != "2026-09-04T11:45:00Z" || options.Filters.To != "2026-09-04T12:00:00Z" {
		t.Fatalf("options=%+v", options)
	}
	repository := &fakeRepository{}
	service := NewService(repository, "01234567890123456789012345678901")
	service.now = func() time.Time { return now }
	page, err := service.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.NextCursor != nil || repository.opens != 1 || repository.searches != 1 || repository.closes != 1 {
		t.Fatalf("page=%+v repo=%+v", page, repository)
	}
}

func TestParseRejectsCursorMixedWithFilters(t *testing.T) {
	if _, err := ParseOptions(url.Values{"cursor": {"abc"}, "limit": {"1"}}, time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("mixed cursor was accepted")
	}
}
