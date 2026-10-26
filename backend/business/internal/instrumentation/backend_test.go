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

func TestAsyncBudgetAvailabilityAndRecovery(t *testing.T) {
	b, err := NewBackend(nil)
	if err != nil {
		t.Fatal(err)
	}
	text := func() string {
		body, ok := b.Snapshot()
		if !ok {
			t.Fatal("snapshot unavailable")
		}
		return string(body)
	}
	if strings.Contains(text(), "\ngopulse_backend_async_budget_open_requirements ") {
		t.Fatal("unsampled budget exposed")
	}
	b.ObserveAsyncBudget(3, 2, 4, 4096, time.Unix(1700000000, 0), nil)
	for _, line := range []string{"open_requirements 3", "parked_messages 2", "retained_groups 4", "retained_bytes 4096", "sample_success 1", "last_success_timestamp_seconds 1.7e+09"} {
		if !strings.Contains(text(), "gopulse_backend_async_budget_"+line) {
			t.Fatal("valid budget absent", text())
		}
	}
	b.ObserveAsyncBudget(0, 0, 0, 0, time.Now(), errors.New("query failed"))
	if strings.Contains(text(), "\ngopulse_backend_async_budget_open_requirements ") || !strings.Contains(text(), "gopulse_backend_async_budget_sample_success 0") {
		t.Fatal("failure retained stale budget")
	}
	b.ObserveAsyncBudget(0, 0, 0, 0, time.Now(), nil)
	if !strings.Contains(text(), "gopulse_backend_async_budget_open_requirements 0") || !strings.Contains(text(), "gopulse_backend_async_budget_sample_success 1") {
		t.Fatal("valid zero lost")
	}
	b.ObserveAsyncBudget(1, 1, 1, 1, time.Time{}, nil)
	if strings.Contains(text(), "\ngopulse_backend_async_budget_open_requirements ") {
		t.Fatal("missing timestamp accepted")
	}
}
