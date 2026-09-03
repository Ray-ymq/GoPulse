package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

func TestExporterPluginRoutesAuthorizeBeforeCallingMonitor(t *testing.T) {
	calls := 0
	monitor := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer monitor.Close()
	client, err := exporterplugin.NewClient(monitor.URL, "01234567890123456789012345678901", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }, Authorization: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodePermissionDenied, "administrator permission is required"))
		c.Abort()
	}, ExporterPlugins: exporterplugin.NewHandler(client)})
	tests := []struct{ method, path string }{{http.MethodGet, "/api/v1/exporter-plugins"}, {http.MethodGet, "/api/v1/exporter-plugins/redis-exporter"}, {http.MethodPost, "/api/v1/exporter-plugins/install"}, {http.MethodPost, "/api/v1/exporter-plugins/redis-exporter/start"}, {http.MethodPost, "/api/v1/exporter-plugins/redis-exporter/stop"}, {http.MethodPost, "/api/v1/exporter-plugins/redis-exporter/update"}}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, bytes.NewReader(nil))
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		if result.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d", test.method, test.path, result.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("denied requests reached Monitor %d times", calls)
	}
}
