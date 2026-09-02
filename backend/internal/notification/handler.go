package notification

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
	List(context.Context, uint64, ListOptions) (Page, error)
	MarkRead(context.Context, uint64, uint64) error
}

type Handler struct {
	application Application
}

func NewHandler(application Application) *Handler {
	return &Handler{application: application}
}

func (handler *Handler) List(c *gin.Context) {
	recipientID, ok := notificationUserID(c)
	if !ok {
		return
	}
	options, err := ParseListOptions(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}
	page, err := handler.application.List(c.Request.Context(), recipientID, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, stdhttp.StatusOK, page.Notifications, page.NextCursor)
}

func (handler *Handler) MarkRead(c *gin.Context) {
	recipientID, ok := notificationUserID(c)
	if !ok {
		return
	}
	notificationID, err := params.PositiveID(c, "notificationId")
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := handler.application.MarkRead(c.Request.Context(), recipientID, notificationID); err != nil {
		response.Error(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func notificationUserID(c *gin.Context) (uint64, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		return 0, false
	}
	return userID, true
}
