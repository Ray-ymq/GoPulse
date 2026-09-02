//go:build integration

package outbox_test

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
)

type fakeClock struct {
	mutex sync.RWMutex
	now   time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mutex.RLock()
	defer clock.mutex.RUnlock()
	return clock.now
}

func (clock *fakeClock) Set(now time.Time) {
	clock.mutex.Lock()
	clock.now = now
	clock.mutex.Unlock()
}

func TestIntegrationOutboxLeaseStateMachine(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := database.ExecContext(ctx, `DELETE FROM business_outbox`); err != nil {
		t.Fatalf("clear business_outbox: %v", err)
	}
	t.Cleanup(func() {
		if _, err := database.ExecContext(context.Background(), `DELETE FROM business_outbox`); err != nil {
			t.Errorf("cleanup business_outbox: %v", err)
		}
	})

	clock := &fakeClock{now: time.Date(2026, time.September, 2, 3, 0, 0, 0, time.UTC)}
	repository, err := outbox.NewRepository(database, outbox.Options{
		Clock: clock.Now,
		Backoff: func(attempt uint32) time.Duration {
			return time.Duration(attempt) * 5 * time.Second
		},
	})
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}

	rolledBack := newIntegrationEvent(t, clock.Now(), 100)
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if err := repository.Insert(ctx, transaction, rolledBack); err != nil {
		t.Fatalf("Insert(transaction) error = %v", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	assertEventCount(t, ctx, database, rolledBack.EventID, 0)

	events := make([]bus.Envelope, 5)
	for index := range events {
		events[index] = newIntegrationEvent(t, clock.Now(), uint64(index+1))
		if err := repository.Insert(ctx, database, events[index]); err != nil {
			t.Fatalf("Insert(%d) error = %v", index, err)
		}
	}
	if err := repository.Insert(ctx, database, events[0]); err == nil {
		t.Fatal("duplicate event ID insert error = nil")
	}

	type claimResult struct {
		records []outbox.Record
		err     error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	for _, owner := range []string{"dispatcher-a", "dispatcher-b"} {
		owner := owner
		go func() {
			<-start
			records, claimErr := repository.Claim(ctx, owner, 2, 30*time.Second)
			results <- claimResult{records: records, err: claimErr}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent Claim() errors = %v, %v", first.err, second.err)
	}
	if len(first.records) != 2 || len(second.records) != 2 {
		t.Fatalf("concurrent claim sizes = %d, %d", len(first.records), len(second.records))
	}
	assertStableIDs(t, first.records)
	assertStableIDs(t, second.records)
	claimed := map[uint64]string{}
	for _, result := range []claimResult{first, second} {
		for _, record := range result.records {
			if previous, exists := claimed[record.ID]; exists {
				t.Fatalf("outbox ID %d claimed by both %s and %s", record.ID, previous, record.LeaseOwner)
			}
			claimed[record.ID] = record.LeaseOwner
			if _, err := record.Envelope(); err != nil {
				t.Fatalf("claimed record Envelope() error = %v", err)
			}
		}
	}

	allClaimed := append(append([]outbox.Record(nil), first.records...), second.records...)
	sort.Slice(allClaimed, func(i, j int) bool { return allClaimed[i].ID < allClaimed[j].ID })
	publishedFirst := allClaimed[0]
	retryRecord := allClaimed[1]
	leasedFirst := allClaimed[2]
	leasedSecond := allClaimed[3]

	if err := repository.MarkPublished(ctx, publishedFirst.ID, "wrong-owner"); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("wrong-owner MarkPublished() error = %v", err)
	}
	if err := repository.MarkPublished(ctx, publishedFirst.ID, publishedFirst.LeaseOwner); err != nil {
		t.Fatalf("MarkPublished() error = %v", err)
	}
	if err := repository.ReleaseFailed(ctx, retryRecord.ID, retryRecord.LeaseOwner, outbox.FailurePublishTimeout); err != nil {
		t.Fatalf("ReleaseFailed() error = %v", err)
	}

	pendingClaim, err := repository.Claim(ctx, "dispatcher-c", 5, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim(pending) error = %v", err)
	}
	if len(pendingClaim) != 1 {
		t.Fatalf("immediate pending claim count = %d, want 1", len(pendingClaim))
	}
	if pendingClaim[0].ID == retryRecord.ID || pendingClaim[0].ID == leasedFirst.ID || pendingClaim[0].ID == leasedSecond.ID {
		t.Fatalf("immediate claim returned delayed or actively leased record: %#v", pendingClaim[0])
	}

	clock.Set(clock.Now().Add(6 * time.Second))
	retryClaim, err := repository.Claim(ctx, "dispatcher-d", 5, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim(retry) error = %v", err)
	}
	if len(retryClaim) != 1 || retryClaim[0].ID != retryRecord.ID || retryClaim[0].AttemptCount != 1 || retryClaim[0].LastError != string(outbox.FailurePublishTimeout) {
		t.Fatalf("retry claim = %#v", retryClaim)
	}

	clock.Set(time.Date(2026, time.September, 2, 3, 0, 31, 0, time.UTC))
	recovered, err := repository.Claim(ctx, "dispatcher-e", 5, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim(recovered) error = %v", err)
	}
	if len(recovered) != 3 {
		t.Fatalf("recovered claim count = %d, want 3: %#v", len(recovered), recovered)
	}
	recoveredByID := recordsByID(recovered)
	for _, record := range []outbox.Record{leasedFirst, leasedSecond, pendingClaim[0]} {
		if _, ok := recoveredByID[record.ID]; !ok {
			t.Fatalf("expired lease ID %d was not recovered", record.ID)
		}
	}
	if err := repository.MarkPublished(ctx, leasedFirst.ID, leasedFirst.LeaseOwner); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("stale-owner MarkPublished() error = %v", err)
	}
	if err := repository.MarkPublished(ctx, leasedFirst.ID, "dispatcher-e"); err != nil {
		t.Fatalf("recovered MarkPublished() error = %v", err)
	}
	if err := repository.ReleaseFailed(ctx, leasedSecond.ID, "dispatcher-e", outbox.FailurePublishNack); err != nil {
		t.Fatalf("recovered ReleaseFailed() error = %v", err)
	}
	if err := repository.MarkPublished(ctx, retryClaim[0].ID, "dispatcher-d"); err != nil {
		t.Fatalf("retry MarkPublished() error = %v", err)
	}

	deleted, err := repository.CleanupPublished(ctx, clock.Now().Add(time.Second), 100)
	if err != nil {
		t.Fatalf("CleanupPublished() error = %v", err)
	}
	if deleted != 3 {
		t.Fatalf("CleanupPublished() deleted = %d, want 3", deleted)
	}
	assertStatusCount(t, ctx, database, outbox.StatusPending, 1)
	assertStatusCount(t, ctx, database, outbox.StatusLeased, 1)
	assertStatusCount(t, ctx, database, outbox.StatusPublished, 0)

	clock.Set(time.Date(2026, time.September, 2, 3, 1, 2, 0, time.UTC))
	finalClaim, err := repository.Claim(ctx, "dispatcher-f", 5, 30*time.Second)
	if err != nil {
		t.Fatalf("Claim(final recovery) error = %v", err)
	}
	if len(finalClaim) != 2 {
		t.Fatalf("final claim count = %d, want 2: %#v", len(finalClaim), finalClaim)
	}
}

func TestIntegrationOutboxSchemaConstraintsAndIndexes(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var createTable string
	if err := database.QueryRowContext(ctx, `SHOW CREATE TABLE business_outbox`).Scan(new(string), &createTable); err != nil {
		t.Fatalf("SHOW CREATE TABLE business_outbox: %v", err)
	}
	for _, required := range []string{
		"uq_business_outbox_event_id",
		"idx_business_outbox_pending",
		"idx_business_outbox_lease_recovery",
		"idx_business_outbox_published_cleanup",
		"chk_business_outbox_event_type",
		"chk_business_outbox_schema_version",
		"chk_business_outbox_state",
		"`payload` json NOT NULL",
	} {
		if !containsFold(createTable, required) {
			t.Fatalf("SHOW CREATE TABLE missing %q: %s", required, createTable)
		}
	}

	_, err = database.ExecContext(ctx, `
		INSERT INTO business_outbox (
			event_id, event_type, schema_version, payload, status, available_at,
			attempt_count, created_at, updated_at
		) VALUES ('123e4567-e89b-12d3-a456-426614174000', 'comment.created', 1, JSON_OBJECT(), 'leased', UTC_TIMESTAMP(6), 0, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6))`)
	if err == nil {
		t.Fatal("invalid leased state insert succeeded")
	}
}

func newIntegrationEvent(t *testing.T, occurredAt time.Time, seed uint64) bus.Envelope {
	t.Helper()
	if seed%2 == 0 {
		event, err := bus.NewPostLiked(occurredAt, seed+1, seed+101, seed+201)
		if err != nil {
			t.Fatalf("NewPostLiked() error = %v", err)
		}
		return event
	}
	event, err := bus.NewCommentCreated(occurredAt, seed+1, seed+101, seed+201, seed+301)
	if err != nil {
		t.Fatalf("NewCommentCreated() error = %v", err)
	}
	return event
}

func assertStableIDs(t *testing.T, records []outbox.Record) {
	t.Helper()
	for index := 1; index < len(records); index++ {
		if records[index-1].ID >= records[index].ID {
			t.Fatalf("claim IDs are not stable ascending: %#v", records)
		}
	}
}

func recordsByID(records []outbox.Record) map[uint64]outbox.Record {
	indexed := make(map[uint64]outbox.Record, len(records))
	for _, record := range records {
		indexed[record.ID] = record
	}
	return indexed
}

func assertEventCount(t *testing.T, ctx context.Context, database *sql.DB, eventID string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_outbox WHERE event_id = ?`, eventID).Scan(&count); err != nil {
		t.Fatalf("count event ID: %v", err)
	}
	if count != want {
		t.Fatalf("event count = %d, want %d", count, want)
	}
}

func assertStatusCount(t *testing.T, ctx context.Context, database *sql.DB, status outbox.Status, want int) {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM business_outbox WHERE status = ?`, string(status)).Scan(&count); err != nil {
		t.Fatalf("count status %s: %v", status, err)
	}
	if count != want {
		t.Fatalf("status %s count = %d, want %d", status, count, want)
	}
}

func containsFold(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if equalFoldASCII(value[index:index+len(substring)], substring) {
			return true
		}
	}
	return false
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		l, r := left[index], right[index]
		if l >= 'A' && l <= 'Z' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}
