package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/gin-gonic/gin"
)

type countingLogApplication struct{ calls int }

func (a *countingLogApplication) Query(context.Context, logquery.Options) (logquery.Page, error) {
	a.calls++
	return logquery.Page{Entries: []logquery.Entry{}}, nil
}

func TestLogRoutesAuthorizeBeforeRepository(t *testing.T) {
	application := &countingLogApplication{}
	handler := logquery.NewHandler(application)
	unauthenticated := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		c.Abort()
	}, Authorization: func(c *gin.Context) { c.Next() }, Logs: handler})
	responseRecorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(responseRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/observability/logs", nil))
	if responseRecorder.Code != http.StatusUnauthorized || application.calls != 0 {
		t.Fatalf("unauthenticated status=%d calls=%d", responseRecorder.Code, application.calls)
	}
	ordinary := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }, Authorization: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodePermissionDenied, "administrator permission is required"))
		c.Abort()
	}, Logs: handler})
	responseRecorder = httptest.NewRecorder()
	ordinary.ServeHTTP(responseRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/observability/logs", nil))
	if responseRecorder.Code != http.StatusForbidden || application.calls != 0 {
		t.Fatalf("ordinary status=%d calls=%d", responseRecorder.Code, application.calls)
	}
}
