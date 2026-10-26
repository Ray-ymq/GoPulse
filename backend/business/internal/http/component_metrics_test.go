package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActualRequestMetricsDoNotExposePublicEndpoint(t *testing.T) {
	metrics, err := instrumentation.NewBackend(instrumentation.BackendRoutes())
	if err != nil {
		t.Fatal(err)
	}
	instrumentation.InstallBackend(metrics)
	defer instrumentation.InstallBackend(nil)
	metrics.ObserveOutbox(0, 0, nil)
	router := NewRouter(Dependencies{})
	for path, status := range map[string]int{"/health": 200, "/internal/v1/metrics": 404, "/private-user-id?secret=1": 404} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != status {
			t.Fatalf("%s status %d", path, response.Code)
		}
	}
	body, _ := metrics.Snapshot()
	text := string(body)
	if !strings.Contains(text, `gopulse_backend_http_requests_total{method="GET",route="/health",status_class="2xx"} 1`) {
		t.Fatal("actual completion not recorded")
	}
	if !strings.Contains(text, `route="_unmatched",status_class="4xx"} 2`) {
		t.Fatal("unmatched requests not bounded")
	}
	if strings.Contains(text, "private-user") || strings.Contains(text, "secret=") {
		t.Fatal("request data leaked")
	}
}
