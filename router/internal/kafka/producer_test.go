package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type pendingProduce struct {
	record   *kgo.Record
	callback func(*kgo.Record, error)
}

type fakeClient struct {
	mu       sync.Mutex
	pending  []pendingProduce
	pinged   chan struct{}
	tryErr   error
	flushErr error
	aborted  bool
	closed   bool
}

func (f *fakeClient) TryProduce(_ context.Context, record *kgo.Record, callback func(*kgo.Record, error)) {
	if f.tryErr != nil {
		callback(record, f.tryErr)
		return
	}
	f.mu.Lock()
	f.pending = append(f.pending, pendingProduce{record: record, callback: callback})
	f.mu.Unlock()
}
func (f *fakeClient) Ping(context.Context) error {
	if f.pinged != nil {
		select {
		case f.pinged <- struct{}{}:
		default:
		}
	}
	return nil
}
func (f *fakeClient) Request(context.Context, kmsg.Request) (kmsg.Response, error) {
	response := kmsg.NewPtrMetadataResponse()
	response.Topics = []kmsg.MetadataResponseTopic{{Partitions: []kmsg.MetadataResponseTopicPartition{{Leader: 1}}}}
	return response, nil
}
func (f *fakeClient) Flush(context.Context) error { return f.flushErr }
func (f *fakeClient) UnsafeAbortBufferedRecords() { f.aborted = true }
func (f *fakeClient) Close()                      { f.closed = true }
func (f *fakeClient) count() int                  { f.mu.Lock(); defer f.mu.Unlock(); return len(f.pending) }
func (f *fakeClient) complete(index int, err error) {
	f.mu.Lock()
	pending := f.pending[index]
	f.mu.Unlock()
	pending.callback(pending.record, err)
}

func TestProduceAllowsConcurrentBufferedRecords(t *testing.T) {
	client := &fakeClient{}
	producer := &Producer{client: client, produceTimeout: time.Second}
	done := make(chan error, 2)
	go func() { done <- producer.Produce(context.Background(), "topic", "one", []byte("one")) }()
	go func() { done <- producer.Produce(context.Background(), "topic", "two", []byte("two")) }()
	deadline := time.After(time.Second)
	for client.count() != 2 {
		select {
		case <-deadline:
			t.Fatal("produces were serialized before the client buffer")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	client.complete(0, nil)
	client.complete(1, nil)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("Produce() error = %v", err)
		}
	}
}

func TestProduceRejectsFullBufferImmediately(t *testing.T) {
	client := &fakeClient{tryErr: kgo.ErrMaxBuffered}
	producer := &Producer{client: client, produceTimeout: time.Second}
	started := time.Now()
	err := producer.Produce(context.Background(), "topic", "key", []byte("value"))
	if !errors.Is(err, kgo.ErrMaxBuffered) || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("Produce() error=%v duration=%s", err, time.Since(started))
	}
}

func TestCanceledCallerDoesNotAbortOtherRecordsOrReadiness(t *testing.T) {
	client := &fakeClient{pinged: make(chan struct{}, 1)}
	producer := &Producer{client: client, produceTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- producer.Produce(ctx, "topic", "one", []byte("one")) }()
	for client.count() != 1 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Produce() error = %v", err)
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- producer.Produce(context.Background(), "topic", "two", []byte("two")) }()
	for client.count() != 2 {
		time.Sleep(time.Millisecond)
	}
	readyDone := make(chan error, 1)
	go func() { readyDone <- producer.Ready(context.Background(), "topic") }()
	select {
	case err := <-readyDone:
		if err != nil {
			t.Fatalf("Ready() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("readiness was blocked by a pending produce")
	}
	client.complete(1, nil)
	if err := <-secondDone; err != nil {
		t.Fatalf("second Produce() error = %v", err)
	}
	client.complete(0, nil)
	if client.aborted {
		t.Fatal("request cancellation aborted unrelated records")
	}
}

func TestCloseAbortsOnlyAfterBoundedFlushFailure(t *testing.T) {
	client := &fakeClient{flushErr: context.DeadlineExceeded}
	producer := &Producer{client: client, produceTimeout: time.Second}
	if err := producer.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v", err)
	}
	if !client.aborted || !client.closed {
		t.Fatalf("abort=%t closed=%t", client.aborted, client.closed)
	}
}

func TestConfiguredRecordLimitRejectsWithoutHandlerQueue(t *testing.T) {
	t.Parallel()
	producer, err := New(Config{Brokers: []string{"127.0.0.1:1"}, ProduceTimeout: time.Second, MaxBufferedRecords: 1, MaxBufferedBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	client := producer.client.(*kgo.Client)
	defer client.Close()
	firstDone := make(chan error, 1)
	go func() { firstDone <- producer.Produce(context.Background(), "topic", "one", []byte("one")) }()
	waitForBufferedRecords(t, client, 1)
	started := time.Now()
	err = producer.Produce(context.Background(), "topic", "two", []byte("two"))
	if !errors.Is(err, kgo.ErrMaxBuffered) || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("second Produce() error=%v duration=%s", err, time.Since(started))
	}
	<-firstDone
}

func TestConfiguredByteLimitRejectsWithoutHandlerQueue(t *testing.T) {
	t.Parallel()
	producer, err := New(Config{Brokers: []string{"127.0.0.1:1"}, ProduceTimeout: time.Second, MaxBufferedRecords: 10, MaxBufferedBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	client := producer.client.(*kgo.Client)
	defer client.Close()
	value := make([]byte, 700<<10)
	firstDone := make(chan error, 1)
	go func() { firstDone <- producer.Produce(context.Background(), "topic", "one", value) }()
	waitForBufferedRecords(t, client, 1)
	started := time.Now()
	err = producer.Produce(context.Background(), "topic", "two", value)
	if !errors.Is(err, kgo.ErrMaxBuffered) || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("second Produce() error=%v duration=%s", err, time.Since(started))
	}
	<-firstDone
}

func waitForBufferedRecords(t *testing.T, client *kgo.Client, expected int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for client.BufferedProduceRecords() != expected {
		if time.Now().After(deadline) {
			t.Fatalf("BufferedProduceRecords()=%d, want %d", client.BufferedProduceRecords(), expected)
		}
		time.Sleep(time.Millisecond)
	}
}
