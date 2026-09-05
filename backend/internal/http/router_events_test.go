package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type countingEventApplication struct{ calls int }

func (a *countingEventApplication) Query(context.Context, eventquery.Options) (eventquery.Page, error) {
	a.calls++
	return eventquery.Page{Entries: []eventquery.Entry{}}, nil
}

func TestEventRoutesAuthorizeBeforeRepository(t *testing.T) {
	application := &countingEventApplication{}
	handler := eventquery.NewHandler(application)
	unauthenticated := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		c.Abort()
	}, Authorization: func(c *gin.Context) { c.Next() }, Events: handler})
	recorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/observability/events", nil))
	if recorder.Code != http.StatusUnauthorized || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", recorder.Code, application.calls)
	}
	ordinary := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }, Authorization: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodePermissionDenied, "administrator permission is required"))
		c.Abort()
	}, Events: handler})
	recorder = httptest.NewRecorder()
	ordinary.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/observability/events", nil))
	if recorder.Code != http.StatusForbidden || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", recorder.Code, application.calls)
	}
	admin := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }, Authorization: func(c *gin.Context) { c.Next() }, Events: handler})
	recorder = httptest.NewRecorder()
	admin.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/observability/events", nil))
	if recorder.Code != http.StatusOK || application.calls != 1 {
		t.Fatalf("status=%d calls=%d", recorder.Code, application.calls)
	}
}
