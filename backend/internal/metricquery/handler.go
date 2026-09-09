package metricquery

import (
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"io"
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

// Catalog is separate from the historical query DTO and exposes no host,
// credential, process, arbitrary query expression or producer version label.
func (h *Handler) Catalog(c *gin.Context) {
	extra, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if c.Request.URL.RawQuery != "" || err != nil || len(extra) != 0 {
		response.Error(c, apperror.New(apperror.CodeValidationFailed, "query parameters are invalid"))
		return
	}
	response.Data(c, http.StatusOK, Catalog)
}
