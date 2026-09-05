package metricquery

import (
	"context"
	"net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Query(context.Context, Options) (Result, error)
}
type Handler struct{ application Application }

func NewHandler(application Application) *Handler { return &Handler{application: application} }
func (h *Handler) List(c *gin.Context) {
	options, err := ParseOptions(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}
	result, err := h.application.Query(c.Request.Context(), options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, http.StatusOK, result)
}
