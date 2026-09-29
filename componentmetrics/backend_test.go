package componentmetrics

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBackendInitialSnapshotAndOutboxRecovery(t *testing.T) {
	metrics, err := NewBackend([]Route{{Method: "GET", Template: "/posts/:postId"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := metrics.Snapshot(); ok {
		t.Fatal("unknown outbox presented as an empty queue")
	}
	for i := range metrics.dependencies {
		if metrics.dependencies[i].Load() != -1 {
			t.Fatal("dependency did not start unknown")
		}
	}
	metrics.ObserveOutbox(0, 0, nil)
	body, ok := metrics.Snapshot()
	if !ok {
		t.Fatal("valid empty queue unavailable")
	}
	text := string(body)
	for _, want := range []string{
		"gopulse_backend_outbox_pending 0\n",
		"gopulse_backend_outbox_oldest_age_seconds 0\n",
		"gopulse_backend_outbox_last_publish_success_timestamp_seconds 0\n",
		"gopulse_backend_dependency_up{dependency=\"mysql\"} 1\n",
		"gopulse_backend_dependency_up{dependency=\"redis\"} -1\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing initial metric %q", want)
		}
	}
	if strings.Contains(text, "gopulse_backend_http_requests_total{") {
		t.Fatal("unobserved tuple eagerly emitted")
	}
	metrics.ObserveOutbox(0, 0, errors.New("private database failure"))
	if _, ok := metrics.Snapshot(); ok {
		t.Fatal("failed sample served stale or fabricated queue")
	}
	metrics.ObserveOutbox(2, 3*time.Second, nil)
	metrics.ObservePublish(time.Unix(123, 0), nil)
	metrics.ObserveDependency("redis", errors.New("secret cache key"))
	body, ok = metrics.Snapshot()
	if !ok || !strings.Contains(string(body), "gopulse_backend_outbox_pending 2\n") || !strings.Contains(string(body), "gopulse_backend_outbox_last_publish_success_timestamp_seconds 123\n") || !strings.Contains(string(body), "gopulse_backend_dependency_up{dependency=\"redis\"} 0\n") {
		t.Fatal("recovery/actual interaction not reflected")
	}
	if strings.Contains(string(body), "secret") || strings.Contains(string(body), "private") {
		t.Fatal("raw dependency error leaked")
	}
}

func TestBackendFixedRequestTuplesAndPairedUpdates(t *testing.T) {
	metrics, err := NewBackend([]Route{{Method: "GET", Template: "/posts/:postId"}})
	if err != nil {
		t.Fatal(err)
	}
	metrics.ObserveOutbox(0, 0, nil)
	metrics.ObserveRequest("GET", "/posts/:postId", 200, 250*time.Millisecond)
	metrics.ObserveRequest("GET", "/posts/:postId", 200, 750*time.Millisecond)
	// Neither a business ID nor an unknown HTTP method can create a label value.
	metrics.ObserveRequest("GET", "/posts/private-post-id?secret=1", 404, time.Second)
	metrics.ObserveRequest("private-method", "/private", 404, time.Second)
	body, _ := metrics.Snapshot()
	text := string(body)
	for _, want := range []string{
		"gopulse_backend_http_requests_total{method=\"GET\",route=\"/posts/:postId\",status_class=\"2xx\"} 2\n",
		"gopulse_backend_http_request_duration_seconds_total{method=\"GET\",route=\"/posts/:postId\",status_class=\"2xx\"} 1\n",
		"gopulse_backend_http_requests_total{method=\"GET\",route=\"_unmatched\",status_class=\"4xx\"} 1\n",
		"gopulse_backend_http_requests_total{method=\"unknown\",route=\"_unmatched\",status_class=\"4xx\"} 1\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(text, "private") || strings.Contains(text, "secret") {
		t.Fatal("untrusted label value escaped")
	}
	if metrics.MaxSamples() != 896 {
		t.Fatalf("sample budget = %d, want 896", metrics.MaxSamples())
	}
	for _, want := range []string{
		`gopulse_backend_http_request_duration_seconds_bucket{method="GET",route="/posts/:postId",status_class="2xx",le="0.25"} 1`,
		`gopulse_backend_http_request_duration_seconds_bucket{method="GET",route="/posts/:postId",status_class="2xx",le="+Inf"} 2`,
		`gopulse_backend_http_request_duration_seconds_count{method="GET",route="/posts/:postId",status_class="2xx"} 2`,
		`gopulse_backend_http_request_duration_seconds_sum{method="GET",route="/posts/:postId",status_class="2xx"} 1`,
		`gopulse_backend_http_requests_in_flight 0`,
		`gopulse_backend_http_concurrency_limit 0`,
		`gopulse_backend_http_rejected_total 0`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing latency or saturation metric %q", want)
		}
	}
}

func TestBackendConcurrentCompletion(t *testing.T) {
	metrics, err := NewBackend([]Route{{Method: "GET", Template: "/health"}})
	if err != nil {
		t.Fatal(err)
	}
	metrics.ObserveOutbox(0, 0, nil)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				metrics.ObserveRequest("GET", "/health", 200, time.Second)
				_, _ = metrics.Snapshot()
			}
		}()
	}
	workers.Wait()
	body, _ := metrics.Snapshot()
	for _, name := range []string{"http_requests_total", "http_request_duration_seconds_total"} {
		if !strings.Contains(string(body), "gopulse_backend_"+name+"{method=\"GET\",route=\"/health\",status_class=\"2xx\"} 400\n") {
			t.Fatal("concurrent count/duration update lost")
		}
	}
	if !strings.Contains(string(body), `gopulse_backend_http_request_duration_seconds_count{method="GET",route="/health",status_class="2xx"} 400`) {
		t.Fatal("concurrent histogram count update lost")
	}
	if !strings.Contains(string(body), `gopulse_backend_http_request_duration_seconds_bucket{method="GET",route="/health",status_class="2xx",le="+Inf"} 400`) {
		t.Fatal("concurrent histogram bucket update lost")
	}
}

func TestBackendSaturationSignalsAreBoundedAndConcurrent(t *testing.T) {
	metrics, err := NewBackend([]Route{{Method: "GET", Template: "/health"}})
	if err != nil {
		t.Fatal(err)
	}
	metrics.SetHTTPConcurrencyLimit(2)
	metrics.ObserveOutbox(0, 0, nil)
	metrics.ObserveHTTPStarted()
	metrics.ObserveHTTPStarted()
	metrics.ObserveHTTPRejected()
	metrics.ObserveHTTPFinished()
	metrics.ObserveHTTPFinished()
	metrics.ObserveHTTPFinished()
	body, ok := metrics.Snapshot()
	if !ok {
		t.Fatal("snapshot unavailable")
	}
	text := string(body)
	for _, want := range []string{
		"gopulse_backend_http_requests_in_flight 0",
		"gopulse_backend_http_concurrency_limit 2",
		"gopulse_backend_http_rejected_total 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing saturation signal %q", want)
		}
	}
}
