package like

import (
	"context"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/params"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Like(context.Context, uint64, uint64) error
	Unlike(context.Context, uint64, uint64) error
}

type Handler struct {
	application Application
}

func NewHandler(application Application) *Handler {
	return &Handler{application: application}
}

func (handler *Handler) Like(c *gin.Context) {
	handler.apply(c, handler.application.Like)
}

func (handler *Handler) Unlike(c *gin.Context) {
	handler.apply(c, handler.application.Unlike)
}

func (handler *Handler) apply(c *gin.Context, operation func(context.Context, uint64, uint64) error) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	postID, err := params.PositiveID(c, "postId")
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := operation(c.Request.Context(), postID, userID); err != nil {
		response.Error(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func currentUserID(c *gin.Context) (uint64, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		return 0, false
	}
	return userID, true
}
