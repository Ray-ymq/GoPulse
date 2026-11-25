package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRemovedPlatformRoutesReturnNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }})
	for _, path := range []string{
		"/api/v1/admin/overview", "/api/v1/admin/self-monitor", "/api/v1/admin/component-status",
		"/api/v1/admin/telemetry-delivery", "/api/v1/observability/metrics", "/api/v1/observability/logs",
		"/api/v1/observability/events", "/api/v1/observability/business-stages", "/internal/v1/business-stages",
		"/api/v1/alerts/rules", "/api/v1/exporter-plugins", "/api/v1/exporter-plugins/mysql-exporter/install",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			t.Run(method+path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
				if response.Code != http.StatusNotFound {
					t.Fatalf("status = %d, want 404", response.Code)
				}
			})
		}
	}
}
