package http

import (
	"context"
	"net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/selfmonitor"
	"github.com/gin-gonic/gin"
)

type SelfMonitorController interface {
	Do(context.Context, string) (selfmonitor.Status, error)
}

func selfMonitorHandler(client SelfMonitorController, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		r := c.Request
		if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "validation_failed", "message": "request must not contain parameters or a body"}})
			return
		}
		state, err := client.Do(r.Context(), action)
		if err != nil {
			state = selfmonitor.Status{State: "unavailable", Reason: "control_unavailable", GrafanaURL: "/grafana/", Targets: []selfmonitor.Target{}}
		}
		// An unavailable control plane is a displayable state, never a fabricated
		// stopped state; the UI can keep its retry control available.
		response.Data(c, http.StatusOK, state)
	}
}
