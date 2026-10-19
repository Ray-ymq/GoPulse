package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/selfmonitor"
	"github.com/gin-gonic/gin"
)

type monitorStub struct{ calls int }

func (s *monitorStub) Do(context.Context, string) (selfmonitor.Status, error) {
	s.calls++
	return selfmonitor.Status{State: "stopped", Reason: "stopped", GrafanaURL: "/grafana/", Targets: []selfmonitor.Target{}}, nil
}
func TestSelfMonitorRequiresBothAuthenticationAndAuthorization(t *testing.T) {
	for _, status := range []int{401, 403, 200} {
		c := &monitorStub{}
		r := gin.New()
		auth := func(ctx *gin.Context) {
			if status == 401 {
				ctx.AbortWithStatus(401)
			}
		}
		admin := func(ctx *gin.Context) {
			if status == 403 {
				ctx.AbortWithStatus(403)
			}
		}
		registerAPIV1Routes(r, APIRoutes{Authentication: auth, Authorization: admin, SelfMonitor: c})
		for _, path := range []string{"/api/v1/admin/self-monitor", "/api/v1/admin/self-monitor/start", "/api/v1/admin/self-monitor/stop"} {
			method := "POST"
			if strings.HasSuffix(path, "self-monitor") {
				method = "GET"
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != status {
				t.Fatal(path, w.Code)
			}
		}
		if status != 200 && c.calls != 0 {
			t.Fatal("unauthorized control call")
		}
		if status == 200 && c.calls != 3 {
			t.Fatal("missing control call")
		}
	}
}
func TestSelfMonitorRejectsBodyAndQuery(t *testing.T) {
	s := &monitorStub{}
	for _, path := range []string{"/start?command=stop", "/start?", "/start"} {
		r := gin.New()
		r.POST("/start", selfMonitorHandler(s, "start"))
		body := ""
		if path == "/start" {
			body = `{"path":"/tmp"}`
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if w.Code != 400 || s.calls != 0 {
			t.Fatal(w.Code, s.calls)
		}
	}
}
