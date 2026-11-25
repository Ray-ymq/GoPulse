package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRouterRetainsRequestIDAndRecovery(t *testing.T) {
	router := NewRouter(Dependencies{})
	router.GET("/panic", func(*gin.Context) { panic("test") })
	for _, path := range []string{"/missing", "/live", "/panic", "/metrics", "/internal/v1/metrics"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if path == "/panic" && response.Code != 500 {
			t.Fatalf("panic status=%d", response.Code)
		}
		if (path == "/metrics" || path == "/internal/v1/metrics") && response.Code != 404 {
			t.Fatalf("%s status=%d", path, response.Code)
		}
		if response.Header().Get("X-Request-ID") == "" {
			t.Fatalf("%s lost request ID", path)
		}
	}
}
