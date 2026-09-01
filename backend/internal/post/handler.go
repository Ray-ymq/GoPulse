package post

import (
	"context"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/params"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Create(context.Context, uint64, CreateInput) (Post, error)
	List(context.Context, uint64, ListOptions) (Page, error)
	Detail(context.Context, uint64, uint64) (Post, error)
}

type Handler struct {
	application Application
}

func NewHandler(application Application) *Handler {
	return &Handler{application: application}
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
	response.Data(c, stdhttp.StatusCreated, record)
}

func (handler *Handler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	options, err := ParseListOptions(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}

	page, err := handler.application.List(c.Request.Context(), userID, options)
	if err != nil {
		response.Error(c, err)
		return
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
