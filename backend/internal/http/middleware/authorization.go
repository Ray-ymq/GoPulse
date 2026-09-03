package middleware

import (
	"context"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type currentUserFinder interface {
	FindByID(context.Context, uint64) (user.User, error)
}

// RequireAdmin authorizes the current request from the role stored in MySQL.
// It must run after authentication; JWT claims never supply authorization data.
func RequireAdmin(users currentUserFinder) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := CurrentUserID(c)
		if !ok {
			writeAuthenticationRequired(c)
			return
		}

		record, err := users.FindByID(c.Request.Context(), userID)
		if errors.Is(err, user.ErrNotFound) {
			writeAuthenticationRequired(c)
			return
		}
		if err != nil {
			response.Error(c, apperror.WrapInternal(err))
			c.Abort()
			return
		}
		if record.Role != user.RoleAdmin {
			response.Error(c, apperror.New(apperror.CodePermissionDenied, "administrator permission is required"))
			c.Abort()
			return
		}
		c.Next()
	}
}
