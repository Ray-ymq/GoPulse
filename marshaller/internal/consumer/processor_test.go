package consumer

import (
	"context"
	"errors"
	"sync"
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
	calls int
	err   error
}

func (f *fakeCommitter) Commit(context.Context, Record) error { f.calls++; return f.err }
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
