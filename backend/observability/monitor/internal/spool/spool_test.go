package spool

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
)

type senderFunc func(context.Context, publisher.Message) error

func (f senderFunc) Send(ctx context.Context, message publisher.Message) error {
	return f(ctx, message)
}

func rawMessage(id, kind string) map[string]any {
	return map[string]any{
		"schema_version": 2,
		"message_id":     id,
		"type":           kind,
		"source":         "monitor",
		"timestamp":      "2026-10-17T00:00:00Z",
		"payload":        map[string]any{"value": 1},
	}
}

func TestQueueRestartPreservesBodyAndMarksPendingWorkOld(t *testing.T) {
	dir := t.TempDir()
	queue, err := openQueue(dir, Logs, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	message := publisher.Message{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Type: "logs", Body: []byte(`{"body":"original"}`)}
	if _, err := queue.enqueue(message, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := queue.close(); err != nil {
		t.Fatal(err)
	}
	queue, err = openQueue(dir, Logs, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.close() }()
	if _, found, err := queue.next(true); err != nil || found {
		t.Fatalf("new queue scan found=%v err=%v", found, err)
	}
	entry, found, err := queue.next(false)
	if err != nil || !found || string(entry.Body) != string(message.Body) || entry.MessageID != message.ID {
		t.Fatalf("entry=%+v found=%v err=%v", entry, found, err)
	}
}

func TestQueueEvictsOnlyItsOwnOldestRecords(t *testing.T) {
	dir := t.TempDir()
	metrics, err := openQueue(dir, Metrics, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := openQueue(dir, Logs, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = metrics.close() }()
	defer func() { _ = logs.close() }()
	body := make([]byte, 700<<10)
	for index, kind := range []Kind{Metrics, Metrics, Logs} {
		queue := metrics
		if kind == Logs {
			queue = logs
		}
		id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + string(rune('1'+index))
		if _, err := queue.enqueue(publisher.Message{ID: id, Type: string(kind), Body: body}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	metricStats, err := metrics.stats()
	if err != nil {
		t.Fatal(err)
	}
	logStats, err := logs.stats()
	if err != nil {
		t.Fatal(err)
	}
	if metricStats.Pending != 1 || metricStats.Evicted != 1 || logStats.Pending != 1 || logStats.Evicted != 0 {
		t.Fatalf("metrics=%+v logs=%+v", metricStats, logStats)
	}
}

func TestQueueDoesNotEvictInFlightRecord(t *testing.T) {
	queue, err := openQueue(t.TempDir(), Logs, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.close() }()
	oldID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if _, err := queue.enqueue(publisher.Message{ID: oldID, Type: "logs", Body: make([]byte, 1<<20)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	entry, found, err := queue.claim(false)
	if err != nil || !found {
		t.Fatalf("claim entry=%+v found=%v err=%v", entry, found, err)
	}
	if _, err := queue.enqueue(publisher.Message{ID: "ffffffffffffffffffffffffffffffff", Type: "logs", Body: make([]byte, 128<<10)}, time.Now()); !errors.Is(err, ErrQueueBusy) {
		t.Fatalf("in-flight record eviction error=%v, want %v", err, ErrQueueBusy)
	}
	queue.release(entry)
	if _, err := queue.enqueue(publisher.Message{ID: "ffffffffffffffffffffffffffffffff", Type: "logs", Body: make([]byte, 128<<10)}, time.Now()); err != nil {
		t.Fatalf("enqueue after release: %v", err)
	}
}

func TestManagerRetriesAndCountsKafkaAcknowledgementSeparately(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	delivered := make(chan struct{}, 1)
	sender := senderFunc(func(_ context.Context, message publisher.Message) error {
		mu.Lock()
		attempts++
		current := attempts
		mu.Unlock()
		if current == 1 {
			return errors.New("Kafka unavailable")
		}
		delivered <- struct{}{}
		return nil
	})
	manager, err := New(Config{Dir: t.TempDir(), MetricsMaxSize: 2 << 20, LogsMaxSize: 2 << 20, EventsMaxSize: 2 << 20, Sender: sender, RetryMin: time.Millisecond, RetryMax: 2 * time.Millisecond, Jitter: func(time.Duration) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.PublishRaw(context.Background(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", rawMessage("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "logs")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("message was not retried")
	}
	stats := manager.Status().Queues[1]
	if stats.Enqueued != 1 || stats.Sent != 1 || stats.Failed < 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerRestartResendsPendingRecord(t *testing.T) {
	dir := t.TempDir()
	firstManager, err := New(Config{
		Dir: dir, MetricsMaxSize: 2 << 20, LogsMaxSize: 2 << 20, EventsMaxSize: 2 << 20,
		Sender:   senderFunc(func(context.Context, publisher.Message) error { return errors.New("Kafka unavailable") }),
		RetryMin: time.Millisecond, RetryMax: time.Millisecond, Jitter: func(time.Duration) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstManager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := firstManager.PublishRaw(context.Background(), "dddddddddddddddddddddddddddddddd", rawMessage("dddddddddddddddddddddddddddddddd", "logs")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for firstManager.Status().Queues[1].Failed == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if firstManager.Status().Queues[1].Failed == 0 {
		t.Fatal("first manager did not record a failed delivery")
	}
	if err := firstManager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	delivered := make(chan publisher.Message, 1)
	secondManager, err := New(Config{
		Dir: dir, MetricsMaxSize: 2 << 20, LogsMaxSize: 2 << 20, EventsMaxSize: 2 << 20,
		Sender:   senderFunc(func(_ context.Context, message publisher.Message) error { delivered <- message; return nil }),
		RetryMin: time.Millisecond, RetryMax: time.Millisecond, Jitter: func(time.Duration) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := secondManager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondManager.Close(context.Background()) }()
	expectedBody, _ := json.Marshal(rawMessage("dddddddddddddddddddddddddddddddd", "logs"))
	select {
	case message := <-delivered:
		if message.ID != "dddddddddddddddddddddddddddddddd" || string(message.Body) != string(expectedBody) {
			t.Fatalf("recovered message=%+v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("pending record was not resent after restart")
	}
}

func TestQueueWriteFailureIsNotReportedAsAccepted(t *testing.T) {
	queue, err := openQueue(t.TempDir(), Metrics, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.enqueue(publisher.Message{ID: "cccccccccccccccccccccccccccccccc", Type: "metrics", Body: []byte("metric")}, time.Now()); err == nil {
		t.Fatal("closed database accepted a message")
	}
}
