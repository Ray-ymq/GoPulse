package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/params"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	profiles *user.ProfileService
	posts    *post.Service
	follows  *user.MySQLRepository
}

func NewUserHandler(profiles *user.ProfileService, posts *post.Service, follows ...*user.MySQLRepository) *UserHandler {
	h := &UserHandler{profiles: profiles, posts: posts}
	if len(follows) > 0 {
		h.follows = follows[0]
	}
	return h
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

func (h *UserHandler) Follow(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	target, err := params.PositiveID(c, "username")
	if err != nil {
		response.Error(c, err)
		return
	}
	following := c.Request.Method == "PUT"
	if err = h.follows.SetFollow(c.Request.Context(), id, target, following); err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, 200, gin.H{"following": following})
}
func relationOptions(c *gin.Context) (post.ListOptions, error) {
	for key := range c.Request.URL.Query() {
		if key != "limit" && key != "cursor" {
			return post.ListOptions{}, apperror.New(apperror.CodeValidationFailed, "invalid list parameters")
		}
	}
	return post.ParseListOptions(c.Request.URL.Query())
}
func (h *UserHandler) Relations(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	options, err := relationOptions(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	relation := user.RelationOptions{Limit: options.Limit}
	if options.Cursor != nil {
		relation.Cursor = &user.RelationCursor{CreatedAt: options.Cursor.CreatedAt, ID: options.Cursor.ID}
	}
	records, next, err := h.follows.Relations(c.Request.Context(), id, c.FullPath() == "/api/v1/users/me/followers", relation)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, 200, records, next)
}
func (h *UserHandler) Following(c *gin.Context) {
	id, ok := viewer(c)
	if !ok {
		return
	}
	options, err := relationOptions(c)
	if err != nil {
		response.Error(c, err)
		return
	}
	options.Following = true
	page, err := h.posts.List(c.Request.Context(), id, options)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Page(c, 200, page.Posts, page.NextCursor)
}
