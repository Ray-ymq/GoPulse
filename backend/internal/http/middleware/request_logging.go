package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	stdhttp "net/http"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

type RequestIDGenerator func() (string, error)

func RandomRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func RequestID(logger *slog.Logger, generate RequestIDGenerator) gin.HandlerFunc {
	if generate == nil {
		generate = RandomRequestID
	}
	return func(c *gin.Context) {
		requestID, err := generate()
		if err != nil || requestID == "" {
			logger.Error("request id generation failed", slog.String("error_code", string(apperror.CodeInternal)))
			response.Error(c, apperror.New(apperror.CodeInternal, "an internal error occurred"))
			c.Abort()
			return
		}

		requestLogger := logger.With(slog.String("request_id", requestID))
		c.Request = c.Request.WithContext(logging.WithContext(c.Request.Context(), requestLogger))
		c.Header(requestIDHeader, requestID)
		c.Next()
	}
}

func Access(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		requestLogger := logging.FromContext(c.Request.Context(), logger)
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		attributes := []any{
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			slog.Int("response_bytes", max(c.Writer.Size(), 0)),
		}
		if userID, ok := CurrentUserID(c); ok {
			attributes = append(attributes, slog.Uint64("user_id", userID))
		}
		if errorCode, ok := response.ErrorCode(c); ok {
			attributes = append(attributes, slog.String("error_code", string(errorCode)))
		}

		switch {
		case status >= stdhttp.StatusInternalServerError:
			requestLogger.Error("http request completed", attributes...)
		case status >= stdhttp.StatusBadRequest:
			requestLogger.Warn("http request completed", attributes...)
		default:
			requestLogger.Info("http request completed", attributes...)
		}
	}
}

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				requestLogger := logging.FromContext(c.Request.Context(), logger)
				requestLogger.Error("http panic recovered", slog.String("error_code", string(apperror.CodeInternal)))
				response.Error(c, errors.New("panic recovered"))
				c.Abort()
			}
		}()
		c.Next()
	}
}
