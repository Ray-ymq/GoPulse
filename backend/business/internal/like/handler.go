package like

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
	Like(context.Context, uint64, uint64) error
	Unlike(context.Context, uint64, uint64) error
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
	return &Handler{application: application, logger: logging.Module(logger, "like")}
}

func (handler *Handler) Like(c *gin.Context) {
	handler.apply(c, handler.application.Like, "post liked")
}

func (handler *Handler) Unlike(c *gin.Context) {
	handler.apply(c, handler.application.Unlike, "post unliked")
}

func (handler *Handler) apply(c *gin.Context, operation func(context.Context, uint64, uint64) error, message string) {
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
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "like").Info(message, slog.Uint64("user_id", userID), slog.Uint64("post_id", postID))
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
