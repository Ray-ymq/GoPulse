package search

import (
	"context"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Search(context.Context, uint64, Options) (Page, error)
}

type Handler struct{ application Application }

func NewHandler(application Application) *Handler { return &Handler{application: application} }

func (handler *Handler) Posts(c *gin.Context) {
	viewerID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		return
	}
	options, err := ParseOptions(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := handler.application.Search(c.Request.Context(), viewerID, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, stdhttp.StatusOK, page.Posts, page.NextCursor)
}
