package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestAlertsAuthorizeBeforeDataAccess(t *testing.T) {
	for _, status := range []int{401, 403} {
		deny := func(c *gin.Context) {
			code := apperror.CodePermissionDenied
			if status == 401 {
				code = apperror.CodeAuthenticationRequired
			}
			response.Error(c, apperror.New(code, "access denied"))
			c.Abort()
		}
		pass := func(c *gin.Context) { c.Next() }
		auth := pass
		authorization := deny
		if status == 401 {
			auth = deny
			authorization = pass
		}
		router := NewRouter(Dependencies{}, APIRoutes{Authentication: auth, Authorization: authorization, Alerts: alert.NewHandler(nil, "key")})
		for _, v := range []struct{ m, p string }{{"GET", "catalog"}, {"GET", "rules"}, {"POST", "rules"}, {"GET", "rules/1"}, {"PUT", "rules/1"}, {"DELETE", "rules/1"}, {"POST", "rules/1/enable"}, {"POST", "rules/1/disable"}, {"GET", "current"}, {"GET", "history"}} {
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(v.m, "/api/v1/alerts/"+v.p, nil))
			if res.Code != status {
				t.Fatalf("%s %s: %d", v.m, v.p, res.Code)
			}
		}
	}
}
