//go:build integration

package search

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

func TestIntegrationPointInTimeKeepsPaginationStableAcrossRefresh(t *testing.T) {
	cfg := integrationtest.Environment(t)
	client, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatalf("NewElasticsearch() error = %v", err)
	}
	repository := NewElasticsearchRepository(client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	index := PhysicalIndexPrefix + "pit-" + time.Now().UTC().Format("20060102t150405000000z")
	if err := repository.CreateIndex(ctx, index); err != nil {
		t.Fatalf("CreateIndex() error = %v", err)
	}
	defer func() {
		if err := repository.DeleteIndex(context.Background(), index); err != nil {
			t.Errorf("DeleteIndex() error = %v", err)
		}
	}()

	baseTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	initial := []Document{
		{PostID: 1, Title: "snapshot alpha", Content: "stable search page", CreatedAt: baseTime, UpdatedAt: baseTime},
		{PostID: 2, Title: "snapshot beta", Content: "stable search page", CreatedAt: baseTime.Add(time.Second), UpdatedAt: baseTime.Add(time.Second)},
		{PostID: 3, Title: "snapshot gamma", Content: "stable search page", CreatedAt: baseTime.Add(2 * time.Second), UpdatedAt: baseTime.Add(2 * time.Second)},
	}
	if err := repository.BulkIndex(ctx, index, initial); err != nil {
		t.Fatalf("BulkIndex(initial) error = %v", err)
	}
	if err := repository.Refresh(ctx, index); err != nil {
		t.Fatalf("Refresh(initial) error = %v", err)
	}

	pointInTime, err := repository.OpenPointInTime(ctx, index)
	if err != nil {
		t.Fatalf("OpenPointInTime() error = %v", err)
	}
	defer repository.ClosePointInTime(context.Background(), pointInTime) //nolint:errcheck

	baseline, err := repository.Search(ctx, index, pointInTime, "snapshot", 50, nil)
	if err != nil {
		t.Fatalf("Search(baseline) error = %v", err)
	}
	pageOne, err := repository.Search(ctx, index, baseline.PointInTime, "snapshot", 1, nil)
	if err != nil {
		t.Fatalf("Search(page one) error = %v", err)
	}
	if len(pageOne.Hits) < 2 {
		t.Fatalf("page one hits = %#v, want lookahead", pageOne.Hits)
	}

	incremental := make([]Document, 20)
	for offset := range incremental {
		postID := uint64(100 + offset)
		createdAt := baseTime.Add(time.Duration(offset+10) * time.Second)
		incremental[offset] = Document{
			PostID: postID, Title: "snapshot snapshot snapshot incremental", Content: "snapshot refresh changes live index statistics",
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}
	}
	if err := repository.BulkIndex(ctx, index, incremental); err != nil {
		t.Fatalf("BulkIndex(incremental) error = %v", err)
	}
	if err := repository.Refresh(ctx, index); err != nil {
		t.Fatalf("Refresh(incremental) error = %v", err)
	}

	firstHit := pageOne.Hits[0]
	pageTwo, err := repository.Search(ctx, index, pageOne.PointInTime, "snapshot", 50, &firstHit)
	if err != nil {
		t.Fatalf("Search(page two) error = %v", err)
	}
	actualIDs := []uint64{firstHit.PostID}
	for _, hit := range pageTwo.Hits {
		actualIDs = append(actualIDs, hit.PostID)
	}
	expectedIDs := make([]uint64, 0, len(baseline.Hits))
	for _, hit := range baseline.Hits {
		expectedIDs = append(expectedIDs, hit.PostID)
	}
	if !reflect.DeepEqual(actualIDs, expectedIDs) {
		t.Fatalf("PIT pagination IDs = %v, baseline = %v", actualIDs, expectedIDs)
	}
	if err := repository.ClosePointInTime(ctx, pageTwo.PointInTime); err != nil {
		t.Fatalf("ClosePointInTime() error = %v", err)
	}
}

func TestIntegrationClosedPointInTimeReturnsSafeSentinel(t *testing.T) {
	cfg := integrationtest.Environment(t)
	client, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		t.Fatalf("NewElasticsearch() error = %v", err)
	}
	repository := NewElasticsearchRepository(client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	index := PhysicalIndexPrefix + "pit-expiry-" + time.Now().UTC().Format("20060102t150405000000z")
	if err := repository.CreateIndex(ctx, index); err != nil {
		t.Fatalf("CreateIndex() error = %v", err)
	}
	defer repository.DeleteIndex(context.Background(), index) //nolint:errcheck

	pointInTime, err := repository.OpenPointInTime(ctx, index)
	if err != nil {
		t.Fatalf("OpenPointInTime() error = %v", err)
	}
	if err := repository.ClosePointInTime(ctx, pointInTime); err != nil {
		t.Fatalf("ClosePointInTime() error = %v", err)
	}
	_, err = repository.Search(ctx, index, pointInTime, "snapshot", 1, nil)
	if !errors.Is(err, ErrPointInTimeExpired) {
		t.Fatalf("Search(closed PIT) error = %v, want ErrPointInTimeExpired", err)
	}
}
