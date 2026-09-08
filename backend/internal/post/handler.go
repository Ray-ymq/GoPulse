package post

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
	Create(context.Context, uint64, CreateInput) (Post, error)
	List(context.Context, uint64, ListOptions) (Page, error)
	Detail(context.Context, uint64, uint64) (Post, error)
}

type Handler struct {
	bookmarkKey [32]byte
	application Application
	logger      *slog.Logger
}

func NewHandler(application Application, loggers ...*slog.Logger) *Handler {
	logger := logging.Discard("backend")
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Handler{bookmarkKey: newBookmarkKey(), application: application, logger: logging.Module(logger, "post")}
}

func (handler *Handler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var input CreateInput
	if err := request.DecodeJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}

	record, err := handler.application.Create(c.Request.Context(), userID, input)
	if err != nil {
		response.Error(c, err)
		return
	}
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "post").Info("post created", slog.Uint64("user_id", userID), slog.Uint64("post_id", record.ID))
	response.Data(c, stdhttp.StatusCreated, record)
}

func (handler *Handler) Bookmarks(c *gin.Context) { handler.list(c, true) }
func (handler *Handler) List(c *gin.Context)      { handler.list(c, false) }
func (handler *Handler) list(c *gin.Context, bookmarks bool) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	values := c.Request.URL.Query()
	if bookmarks {
		for key := range values {
			if key != "limit" && key != "cursor" {
				response.Error(c, validationError("unknown bookmark query parameter"))
				return
			}
		}
	}
	if bookmarks && values.Has("cursor") {
		tokens := values["cursor"]
		if len(tokens) != 1 {
			response.Error(c, validationError("cursor is invalid"))
			return
		}
		token, err := handler.verifyBookmarkCursor(userID, tokens[0])
		if err != nil {
			response.Error(c, err)
			return
		}
		values.Set("cursor", token)
	}
	options, err := ParseListOptions(values)
	if err != nil {
		response.Error(c, err)
		return
	}

	options.Bookmarks = bookmarks
	page, err := handler.application.List(c.Request.Context(), userID, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	if bookmarks && page.NextCursor != nil {
		token := handler.signBookmarkCursor(userID, *page.NextCursor)
		page.NextCursor = &token
	}
	response.Page(c, stdhttp.StatusOK, page.Posts, page.NextCursor)
}

func (handler *Handler) Detail(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	postID, err := params.PositiveID(c, "postId")
	if err != nil {
		response.Error(c, err)
		return
	}

	record, err := handler.application.Detail(c.Request.Context(), postID, userID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, stdhttp.StatusOK, record)
}

func currentUserID(c *gin.Context) (uint64, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		return 0, false
	}
	return userID, true
}

func (handler *Handler) Update(c *gin.Context) {
	actor, ok := currentUserID(c)
	if !ok {
		return
	}
	id, err := params.PositiveID(c, "postId")
	if err != nil {
		response.Error(c, err)
		return
	}
	var input CreateInput
	if err := request.DecodeJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	app, ok := handler.application.(interface {
		Update(context.Context, uint64, uint64, CreateInput) (Post, error)
	})
	if !ok {
		response.Error(c, apperror.New(apperror.CodePermissionDenied, "editing unavailable"))
		return
	}
	record, err := app.Update(c.Request.Context(), id, actor, input)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, stdhttp.StatusOK, record)
}

func (handler *Handler) Delete(c *gin.Context) {
	actor, ok := currentUserID(c)
	if !ok {
		return
	}
	id, err := params.PositiveID(c, "postId")
	if err != nil {
		response.Error(c, err)
		return
	}
	app, ok := handler.application.(interface {
		Delete(context.Context, uint64, uint64) error
	})
	if !ok {
		response.Error(c, apperror.New(apperror.CodePermissionDenied, "deletion unavailable"))
		return
	}
	if err := app.Delete(c.Request.Context(), id, actor); err != nil {
		response.Error(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}
