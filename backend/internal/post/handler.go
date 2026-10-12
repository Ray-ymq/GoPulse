package post

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"strings"

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

type commandApplication interface {
	CreateWithCommand(context.Context, uint64, CreateInput, CommandRequest) (CreateResult, error)
	CommandContext(context.Context) (CommandContext, error)
	CommandResult(context.Context, uint64, CommandRequest) (CreateResult, error)
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

	command, keyed, err := commandHeaders(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if commandApplication, ok := handler.application.(commandApplication); ok {
		result, commandErr := commandApplication.CreateWithCommand(c.Request.Context(), userID, input, command)
		if commandErr != nil {
			response.Error(c, commandErr)
			return
		}
		commandLogger := logging.WithTrace(handler.logger, c.Request.Context())
		attributes := []any{slog.Uint64("post_id", result.Meta.PostID), slog.String("stage", "commit"), slog.String("result", "success")}
		if result.Meta.CommandID != "" {
			attributes = append(attributes, slog.String("command_id", result.Meta.CommandID))
		}
		if result.Meta.EventID != "" {
			attributes = append(attributes, slog.String("event_id", result.Meta.EventID))
		}
		if result.Meta.DataEpoch != "" {
			attributes = append(attributes, slog.String("data_epoch", result.Meta.DataEpoch))
		}
		if result.Post != nil {
			attributes = append(attributes, slog.Uint64("content_revision", result.Post.ContentRevision))
		}
		commandLogger.Info("post committed", attributes...)
		if !keyed && result.Meta.CommandID == "" && result.Post != nil {
			response.Data(c, stdhttp.StatusCreated, *result.Post)
			return
		}
		response.DataWithMeta(c, stdhttp.StatusCreated, result.Post, result.Meta)
		return
	}
	if keyed {
		response.Error(c, apperror.New(apperror.CodeCommandOutcomeUnknown, "post command activation is not ready"))
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

func (handler *Handler) CommandContext(c *gin.Context) {
	if _, ok := currentUserID(c); !ok {
		return
	}
	application, ok := handler.application.(commandApplication)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeCommandOutcomeUnknown, "post command activation is not ready"))
		return
	}
	result, err := application.CommandContext(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, stdhttp.StatusOK, result)
}

func (handler *Handler) CommandResult(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	command, keyed, err := commandHeaders(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	if !keyed {
		response.Error(c, validationError("Idempotency-Key and X-GoPulse-Data-Epoch are required"))
		return
	}
	application, ok := handler.application.(commandApplication)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeCommandOutcomeUnknown, "post command activation is not ready"))
		return
	}
	result, err := application.CommandResult(c.Request.Context(), userID, command)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.DataWithMeta(c, stdhttp.StatusOK, result.Post, result.Meta)
}

func commandHeaders(c *gin.Context) (CommandRequest, bool, error) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	epoch := strings.TrimSpace(c.GetHeader("X-GoPulse-Data-Epoch"))
	if key == "" && epoch == "" {
		return CommandRequest{}, false, nil
	}
	if key == "" || epoch == "" {
		return CommandRequest{}, false, validationError("Idempotency-Key and X-GoPulse-Data-Epoch must be supplied together")
	}
	return CommandRequest{DataEpoch: epoch, IdempotencyKey: key}, true, nil
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
