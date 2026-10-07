package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/adminoverview"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/gin-gonic/gin"
)

func TestServiceRoleRoutesAreMutuallyExclusive(t *testing.T) {
	business := NewRouter(Dependencies{}, APIRoutes{
		Auth:           auth.NewHandler(nil, nil),
		Authentication: func(c *gin.Context) { c.Next() },
	})
	platform := NewRouter(Dependencies{}, APIRoutes{
		Authentication:  func(c *gin.Context) { c.Next() },
		Authorization:   func(c *gin.Context) { c.Next() },
		Overview:        &adminoverview.Service{},
		Metrics:         metricquery.NewHandler(nil),
		Logs:            logquery.NewHandler(nil),
		Events:          eventquery.NewHandler(nil),
		Alerts:          alert.NewHandler(nil, "test-secret"),
		Management:      NewManagementHandler(nil, "test-secret"),
		ExporterPlugins: exporterplugin.NewHandler(nil),
	})

	assertRoute(t, business, "/api/v1/auth/login", true)
	assertRoute(t, business, "/api/v1/users/me", true)
	assertRoute(t, business, "/api/v1/posts", false)
	assertRoute(t, business, "/api/v1/observability/metrics", false)
	assertRoute(t, business, "/api/v1/admin/users/:userId", false)
	assertRoute(t, business, "/api/v1/exporter-plugins", false)

	assertRoute(t, platform, "/api/v1/auth/login", false)
	assertRoute(t, platform, "/api/v1/users/me", false)
	assertRoute(t, platform, "/api/v1/posts", false)
	assertRoute(t, platform, "/api/v1/observability/metrics", true)
	assertRoute(t, platform, "/api/v1/observability/logs", true)
	assertRoute(t, platform, "/api/v1/alerts/rules", true)
	assertRoute(t, platform, "/api/v1/admin/users/:userId", true)
	assertRoute(t, platform, "/api/v1/exporter-plugins", true)
}

func assertRoute(t *testing.T, router *gin.Engine, path string, want bool) {
	t.Helper()
	for _, route := range router.Routes() {
		if route.Path == path {
			if !want {
				t.Fatalf("route %s is registered unexpectedly", path)
			}
			return
		}
	}
	if want {
		t.Fatalf("route %s is not registered", path)
	}
}

func TestServiceRoleAdmissionIsolation(t *testing.T) {
	business, businessStarted, businessRelease, businessCancel := admissionTestRouter(t, 128)
	platform, platformStarted, platformRelease, platformCancel := admissionTestRouter(t, 32)
	defer businessCancel()
	defer platformCancel()

	businessRequests := holdAdmissionRequests(t, business, businessStarted, 128)
	if busy := performRequest(business, "/api/v1/test-admission"); busy.Code != http.StatusServiceUnavailable {
		t.Fatalf("business saturated status = %d, want 503", busy.Code)
	}
	for _, path := range []string{"/startup", "/live", "/ready", "/health"} {
		if response := performRequest(business, path); response.Code != http.StatusOK {
			t.Fatalf("business %s status = %d, want 200 while API is saturated", path, response.Code)
		}
	}
	platformProbeDone := make(chan struct{})
	go func() {
		response := performRequest(platform, "/api/v1/test-admission")
		if response.Code != http.StatusNoContent {
			t.Errorf("platform isolated request status = %d, want 204", response.Code)
		}
		close(platformProbeDone)
	}()
	waitAdmissionStarts(t, platformStarted, 1)
	platformRelease <- struct{}{}
	waitAdmissionDone(t, platformProbeDone, 1)

	releaseAdmissionRequests(businessRelease, 128)
	waitAdmissionDone(t, businessRequests, 128)

	platformRequests := holdAdmissionRequests(t, platform, platformStarted, 32)
	if busy := performRequest(platform, "/api/v1/test-admission"); busy.Code != http.StatusServiceUnavailable {
		t.Fatalf("platform saturated status = %d, want 503", busy.Code)
	}
	businessProbeDone := make(chan struct{})
	go func() {
		response := performRequest(business, "/api/v1/test-admission")
		if response.Code != http.StatusNoContent {
			t.Errorf("business isolated request status = %d, want 204", response.Code)
		}
		close(businessProbeDone)
	}()
	waitAdmissionStarts(t, businessStarted, 1)

	releaseAdmissionRequests(platformRelease, 32)
	waitAdmissionDone(t, platformRequests, 32)
	businessRelease <- struct{}{}
	waitAdmissionDone(t, businessProbeDone, 1)
}

func admissionTestRouter(t *testing.T, limit int) (*gin.Engine, chan struct{}, chan struct{}, context.CancelFunc) {
	t.Helper()
	root, cancel := context.WithCancel(context.Background())
	probes, err := componentmetrics.NewProbes(root, time.Second, time.Second, nil)
	if err != nil {
		t.Fatalf("NewProbes() error = %v", err)
	}
	probes.Started()
	started := make(chan struct{}, limit+1)
	release := make(chan struct{}, limit+1)
	router := NewRouter(Dependencies{Probes: probes, HTTPMaxConcurrency: limit})
	router.GET("/api/v1/test-admission", func(c *gin.Context) {
		started <- struct{}{}
		select {
		case <-release:
			c.Status(http.StatusNoContent)
		case <-root.Done():
			c.Status(http.StatusRequestTimeout)
		}
	})
	return router, started, release, cancel
}

func holdAdmissionRequests(t *testing.T, router *gin.Engine, started chan struct{}, count int) chan struct{} {
	t.Helper()
	done := make(chan struct{}, count)
	for index := 0; index < count; index++ {
		go func() {
			response := performRequest(router, "/api/v1/test-admission")
			if response.Code != http.StatusNoContent {
				t.Errorf("held request status = %d, want 204", response.Code)
			}
			done <- struct{}{}
		}()
	}
	waitAdmissionStarts(t, started, count)
	return done
}

func waitAdmissionStarts(t *testing.T, started chan struct{}, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatalf("admission request %d/%d did not enter handler", index+1, count)
		}
	}
}

func releaseAdmissionRequests(release chan struct{}, count int) {
	for index := 0; index < count; index++ {
		release <- struct{}{}
	}
}

func waitAdmissionDone(t *testing.T, done chan struct{}, count int) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for index := 0; index < count; index++ {
		select {
		case <-done:
		case <-deadline.C:
			t.Fatalf("admission request %d/%d did not finish", index+1, count)
		}
	}
}
