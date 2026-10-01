package consumer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
)

type fakeDecoder struct {
	message envelope.Envelope
	err     error
}

func (f fakeDecoder) Decode([]byte, []byte) (envelope.Envelope, error) { return f.message, f.err }

type fakeTransformer struct {
	body []byte
	err  error
}

func (f fakeTransformer) Transform(envelope.Envelope) ([]byte, error) { return f.body, f.err }

type fakeWriter struct {
	mu      sync.Mutex
	errors  []error
	calls   int
	onWrite func()
}

type writerFunc func(context.Context, []byte) error

func (f writerFunc) Write(ctx context.Context, body []byte) error { return f(ctx, body) }

func (f *fakeWriter) Write(context.Context, []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.onWrite != nil {
		f.onWrite()
	}
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		return err
	}
	return nil
}

type fakeCommitter struct {
	calls    int
	err      error
	onCommit func(context.Context) error
}

func (f *fakeCommitter) Commit(ctx context.Context, _ Record) error {
	f.calls++
	if f.onCommit != nil {
		return f.onCommit(ctx)
	}
	return f.err
}
func leaseFor(t *testing.T) (*Ownership, Lease) {
	t.Helper()
	o := NewOwnership()
	p := Partition{Topic: "topic", Partition: 0}
	o.Assign([]Partition{p})
	l, ok := o.Lease(p)
	if !ok {
		t.Fatal("missing lease")
	}
	return o, l
}
func baseProcessor(writer Writer, committer Committer) *Processor {
	return &Processor{Decoder: fakeDecoder{message: envelope.Envelope{}}, Transformer: fakeTransformer{body: []byte("metric 1\n")}, Writer: writer, Committer: committer, RetryMin: time.Millisecond, RetryMax: 2 * time.Millisecond, Sleep: func(context.Context, time.Duration) error { return nil }}
}
func TestProcessorAcceptedWritesBeforeCommit(t *testing.T) {
	_, lease := leaseFor(t)
	writer := &fakeWriter{}
	committer := &fakeCommitter{}
	if err := baseProcessor(writer, committer).Handle(context.Background(), Record{}, lease); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || committer.calls != 1 {
		t.Fatalf("writes=%d commits=%d", writer.calls, committer.calls)
	}
}
func TestProcessorPermanentErrorSkipsWriterAndCommits(t *testing.T) {
	_, lease := leaseFor(t)
	writer := &fakeWriter{}
	committer := &fakeCommitter{}
	p := baseProcessor(writer, committer)
	p.Decoder = fakeDecoder{err: &envelope.PermanentError{Code: "invalid_json"}}
	if err := p.Handle(context.Background(), Record{}, lease); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 0 || committer.calls != 1 {
		t.Fatalf("writes=%d commits=%d", writer.calls, committer.calls)
	}
}
func TestProcessorTransientFailureRetriesWithoutEarlyCommit(t *testing.T) {
	_, lease := leaseFor(t)
	writer := &fakeWriter{errors: []error{errors.New("temporary"), nil}}
	committer := &fakeCommitter{}
	if err := baseProcessor(writer, committer).Handle(context.Background(), Record{}, lease); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 2 || committer.calls != 1 {
		t.Fatalf("writes=%d commits=%d", writer.calls, committer.calls)
	}
}

func TestProcessorPermanentStorageFailureCommitsWithoutRetry(t *testing.T) {
	_, lease := leaseFor(t)
	writer := &fakeWriter{errors: []error{&envelope.PermanentError{Code: "expired_log_retention"}}}
	committer := &fakeCommitter{}
	p := baseProcessor(writer, committer)
	if err := p.Handle(context.Background(), Record{}, lease); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || committer.calls != 1 {
		t.Fatalf("writes=%d commits=%d", writer.calls, committer.calls)
	}
}
func TestProcessorCommitFailureHalts(t *testing.T) {
	_, lease := leaseFor(t)
	committer := &fakeCommitter{err: errors.New("commit")}
	err := baseProcessor(&fakeWriter{}, committer).Handle(context.Background(), Record{}, lease)
	if !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("expected commit failure, got %v", err)
	}
}
func TestProcessorLateAcceptanceAfterRevokeDoesNotCommit(t *testing.T) {
	owner, lease := leaseFor(t)
	committer := &fakeCommitter{}
	writer := &fakeWriter{onWrite: func() { owner.Revoke([]Partition{{Topic: "topic", Partition: 0}}) }}
	err := baseProcessor(writer, committer).Handle(context.Background(), Record{}, lease)
	if !errors.Is(err, ErrOwnershipLost) || committer.calls != 0 {
		t.Fatalf("err=%v commits=%d", err, committer.calls)
	}
}
func TestProcessorRetryBackoffCancelsOnLostOwnership(t *testing.T) {
	owner, lease := leaseFor(t)
	committer := &fakeCommitter{}
	writer := &fakeWriter{errors: []error{errors.New("temporary")}}
	p := baseProcessor(writer, committer)
	p.Sleep = func(ctx context.Context, _ time.Duration) error {
		owner.Lose([]Partition{{Topic: "topic", Partition: 0}})
		<-ctx.Done()
		return ctx.Err()
	}
	err := p.Handle(context.Background(), Record{}, lease)
	if !errors.Is(err, ErrOwnershipLost) || committer.calls != 0 {
		t.Fatalf("err=%v commits=%d", err, committer.calls)
	}
}

