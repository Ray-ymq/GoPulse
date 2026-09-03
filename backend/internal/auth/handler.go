package auth

import (
	"context"
	"log/slog"
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type Application interface {
	Register(context.Context, Credentials) (user.Public, string, error)
	Login(context.Context, Credentials) (user.Public, string, error)
	CurrentUser(context.Context, uint64) (user.Public, error)
}

type Handler struct {
	application Application
	cookies     *CookieManager
	logger      *slog.Logger
}

func NewHandler(application Application, cookies *CookieManager, loggers ...*slog.Logger) *Handler {
	logger := logging.Discard("backend")
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Handler{application: application, cookies: cookies, logger: logging.Module(logger, "auth")}
}

func (handler *Handler) Register(c *gin.Context) {
	var credentials Credentials
	if err := request.DecodeJSON(c, &credentials); err != nil {
		response.Error(c, err)
		return
	}

	publicUser, token, err := handler.application.Register(c.Request.Context(), credentials)
	if err != nil {
		response.Error(c, err)
		return
	}
	handler.cookies.Set(c.Writer, token)
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "auth").Info("user registered", slog.Uint64("user_id", publicUser.ID))
	response.Data(c, stdhttp.StatusCreated, publicUser)
}

func (handler *Handler) Login(c *gin.Context) {
	var credentials Credentials
	if err := request.DecodeJSON(c, &credentials); err != nil {
		response.Error(c, err)
		return
	}

	publicUser, token, err := handler.application.Login(c.Request.Context(), credentials)
	if err != nil {
		response.Error(c, err)
		return
	}
	handler.cookies.Set(c.Writer, token)
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "auth").Info("user logged in", slog.Uint64("user_id", publicUser.ID))
	response.Data(c, stdhttp.StatusOK, publicUser)
}

func (handler *Handler) Logout(c *gin.Context) {
	handler.cookies.Clear(c.Writer)
	logging.Module(logging.FromContext(c.Request.Context(), handler.logger), "auth").Info("user logged out")
	c.Status(stdhttp.StatusNoContent)
}

func (handler *Handler) CurrentUser(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
		return
	}

	publicUser, err := handler.application.CurrentUser(c.Request.Context(), userID)
	if err != nil {
		if appError, ok := apperror.As(err); ok && appError.Code == apperror.CodeAuthenticationRequired {
			handler.cookies.Clear(c.Writer)
		}
		response.Error(c, err)
		return
	}
	response.Data(c, stdhttp.StatusOK, publicUser)
}
