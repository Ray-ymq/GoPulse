package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/gin-gonic/gin"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeChecker struct {
	calls atomic.Int32
	check func(context.Context) error
}

func (c *fakeChecker) Check(ctx context.Context) error {
	c.calls.Add(1)
	if c.check != nil {
		return c.check(ctx)
	}
	return nil
}
func TestRuntimeProbesHardAndSoftDependencies(t *testing.T) {
	down := &fakeChecker{check: func(context.Context) error { return errors.New("unavailable") }}
	mysql := &fakeChecker{}
	router := NewRouter(Dependencies{MySQL: mysql, Redis: down, RabbitMQ: down, Elasticsearch: down})
	live := performRequest(router, "/live")
	health := performRequest(router, "/health")
	if live.Code != 200 || live.Body.String() != health.Body.String() || mysql.calls.Load() != 0 {
		t.Fatal("liveness contract")
	}
	ready := performRequest(router, "/ready")
	if ready.Code != 200 || down.calls.Load() != 0 {
		t.Fatal("soft dependencies blocked readiness")
	}
	hard := NewRouter(Dependencies{MySQL: down})
	if performRequest(hard, "/ready").Code != 503 {
		t.Fatal("hard failure ready")
	}
	missing := NewRouter(Dependencies{})
	if performRequest(missing, "/ready").Code != 503 {
		t.Fatal("missing dependency ready")
	}
}

func TestAPIBusinessAdmissionDoesNotConsumeProbeSlot(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	probes, err := componentmetrics.NewProbes(context.Background(), time.Second, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	probes.Started()
	metrics, err := componentmetrics.NewBackend([]componentmetrics.Route{{Method: "GET", Template: "/api/v1/test-admission"}})
	if err != nil {
		t.Fatal(err)
	}
	metrics.ObserveOutbox(0, 0, nil)
	metrics.SetHTTPConcurrencyLimit(1)
	componentmetrics.InstallBackend(metrics)
	t.Cleanup(func() { componentmetrics.InstallBackend(nil) })
	router := NewRouter(Dependencies{Probes: probes, HTTPMaxConcurrency: 1})
	router.GET("/api/v1/test-admission", func(c *gin.Context) {
		close(started)
		<-release
		c.Status(stdhttp.StatusNoContent)
	})
	firstDone := make(chan struct{})
	go func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/test-admission", nil))
		if response.Code != stdhttp.StatusNoContent {
			t.Errorf("first status = %d, want 204", response.Code)
		}
		close(firstDone)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("business request did not acquire admission slot")
	}

	busy := performRequest(router, "/api/v1/test-admission")
	if busy.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("saturated API status = %d, want 503", busy.Code)
	}
	assertJSONEqual(t, busy.Body.String(), `{"error":{"code":"backend_busy","message":"service temporarily busy"}}`)
	for _, path := range []string{"/startup", "/live", "/ready", "/health"} {
		probe := performRequest(router, path)
		if probe.Code != stdhttp.StatusOK {
			t.Fatalf("%s status while API is saturated = %d, want 200", path, probe.Code)
		}
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("business request did not finish")
	}
	body, ok := metrics.Snapshot()
	if !ok || !strings.Contains(string(body), "gopulse_backend_http_rejected_total 1\n") || !strings.Contains(string(body), "gopulse_backend_http_requests_in_flight 0\n") {
		t.Fatalf("admission metrics missing: %s", body)
	}
}
func performRequest(handler stdhttp.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(stdhttp.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertJSONEqual(t *testing.T, actual, expected string) {
	t.Helper()
	var actualValue any
	if err := json.Unmarshal([]byte(actual), &actualValue); err != nil {
		t.Fatalf("actual response is invalid JSON: %v", err)
	}
	var expectedValue any
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		t.Fatalf("test expectation is invalid JSON: %v", err)
	}
	if obj, ok := actualValue.(map[string]any); ok {
		if body, ok := obj["error"].(map[string]any); ok {
			if id, present := body["request_id"]; present {
				if !componentmetrics.ValidRequestID(id.(string)) {
					t.Fatal("invalid error request ID")
				}
				if expectedObj, ok := expectedValue.(map[string]any); ok {
					if expectedErr, ok := expectedObj["error"].(map[string]any); ok {
						if _, explicit := expectedErr["request_id"]; !explicit {
							delete(body, "request_id")
						}
					}
				}
			}
		}
	}
	actualJSON, _ := json.Marshal(actualValue)
	expectedJSON, _ := json.Marshal(expectedValue)
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("JSON = %s, want %s", actualJSON, expectedJSON)
	}
}

func TestDevelopmentRouterSuppressesGinFrameworkOutput(t *testing.T) {
	originalMode := gin.Mode()
	originalWriter := gin.DefaultWriter
	originalErrorWriter := gin.DefaultErrorWriter
	t.Cleanup(func() {
		gin.DefaultWriter = originalWriter
		gin.DefaultErrorWriter = originalErrorWriter
		gin.SetMode(originalMode)
	})

	var frameworkOutput bytes.Buffer
	gin.DefaultWriter = &frameworkOutput
	gin.DefaultErrorWriter = &frameworkOutput
	if err := ConfigureGinMode("development"); err != nil {
		t.Fatalf("ConfigureGinMode(development) error = %v", err)
	}
	NewRouter(Dependencies{})

	if frameworkOutput.Len() != 0 {
		t.Fatalf("Gin framework output = %q", frameworkOutput.String())
	}
}

func TestConfigureGinMode(t *testing.T) {
	original := gin.Mode()
	t.Cleanup(func() { gin.SetMode(original) })

	for _, test := range []struct {
		appEnv string
		want   string
	}{
		{appEnv: "development", want: gin.DebugMode},
		{appEnv: "test", want: gin.TestMode},
		{appEnv: "production", want: gin.ReleaseMode},
	} {
		if err := ConfigureGinMode(test.appEnv); err != nil {
			t.Fatalf("ConfigureGinMode(%q) error = %v", test.appEnv, err)
		}
		if gin.Mode() != test.want {
			t.Fatalf("Gin mode = %q, want %q", gin.Mode(), test.want)
		}
	}

	if err := ConfigureGinMode("staging"); err == nil {
		t.Fatal("ConfigureGinMode(staging) error = nil")
	}
}
