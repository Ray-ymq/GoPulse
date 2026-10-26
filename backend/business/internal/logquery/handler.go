package logquery

import (
	"context"
	"net/http"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Query(context.Context, Options) (Page, error)
}
type Handler struct {
	application Application
	now         func() time.Time
}

func NewHandler(application Application) *Handler {
	return &Handler{application: application, now: time.Now}
}
func (h *Handler) List(c *gin.Context) {
	options, err := ParseOptions(c.Request.URL.Query(), h.now())
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := h.application.Query(c.Request.Context(), options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, http.StatusOK, page.Entries, page.NextCursor)
}
