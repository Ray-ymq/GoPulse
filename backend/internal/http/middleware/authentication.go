package middleware

import (
	"context"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type tokenVerifier interface {
	Verify(string) (uint64, error)
}

type currentUserIDKey struct{}

func RequireAuthentication(cookieName string, verifier tokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			writeAuthenticationRequired(c)
			return
		}

		userID, err := verifier.Verify(cookie.Value)
		if err != nil || userID == 0 {
			writeAuthenticationRequired(c)
			return
		}

		requestContext := context.WithValue(c.Request.Context(), currentUserIDKey{}, userID)
		c.Request = c.Request.WithContext(requestContext)
		c.Next()
	}
}

func CurrentUserID(c *gin.Context) (uint64, bool) {
	userID, ok := c.Request.Context().Value(currentUserIDKey{}).(uint64)
	return userID, ok && userID > 0
}

func writeAuthenticationRequired(c *gin.Context) {
	response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
	c.Abort()
}
