package runtimekit

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeLifecycle(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	var checks atomic.Int32
	p, err := NewProbes(root, time.Second, time.Millisecond, func(context.Context) error { checks.Add(1); return errors.New("unavailable") })
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		p.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%s %s: %d %v", method, path, w.Code, w.Header())
		}
		return w.Body.String()
	}
	request("GET", "/startup", 503)
	request("GET", "/ready", 503)
	if request("GET", "/live", 200) != request("GET", "/health", 200) {
		t.Fatal("health differs from live")
	}
	if checks.Load() != 0 {
		t.Fatal("liveness called dependency")
	}
	p.Started()
	request("GET", "/startup", 200)
	request("GET", "/ready", 503)
	for _, path := range []string{"/startup", "/live", "/ready", "/health"} {
		request("POST", path, 405)
		request("GET", path+"?", 400)
	}
	bodyResponse := httptest.NewRecorder()
	p.ServeHTTP(bodyResponse, httptest.NewRequest("GET", "/live", strings.NewReader("body")))
	if bodyResponse.Code != 400 {
		t.Fatal("probe accepted a request body")
	}
	request("GET", "/unknown", 404)
	cancel()
	request("GET", "/ready", 503)
	request("GET", "/live", 200)
}

func TestProbeSingleFlightAndDrain(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocked := make(chan struct{})
	var calls atomic.Int32
	p, _ := NewProbes(root, 5*time.Millisecond, time.Second, func(context.Context) error { calls.Add(1); <-blocked; return nil })
	p.Started()
	for range 10 {
		if p.readiness(context.Background()) {
			t.Fatal("blocked dependency ready")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("unbounded checks: %d", calls.Load())
	}
	p.mu.Lock()
	done := p.flight
	p.mu.Unlock()
	close(blocked)
	<-done
	if p.readiness(context.Background()) {
		t.Fatal("expired check accepted")
	}
	healthy, _ := NewProbes(root, time.Second, time.Second, nil)
	healthy.Started()
	if !healthy.readiness(context.Background()) {
		t.Fatal("local initialization not ready")
	}
	healthy.Stop()
	if healthy.readiness(context.Background()) {
		t.Fatal("draining process ready")
	}
}

func TestProbesDoNotExposeMetrics(t *testing.T) {
	p, _ := NewProbes(context.Background(), time.Second, time.Second, nil)
	for _, path := range []string{"/metrics", "/internal/v1/metrics"} {
		w := httptest.NewRecorder()
		p.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}
