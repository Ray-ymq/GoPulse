package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type senderFunc func(context.Context, string, any) error

func (f senderFunc) PublishRaw(ctx context.Context, id string, body any) error {
	return f(ctx, id, body)
}

type permanentFailure struct{}

func (permanentFailure) Error() string   { return "rejected" }
func (permanentFailure) Permanent() bool { return true }

func testEvent(name string) Event {
	from, to, version, previous := "stopped", "running", "1.2.3", ""
	if name == "exporter_plugin_installed" {
		from = "not_installed"
	}
	if name == "exporter_plugin_stopped" {
		from, to = "running", "stopped"
	}
	if name == "exporter_plugin_updated" {
		from, to, previous = "running", "running", "1.2.2"
	}
	return New(name, version, previous, from, to, time.Date(2026, 9, 5, 8, 0, 0, 123, time.UTC))
}

func TestContractAndCanonicalEnvelope(t *testing.T) {
	now := time.Date(2026, 9, 5, 8, 1, 0, 0, time.UTC)
	for _, name := range []string{"exporter_plugin_installed", "exporter_plugin_started", "exporter_plugin_stopped", "exporter_plugin_updated"} {
		event := testEvent(name)
		body, err := CanonicalEnvelope(event, "0123456789abcdef0123456789abcdef", now)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		envelope, err := DecodeStrict(body, now)
		if err != nil || envelope.Payload.EventName != name || envelope.Timestamp != event.Timestamp {
			t.Fatalf("%s decode=%+v err=%v", name, envelope, err)
		}
	}
	bad := testEvent("exporter_plugin_started")
	bad.Message = "free text"
	if Validate(bad, now) == nil {
		t.Fatal("free-text message was accepted")
	}
	body, _ := CanonicalEnvelope(testEvent("exporter_plugin_started"), "0123456789abcdef0123456789abcdef", now)
	duplicate := bytes.Replace(body, []byte(`"source":"monitor"`), []byte(`"source":"monitor","source":"monitor"`), 1)
	if _, err := DecodeStrict(duplicate, now); err == nil {
		t.Fatal("duplicate field was accepted")
	}
}

func TestMonitorRetriesSameItemAndContinuesAfterPermanentRejection(t *testing.T) {
	var mu sync.Mutex
	var ids []string
	var bodies []string
	attempt := 0
	sender := senderFunc(func(_ context.Context, id string, body any) error {
		mu.Lock()
		defer mu.Unlock()
		ids = append(ids, id)
		bodies = append(bodies, string(body.(json.RawMessage)))
		attempt++
		if attempt == 1 {
			return errors.New("temporary")
		}
		if attempt == 3 {
			return permanentFailure{}
		}
		return nil
	})
	monitor, err := NewMonitor(Config{Capacity: 4, Timeout: time.Second, RetryMin: time.Millisecond, RetryMax: time.Millisecond, Now: func() time.Time { return time.Date(2026, 9, 5, 8, 1, 0, 0, time.UTC) }, Jitter: func(time.Duration) time.Duration { return 0 }, Sender: sender, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if !monitor.Record(testEvent("exporter_plugin_started")) || !monitor.Record(testEvent("exporter_plugin_stopped")) || !monitor.Record(testEvent("exporter_plugin_installed")) {
		t.Fatal("valid event was rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := monitor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 4 || ids[0] != ids[1] || bodies[0] != bodies[1] || ids[2] == ids[3] {
		t.Fatalf("unexpected attempts ids=%v", ids)
	}
}

func TestMonitorQueueFullAndCloseAreBounded(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	monitor, err := NewMonitor(Config{Capacity: 2, Timeout: time.Second, Now: func() time.Time { return time.Date(2026, 9, 5, 8, 1, 0, 0, time.UTC) }, Sender: senderFunc(func(ctx context.Context, _ string, _ any) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if !monitor.Record(testEvent("exporter_plugin_started")) {
		t.Fatal("first record rejected")
	}
	<-entered
	if !monitor.Record(testEvent("exporter_plugin_stopped")) {
		t.Fatal("queue should accept one waiting record")
	}
	start := time.Now()
	if monitor.Record(testEvent("exporter_plugin_installed")) {
		t.Fatal("full queue accepted another record")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("Record blocked on a full queue")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := monitor.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error=%v", err)
	}
	close(release)
	if monitor.Record(testEvent("exporter_plugin_started")) {
		t.Fatal("closed monitor accepted a record")
	}
}
