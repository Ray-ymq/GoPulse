package outbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
)

type dispatcherStoreFake struct {
	mu sync.Mutex

	records       []Record
	claimErr      error
	markErr       error
	releaseErr    error
	cleanupErr    error
	cleanupResult int64
	cleanupCalled chan struct{}
	cleanupOnce   sync.Once

	claimCalls   int
	markCalls    []uint64
	releaseCalls []dispatcherReleaseCall
	cleanupCalls []dispatcherCleanupCall
}

type dispatcherReleaseCall struct {
	id      uint64
	owner   string
	failure FailureCode
}

type dispatcherCleanupCall struct {
	cutoff time.Time
	batch  int
}

func (store *dispatcherStoreFake) Claim(_ context.Context, owner string, _ int, _ time.Duration) ([]Record, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimCalls++
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	records := make([]Record, len(store.records))
	copy(records, store.records)
	for index := range records {
		records[index].LeaseOwner = owner
	}
	return records, nil
}

func (store *dispatcherStoreFake) MarkPublished(_ context.Context, id uint64, _ string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.markCalls = append(store.markCalls, id)
	return store.markErr
}

func (store *dispatcherStoreFake) ReleaseFailed(_ context.Context, id uint64, owner string, failure FailureCode) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.releaseCalls = append(store.releaseCalls, dispatcherReleaseCall{id: id, owner: owner, failure: failure})
	return store.releaseErr
}

func (store *dispatcherStoreFake) CleanupPublished(_ context.Context, cutoff time.Time, batch int) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cleanupCalls = append(store.cleanupCalls, dispatcherCleanupCall{cutoff: cutoff, batch: batch})
	if store.cleanupCalled != nil {
		store.cleanupOnce.Do(func() { close(store.cleanupCalled) })
	}
	return store.cleanupResult, store.cleanupErr
}

type dispatcherPublisherFake struct {
	mu        sync.Mutex
	err       error
	block     bool
	published []bus.Envelope
}

func (publisher *dispatcherPublisherFake) Publish(ctx context.Context, envelope bus.Envelope) error {
	if publisher.block {
		<-ctx.Done()
		return ctx.Err()
	}
	publisher.mu.Lock()
	publisher.published = append(publisher.published, envelope)
	publisher.mu.Unlock()
	return publisher.err
}

func dispatcherTestRecord(t *testing.T, id uint64) Record {
	t.Helper()
	event, err := bus.NewPostLiked(time.Date(2026, time.September, 2, 3, 4, 5, 0, time.UTC), 1, 2, 3)
	if err != nil {
		t.Fatalf("NewPostLiked() error = %v", err)
	}
	payload, err := bus.Encode(event)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	return Record{
		ID:            id,
		EventID:       event.EventID,
		EventType:     event.EventType,
		SchemaVersion: event.SchemaVersion,
		Payload:       payload,
	}
}

func newDispatcherForTest(t *testing.T, store Store, publisher Publisher) *Dispatcher {
	t.Helper()
	dispatcher, err := NewDispatcher(store, publisher, DispatcherOptions{
		Owner:          "dispatcher-test",
		PollInterval:   10 * time.Millisecond,
		ClaimBatch:     2,
		LeaseDuration:  2 * time.Second,
		PublishTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	return dispatcher
}

func TestDispatcherPublishesAndMarksOnlyAfterPublishSucceeds(t *testing.T) {
	store := &dispatcherStoreFake{records: []Record{dispatcherTestRecord(t, 41)}}
	publisher := &dispatcherPublisherFake{}
	dispatcher := newDispatcherForTest(t, store, publisher)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if store.claimCalls != 1 || len(publisher.published) != 1 {
		t.Fatalf("claim calls=%d published=%d, want one each", store.claimCalls, len(publisher.published))
	}
	if len(store.markCalls) != 1 || store.markCalls[0] != 41 {
		t.Fatalf("mark calls=%v, want [41]", store.markCalls)
	}
	if len(store.releaseCalls) != 0 {
		t.Fatalf("release calls=%v, want none", store.releaseCalls)
	}
}

func TestDispatcherReleasesBoundedPublishFailuresWithoutReturningCycleError(t *testing.T) {
	store := &dispatcherStoreFake{records: []Record{dispatcherTestRecord(t, 42)}}
	publisher := &dispatcherPublisherFake{err: NewPublishError(FailurePublishUnroutable, errors.New("amqp://secret"))}
	dispatcher := newDispatcherForTest(t, store, publisher)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v, want handled publish failure", err)
	}
	if len(store.markCalls) != 0 || len(store.releaseCalls) != 1 {
		t.Fatalf("mark calls=%v release calls=%v", store.markCalls, store.releaseCalls)
	}
	if store.releaseCalls[0].failure != FailurePublishUnroutable {
		t.Fatalf("release failure=%q, want %q", store.releaseCalls[0].failure, FailurePublishUnroutable)
	}
}

func TestDispatcherDoesNotLogOrReturnPublisherDetails(t *testing.T) {
	store := &dispatcherStoreFake{records: []Record{dispatcherTestRecord(t, 43)}}
	publisher := &dispatcherPublisherFake{err: errors.New("amqp://user:password@example.invalid")}
	dispatcher := newDispatcherForTest(t, store, publisher)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v, want handled publisher failure", err)
	}
	if len(store.releaseCalls) != 1 || store.releaseCalls[0].failure != FailurePublishUnavailable {
		t.Fatalf("release calls=%v, want unavailable failure", store.releaseCalls)
	}
	if strings.Contains(safeDispatcherError(publisher.err), "password") {
		t.Fatal("safeDispatcherError leaked publisher detail")
	}
}

