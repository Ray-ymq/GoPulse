package exporterplugin

import (
	"context"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type Auditor interface {
	RequestPluginAudit(context.Context, uint64, string, string, string) (string, error)
	CompletePluginAudit(context.Context, string, string, string, string) error
}

func (h *Handler) WithAudit(a Auditor) *Handler { h.audit = a; return h }

// Commit intent before forwarding a mutation. Completion failure leaves the
// immutable requested/unknown record; never log request/configuration bodies.
func (h *Handler) Audit(c *gin.Context) {
	if h.audit == nil || c.Request.Method == "GET" {
		c.Next()
		return
	}
	id := c.Param("pluginId")
	if id == "" {
		id = pluginID
	}
	if _, ok := LookupOfficial(id); !ok {
		c.Next()
		return
	}
	path := strings.Split(c.FullPath(), "/")
	action := "plugin." + path[len(path)-1]
	actor, _ := middleware.CurrentUserID(c)
	operation, err := h.audit.RequestPluginAudit(c.Request.Context(), actor, action, id, c.Writer.Header().Get("X-Request-ID"))
	if err != nil {
		response.Error(c, apperror.New(apperror.CodeInternal, "management audit is unavailable"))
		c.Abort()
		return
	}
	c.Next()
	outcome := "succeeded"
	if c.Writer.Status() >= 400 {
		outcome = "failed"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), time.Second)
	defer cancel()
	// Do not claim completion if persistence fails: requested remains unknown.
	_ = h.audit.CompletePluginAudit(ctx, operation, action, id, outcome)
}
