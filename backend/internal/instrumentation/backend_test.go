package instrumentation

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUnknownOutboxDoesNotHideLiveServiceOrCreateZero(t *testing.T) {
	b, err := NewBackend([]Route{{"GET", "/posts/:id"}})
	if err != nil {
		t.Fatal(err)
	}
	b.ObserveRequest("GET", "/posts/:id", 200, 100*time.Millisecond)
	b.ObserveRequest("GET", "/private?secret=x", 404, time.Second)
	body, ok := b.Snapshot()
	if !ok {
		t.Fatal("unknown outbox hid running service")
	}
	text := string(body)
	if strings.Contains(text, "\ngopulse_backend_outbox_pending ") || strings.Contains(text, "private?") || strings.Contains(text, "secret=x") {
		t.Fatal("unknown or unbounded data exposed")
	}
	if !strings.Contains(text, "# TYPE gopulse_backend_http_request_duration_seconds histogram") || !strings.Contains(text, `method="GET",route="/posts/:id",status_class="2xx",le="+Inf"} 1`) {
		t.Fatal("missing standard histogram", text)
	}
	b.ObserveOutbox(5, time.Second, nil)
	body, _ = b.Snapshot()
	if !strings.Contains(string(body), "gopulse_backend_outbox_pending 5") {
		t.Fatal("valid outbox omitted")
	}
	b.ObserveOutbox(0, 0, errors.New("mysql unavailable"))
	body, ok = b.Snapshot()
	if !ok || strings.Contains(string(body), "\ngopulse_backend_outbox_pending ") {
		t.Fatal("failed outbox fabricated zero or broke scrape")
	}
}