func TestDispatcherReleasesMalformedEnvelopeAndReportsInternalError(t *testing.T) {
	record := dispatcherTestRecord(t, 44)
	record.Payload = []byte(`{"event_type":"broken"}`)
	store := &dispatcherStoreFake{records: []Record{record}}
	publisher := &dispatcherPublisherFake{}
	dispatcher := newDispatcherForTest(t, store, publisher)

	err := dispatcher.DispatchOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode outbox event") {
		t.Fatalf("DispatchOnce() error = %v, want decode error", err)
	}
	if len(publisher.published) != 0 || len(store.markCalls) != 0 || len(store.releaseCalls) != 1 {
		t.Fatalf("published=%d marks=%d releases=%d", len(publisher.published), len(store.markCalls), len(store.releaseCalls))
	}
	if store.releaseCalls[0].failure != FailureInternal {
		t.Fatalf("release failure=%q, want %q", store.releaseCalls[0].failure, FailureInternal)
	}
}

func TestDispatcherReleasesAfterMarkFailureToPreserveAtLeastOnceRetry(t *testing.T) {
	store := &dispatcherStoreFake{
		records: []Record{dispatcherTestRecord(t, 45)},
		markErr: errors.New("database unavailable"),
	}
	publisher := &dispatcherPublisherFake{}
	dispatcher := newDispatcherForTest(t, store, publisher)

	err := dispatcher.DispatchOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "mark published") {
		t.Fatalf("DispatchOnce() error = %v, want mark error", err)
	}
	if len(store.markCalls) != 1 || len(store.releaseCalls) != 1 || len(publisher.published) != 1 {
		t.Fatalf("marks=%d releases=%d published=%d", len(store.markCalls), len(store.releaseCalls), len(publisher.published))
	}
	if store.releaseCalls[0].failure != FailureInternal {
		t.Fatalf("release failure=%q, want %q", store.releaseCalls[0].failure, FailureInternal)
	}
}

func TestDispatcherDoesNotClaimWhenContextAlreadyCanceled(t *testing.T) {
	store := &dispatcherStoreFake{}
	publisher := &dispatcherPublisherFake{}
	dispatcher := newDispatcherForTest(t, store, publisher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := dispatcher.DispatchOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("DispatchOnce() error = %v, want context canceled", err)
	}
	if store.claimCalls != 0 {
		t.Fatalf("claim calls=%d, want zero", store.claimCalls)
	}
}

func TestDispatcherCancellationLeavesInFlightLeaseForRecovery(t *testing.T) {
	store := &dispatcherStoreFake{records: []Record{dispatcherTestRecord(t, 46)}}
	publisher := &dispatcherPublisherFake{block: true}
	dispatcher := newDispatcherForTest(t, store, publisher)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := dispatcher.DispatchOnce(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DispatchOnce() error = %v, want context deadline exceeded", err)
	}
	if len(store.markCalls) != 0 || len(store.releaseCalls) != 0 {
		t.Fatalf("marks=%d releases=%d, want no transition after cancellation", len(store.markCalls), len(store.releaseCalls))
	}
}

func TestDispatcherRunSchedulesPublishedCleanup(t *testing.T) {
	store := &dispatcherStoreFake{cleanupCalled: make(chan struct{})}
	publisher := &dispatcherPublisherFake{}
	dispatcher := newDispatcherForTest(t, store, publisher)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()

	select {
	case <-store.cleanupCalled:
	case <-time.After(time.Second):
		t.Fatal("Run() did not schedule published cleanup")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cleanup cancellation")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.cleanupCalls) != 1 {
		t.Fatalf("cleanup calls=%d, want 1", len(store.cleanupCalls))
	}
	if store.cleanupCalls[0].batch != DefaultDispatcherCleanupBatch {
		t.Fatalf("cleanup batch=%d, want %d", store.cleanupCalls[0].batch, DefaultDispatcherCleanupBatch)
	}
}

func TestDispatcherRunStopsWithoutClaimingAfterCancellation(t *testing.T) {
	store := &dispatcherStoreFake{}
	publisher := &dispatcherPublisherFake{}
	dispatcher := newDispatcherForTest(t, store, publisher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
	if store.claimCalls != 0 {
		t.Fatalf("claim calls=%d, want zero", store.claimCalls)
	}
}

func TestNewDispatcherRejectsLeaseThatCannotCoverClaimBatch(t *testing.T) {
	store := &dispatcherStoreFake{}
	publisher := &dispatcherPublisherFake{}
	_, err := NewDispatcher(store, publisher, DispatcherOptions{
		Owner:          "dispatcher-test",
		PollInterval:   10 * time.Millisecond,
		ClaimBatch:     10,
		LeaseDuration:  30 * time.Second,
		PublishTimeout: 5 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "full claim batch publish budget") {
		t.Fatalf("NewDispatcher() error = %v, want batch publish budget validation", err)
	}
}
