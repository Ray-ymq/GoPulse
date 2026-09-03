package comment

import (
	"context"
	"log/slog"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/params"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Create(context.Context, uint64, uint64, CreateInput) (Comment, error)
	List(context.Context, uint64, ListOptions) (Page, error)
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
	return &Handler{application: application, logger: logging.Module(logger, "comment")}
}

func (handler *Handler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	postID, err := params.PositiveID(c, "postId")
	if err != nil {
		response.Error(c, err)
		return
	}
	var input CreateInput
	if err := request.DecodeJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}

	record, err := handler.application.Create(c.Request.Context(), postID, userID, input)
	if err != nil {
		response.Error(c, err)
		return
	}
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "comment").Info("comment created", slog.Uint64("user_id", userID), slog.Uint64("post_id", postID), slog.Uint64("comment_id", record.ID))
	response.Data(c, stdhttp.StatusCreated, record)
}

func (handler *Handler) List(c *gin.Context) {
	if _, ok := currentUserID(c); !ok {
		return
	}
	postID, err := params.PositiveID(c, "postId")
	if err != nil {
		response.Error(c, err)
		return
	}
	options, err := ParseListOptions(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}

	page, err := handler.application.List(c.Request.Context(), postID, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, stdhttp.StatusOK, page.Comments, page.NextCursor)
}

func currentUserID(c *gin.Context) (uint64, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		return 0, false
	}
	return userID, true
}
