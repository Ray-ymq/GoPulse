package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
)

type dispatcherStoreFake struct {
	mu sync.Mutex

	records       []Record
	claimSequence [][]Record
	claimErr      error
	markErr       error
	releaseErr    error
	cleanupErr    error
	cleanupResult int64
	cleanupCalled chan struct{}
	claimObserved chan time.Time
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
	source := store.records
	if store.claimSequence != nil {
		index := store.claimCalls - 1
		if index < len(store.claimSequence) {
			source = store.claimSequence[index]
		} else {
			source = nil
		}
	}
	records := make([]Record, len(source))
	copy(records, source)
	for index := range records {
		records[index].LeaseOwner = owner
	}
	if store.claimObserved != nil {
		store.claimObserved <- time.Now()
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
		Owner:           "dispatcher-test",
		DatabaseTimeout: 10 * time.Millisecond,
		PollInterval:    10 * time.Millisecond,
		ClaimBatch:      2,
		LeaseDuration:   2 * time.Second,
		PublishTimeout:  100 * time.Millisecond,
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
	var output bytes.Buffer
	dispatcher.logger = logging.Module(logging.New("backend", &output), "outbox")

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
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode structured log: %v", err)
	}
	if record["message"] != "outbox publish confirmed" || record["event_id"] != publisher.published[0].EventID || record["event_type"] != string(publisher.published[0].EventType) || record["outbox_id"] != float64(41) {
		t.Fatalf("published log = %#v", record)
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
	if strings.Contains(safeDispatcherReason(publisher.err), "password") {
		t.Fatal("safeDispatcherReason leaked publisher detail")
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
	dispatcher.cleanupStartupDelay = time.Millisecond
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

func TestDispatcherDrainsFullBatchesWithoutPollDelay(t *testing.T) {
	store := &dispatcherStoreFake{
		claimSequence: [][]Record{
			{dispatcherTestRecord(t, 47), dispatcherTestRecord(t, 48)},
			{},
		},
		claimObserved: make(chan time.Time, 2),
	}
	dispatcher := newDispatcherForTest(t, store, &dispatcherPublisherFake{})
	dispatcher.pollInterval = 500 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.runDelivery(ctx) }()

	var firstClaim, secondClaim time.Time
	select {
	case firstClaim = <-store.claimObserved:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not claim the first batch")
	}
	select {
	case secondClaim = <-store.claimObserved:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("dispatcher imposed the poll delay after a full batch")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runDelivery() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runDelivery() did not stop after cancellation")
	}

	if elapsed := secondClaim.Sub(firstClaim); elapsed >= dispatcher.pollInterval/2 {
		t.Fatalf("second claim elapsed=%s, want less than %s", elapsed, dispatcher.pollInterval/2)
	}
}

func TestNewDispatcherRejectsLeaseThatCannotCoverClaimBatch(t *testing.T) {
	store := &dispatcherStoreFake{}
	publisher := &dispatcherPublisherFake{}
	_, err := NewDispatcher(store, publisher, DispatcherOptions{
		Owner:           "dispatcher-test",
		DatabaseTimeout: 10 * time.Millisecond,
		PollInterval:    10 * time.Millisecond,
		ClaimBatch:      10,
		LeaseDuration:   30 * time.Second,
		PublishTimeout:  5 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "full claim batch publish budget") {
		t.Fatalf("NewDispatcher() error = %v, want batch publish budget validation", err)
	}
}

type batchDispatcherStore struct {
	*dispatcherStoreFake
	completed *atomic.Int32
	batches   int
}

func (s *batchDispatcherStore) MarkPublishedBatch(_ context.Context, ids []uint64, _ string) ([]error, error) {
	if int(s.completed.Load()) != len(s.records) {
		return nil, errors.New("ack before all publishers completed")
	}
	s.batches++
	s.markCalls = append(s.markCalls, ids...)
	return make([]error, len(ids)), nil
}

type exclusivePublisher struct {
	active    atomic.Bool
	completed *atomic.Int32
	entered   chan struct{}
	gate      <-chan struct{}
}

func (p *exclusivePublisher) Publish(ctx context.Context, _ bus.Envelope) error {
	if !p.active.CompareAndSwap(false, true) {
		return errors.New("publisher used concurrently")
	}
	defer p.active.Store(false)
	select {
	case p.entered <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.gate:
	}
	p.completed.Add(1)
	return nil
}
func TestParallelPublishersOwnStateAndAcknowledgeAfterWholeBatch(t *testing.T) {
	completed := &atomic.Int32{}
	entered := make(chan struct{}, 16)
	gate := make(chan struct{})
	records := []Record{}
	for i := 1; i <= 16; i++ {
		records = append(records, dispatcherTestRecord(t, uint64(i)))
	}
	store := &batchDispatcherStore{dispatcherStoreFake: &dispatcherStoreFake{records: records}, completed: completed}
	publishers := []Publisher{}
	for i := 0; i < 8; i++ {
		publishers = append(publishers, &exclusivePublisher{completed: completed, entered: entered, gate: gate})
	}
	dispatcher, err := NewDispatcher(store, publishers[0], DispatcherOptions{Publishers: publishers, ClaimBatch: 16})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- dispatcher.DispatchOnce(context.Background()) }()
	for i := 0; i < 8; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(gate)
			t.Fatal("publishers did not run concurrently")
		}
	}
	if completed.Load() != 0 {
		t.Fatal("publish completed before gate")
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if store.batches != 1 || len(store.markCalls) != 16 {
		t.Fatalf("batch calls=%d marked=%d", store.batches, len(store.markCalls))
	}
}
func TestStorageBackoffIsBoundedAndJittered(t *testing.T) {
	for attempt := uint(0); attempt < 16; attempt++ {
		cap := time.Second * time.Duration(1<<min(attempt, 5))
		if cap > 30*time.Second {
			cap = 30 * time.Second
		}
		for i := 0; i < 100; i++ {
			delay := storageBackoff(attempt)
			if delay < cap/2 || delay > cap {
				t.Fatalf("attempt=%d delay=%s", attempt, delay)
			}
		}
	}
}