func TestProcessorCommitCancellationFromOwnershipChangeCanRecover(t *testing.T) {
	partition := Partition{Topic: "topic", Partition: 0}
	tests := map[string]func(*Ownership){
		"revoke": func(owner *Ownership) { owner.Revoke([]Partition{partition}) },
		"lost":   func(owner *Ownership) { owner.Lose([]Partition{partition}) },
	}
	for name, changeOwnership := range tests {
		t.Run(name, func(t *testing.T) {
			owner, lease := leaseFor(t)
			committer := &fakeCommitter{}
			committer.onCommit = func(ctx context.Context) error {
				changeOwnership(owner)
				<-ctx.Done()
				return ctx.Err()
			}

			err := baseProcessor(&fakeWriter{}, committer).Handle(context.Background(), Record{}, lease)
			if !errors.Is(err, ErrOwnershipLost) || committer.calls != 1 {
				t.Fatalf("err=%v commits=%d", err, committer.calls)
			}

			owner.Assign([]Partition{partition})
			newLease, ok := owner.Lease(partition)
			if !ok {
				t.Fatal("missing replacement lease")
			}
			committer.onCommit = nil
			if err := baseProcessor(&fakeWriter{}, committer).Handle(context.Background(), Record{}, newLease); err != nil {
				t.Fatalf("replacement assignment did not recover: %v", err)
			}
			if committer.calls != 2 {
				t.Fatalf("commits=%d, want canceled old attempt plus replacement commit", committer.calls)
			}
		})
	}
}

func TestCommitRetriesWithoutRewriting(t *testing.T) {
	_, lease := leaseFor(t)
	writer := &fakeWriter{}
	committer := &fakeCommitter{}
	committer.onCommit = func(context.Context) error {
		if committer.calls == 1 {
			return errors.New("temporary")
		}
		return nil
	}
	if err := baseProcessor(writer, committer).Handle(context.Background(), Record{}, lease); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || committer.calls != 2 {
		t.Fatalf("writes=%d commits=%d", writer.calls, committer.calls)
	}
}
func TestCommitRetryExhaustionIsBounded(t *testing.T) {
	_, lease := leaseFor(t)
	committer := &fakeCommitter{err: errors.New("temporary")}
	err := baseProcessor(&fakeWriter{}, committer).Handle(context.Background(), Record{}, lease)
	if !errors.Is(err, ErrCommitFailed) || committer.calls != 3 {
		t.Fatalf("err=%v commits=%d", err, committer.calls)
	}
}

func TestProcessorBoundsConcurrentRetrySleepers(t *testing.T) {
	owner := NewOwnership()
	partitions := []Partition{{Topic: "topic", Partition: 0}, {Topic: "topic", Partition: 1}, {Topic: "topic", Partition: 2}, {Topic: "topic", Partition: 3}}
	owner.Assign(partitions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, len(partitions))
	p := baseProcessor(writerFunc(func(context.Context, []byte) error { return errors.New("temporary") }), &fakeCommitter{})
	p.MaxRetrying = 2
	p.Sleep = func(ctx context.Context, _ time.Duration) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		active.Add(-1)
		return ctx.Err()
	}
	var workers sync.WaitGroup
	errorsDone := make(chan error, len(partitions))
	for _, partition := range partitions {
		lease, ok := owner.Lease(partition)
		if !ok {
			t.Fatalf("missing lease for partition %d", partition.Partition)
		}
		workers.Add(1)
		go func(lease Lease) {
			defer workers.Done()
			errorsDone <- p.Handle(ctx, Record{Partition: lease.key.Partition}, lease)
		}(lease)
	}
	deadline := time.After(time.Second)
	for i := 0; i < p.MaxRetrying; i++ {
		select {
		case <-entered:
		case <-deadline:
			t.Fatal("retry sleepers did not reach the configured bound")
		}
	}
	if maximum.Load() != int32(p.MaxRetrying) {
		t.Fatalf("maximum retry sleepers=%d, want %d", maximum.Load(), p.MaxRetrying)
	}
	cancel()
	workers.Wait()
	close(errorsDone)
	for err := range errorsDone {
		if !errors.Is(err, ErrOwnershipLost) {
			t.Fatalf("Handle() error=%v, want ownership loss after cancellation", err)
		}
	}
}

func TestShutdownCancelsStorageBackoff(t *testing.T) {
	_, lease := leaseFor(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &fakeWriter{errors: []error{errors.New("temporary")}}
	committer := &fakeCommitter{}
	p := baseProcessor(writer, committer)
	p.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); <-ctx.Done(); return ctx.Err() }
	if err := p.Handle(ctx, Record{}, lease); !errors.Is(err, ErrOwnershipLost) {
		t.Fatal(err)
	}
	if committer.calls != 0 {
		t.Fatal("shutdown committed failed write")
	}
}
