package telemetrydelivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type sourceFunc struct {
	component string
	instance  string
	item      Item
	err       error
}

func (s sourceFunc) Identity() (string, string)          { return s.component, s.instance }
func (s sourceFunc) Fetch(context.Context) (Item, error) { return s.item, s.err }

func TestSnapshotKeepsSourceFailuresIndependent(t *testing.T) {
	service := New([]Source{
		sourceFunc{component: "monitor", instance: "monitor-1", item: Item{Component: "monitor", Instance: "monitor-1", Status: "healthy"}},
		sourceFunc{component: "marshaller", instance: "marshaller-1", err: context.DeadlineExceeded},
	})
	snapshot := service.Snapshot(context.Background())
	if snapshot.Status != "degraded" || snapshot.Items[0].Status != "healthy" || snapshot.Items[1].Status != "unknown" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestHTTPSourceDerivesMonitorStatusFromQueueBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing source authorization")
		}
		_, _ = w.Write([]byte(`{"data":{"component":"monitor","instance":"monitor-1","observed_at":"2026-10-17T00:00:00Z","queues":[{"kind":"logs","pending":2,"occupied_bytes":4,"capacity_bytes":8,"file_bytes":4096,"enqueued":2,"sent":0,"failed":1,"evicted":0,"write_blocked":true,"reason_code":"disk_full"}]}}`))
	}))
	defer server.Close()
	source, err := NewHTTPSource("monitor", "monitor-1", server.URL, "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	item, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "degraded" || item.ReasonCode != "disk_full" || !item.Queues[0].WriteBlocked {
		t.Fatalf("item=%+v", item)
	}
}

func TestHTTPSourceRejectsWrongInstanceAndTrailingJSON(t *testing.T) {
	for _, body := range []string{
		`{"data":{"component":"monitor","instance":"monitor-2","observed_at":"2026-10-17T00:00:00Z","queues":[]}}`,
		`{"data":{"component":"monitor","instance":"monitor-1","observed_at":"2026-10-17T00:00:00Z","queues":[]}} {"extra":true}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		source, err := NewHTTPSource("monitor", "monitor-1", server.URL, "", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Fetch(context.Background()); err == nil {
			t.Fatalf("accepted invalid delivery response: %s", body)
		}
		server.Close()
	}
}
