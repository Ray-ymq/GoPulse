package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"github.com/gin-gonic/gin"
)

type countingMetricApplication struct{ calls int }

func (a *countingMetricApplication) Query(context.Context, metricquery.Options) (metricquery.Result, error) {
	a.calls++
	return metricquery.Result{Series: []metricquery.Series{}}, nil
}

func TestMetricRoutesAuthorizeBeforeVictoriaMetrics(t *testing.T) {
	application := &countingMetricApplication{}
	handler := metricquery.NewHandler(application)
	request := func(router http.Handler) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/observability/metrics?metric=gopulse_redis_up", nil))
		return recorder
	}
	unauthenticated := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		c.Abort()
	}, Authorization: func(c *gin.Context) { c.Next() }, Metrics: handler})
	if recorder := request(unauthenticated); recorder.Code != http.StatusUnauthorized || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", recorder.Code, application.calls)
	}
	ordinary := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }, Authorization: func(c *gin.Context) {
		response.Error(c, apperror.New(apperror.CodePermissionDenied, "administrator permission is required"))
		c.Abort()
	}, Metrics: handler})
	if recorder := request(ordinary); recorder.Code != http.StatusForbidden || application.calls != 0 {
		t.Fatalf("status=%d calls=%d", recorder.Code, application.calls)
	}
	admin := NewRouter(Dependencies{}, APIRoutes{Authentication: func(c *gin.Context) { c.Next() }, Authorization: func(c *gin.Context) { c.Next() }, Metrics: handler})
	if recorder := request(admin); recorder.Code != http.StatusOK || application.calls != 1 {
		t.Fatalf("status=%d calls=%d", recorder.Code, application.calls)
	}
}
