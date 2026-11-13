package adminhttp

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/modules/user"
	"github.com/Ray-ymq/GoPulse/backend/internal/shared/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/transport/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/transport/http/params"
	"github.com/Ray-ymq/GoPulse/backend/internal/transport/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

type Users interface {
	FindByID(context.Context, uint64) (user.User, error)
	FindByUsername(context.Context, string) (user.User, error)
	BootstrapID(context.Context) (uint64, error)
	ManagedByID(context.Context, uint64) (user.Managed, error)
	ChangeRole(context.Context, uint64, uint64, user.Role, string) (user.RoleChange, error)
}

type Handler struct{ users Users }

func NewHandler(users Users) *Handler { return &Handler{users: users} }

func (h *Handler) Authorize() gin.HandlerFunc { return middleware.RequireSuperAdmin(h.users) }

func (h *Handler) Get(c *gin.Context) {
	username, err := user.NormalizeUsername(c.Param("username"))
	if err != nil {
		response.Error(c, apperror.New(apperror.CodeValidationFailed, "用户名格式不正确"))
		return
	}
	record, err := h.users.FindByUsername(c.Request.Context(), username)
	if err != nil {
		writeError(c, err)
		return
	}
	managed, err := h.users.ManagedByID(c.Request.Context(), record.ID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Data(c, 200, managed)
}

func (h *Handler) ChangeRole(c *gin.Context) {
	actor, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "请先登录"))
		return
	}
	target, err := params.PositiveID(c, "userId")
	if err != nil {
		writeError(c, err)
		return
	}
	var input struct {
		Role user.Role `json:"role"`
	}
	if err := request.DecodeJSON(c, &input); err != nil {
		writeError(c, err)
		return
	}
	if _, err := user.ParseRole(string(input.Role)); err != nil {
		response.Error(c, apperror.New(apperror.CodeValidationFailed, "角色必须为 user 或 super_admin"))
		return
	}
	result, err := h.users.ChangeRole(c.Request.Context(), actor, target, input.Role, c.Writer.Header().Get("X-Request-ID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Data(c, 200, result)
}

func writeError(c *gin.Context, err error) {
	if errors.Is(err, user.ErrNotFound) {
		err = apperror.New(apperror.CodeUserNotFound, "用户不存在")
	}
	response.Error(c, err)
}
