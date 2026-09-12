package exporterplugin

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type auditStub struct {
	requested, completed int
	fail                 bool
	outcome              string
}

func (a *auditStub) RequestPluginAudit(context.Context, uint64, string, string, string) (string, error) {
	a.requested++
	return "operation", nil
}
func (a *auditStub) CompletePluginAudit(_ context.Context, _, _, _, outcome string) error {
	if a.fail {
		return errors.New("unavailable")
	}
	a.completed++
	a.outcome = outcome
	return nil
}
func TestPluginIntentAndCompletion(t *testing.T) {
	for _, fail := range []bool{false, true} {
		a := &auditStub{fail: fail}
		h := NewHandler(nil).WithAudit(a)
		g := gin.New()
		g.POST("/plugins/:pluginId/start", h.Audit, func(c *gin.Context) { c.Status(200) })
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest("POST", "/plugins/redis-exporter/start", nil))
		if w.Code != 200 || a.requested != 1 {
			t.Fatal("intent missing")
		}
		if fail && a.completed != 0 || !fail && (a.completed != 1 || a.outcome != "succeeded") {
			t.Fatal("false completion")
		}
	}
}
