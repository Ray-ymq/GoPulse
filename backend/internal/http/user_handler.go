package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	profiles *user.ProfileService
	posts    *post.Service
}

func NewUserHandler(profiles *user.ProfileService, posts *post.Service) *UserHandler {
	return &UserHandler{profiles, posts}
}
func viewer(c *gin.Context) (uint64, bool) {
	id, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
	}
	return id, ok
}
func (h *UserHandler) Get(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	p, err := h.profiles.Get(c.Request.Context(), c.Param("username"), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, 200, p)
}
func (h *UserHandler) Update(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	var input user.ProfileInput
	if err := request.DecodeJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	p, err := h.profiles.Update(c.Request.Context(), id, input)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, 200, p)
}
func (h *UserHandler) Search(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	options, err := h.profiles.ParseSearch(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}
	records, next, err := h.profiles.Search(c.Request.Context(), id, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, 200, records, next)
}
func (h *UserHandler) Posts(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	options, err := post.ParseListOptions(c.Request.URL.Query())
	if err != nil {
		response.Error(c, err)
		return
	}
	p, err := h.profiles.Get(c.Request.Context(), c.Param("username"), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	options.AuthorID = p.ID
	page, err := h.posts.List(c.Request.Context(), id, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, 200, page.Posts, page.NextCursor)
}
