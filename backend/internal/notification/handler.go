package notification

import (
	"context"
	"log/slog"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/params"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

type Application interface {
	List(context.Context, uint64, ListOptions) (Page, error)
	MarkRead(context.Context, uint64, uint64) error
}

type Handler struct {
	application Application
	logger      *slog.Logger
}

func NewHandler(application Application, loggers ...*slog.Logger) *Handler {
	logger := logging.Discard("backend")
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Handler{application: application, logger: logging.Module(logger, "notification")}
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
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "notification").Info("notification marked read", slog.Uint64("user_id", recipientID), slog.Uint64("notification_id", notificationID))
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
