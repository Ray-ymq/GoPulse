package middleware

import (
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	stdhttp "net/http"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader             = "X-Request-ID"
	panicRecoveredContextKey    = "gopulse.panic_recovered"
	responseCommittedContextKey = "gopulse.response_committed"
)

type RequestIDGenerator func() (string, error)

func RandomRequestID() (string, error) { return componentmetrics.NewRequestID() }

func RequestID(logger *slog.Logger, generate RequestIDGenerator) gin.HandlerFunc {
	if generate == nil {
		generate = RandomRequestID
	}
	return func(c *gin.Context) {
		requestID := c.Request.Header.Get(requestIDHeader)
		var err error
		if len(c.Request.Header.Values(requestIDHeader)) != 1 || !componentmetrics.ValidRequestID(requestID) {
			requestID, err = generate()
		}
		if err != nil || requestID == "" {
			logger.Error("request id generation failed", slog.String("error_code", string(apperror.CodeInternal)))
			response.Error(c, apperror.New(apperror.CodeInternal, "an internal error occurred"))
			c.Abort()
			return
		}

		requestLogger := logger.With(slog.String("request_id", requestID))
		c.Request = c.Request.WithContext(logging.WithContext(componentmetrics.WithRequestID(c.Request.Context(), requestID), requestLogger))
		c.Header(requestIDHeader, requestID)
		c.Next()
	}
}

func Access(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.URL.Path {
		case "/startup", "/live", "/ready", "/health":
			c.Next()
			return
		}
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
		errorCode, hasErrorCode := response.ErrorCode(c)
		if hasErrorCode {
			attributes = append(attributes, slog.String("error_code", string(errorCode)))
		}
		panicRecovered := contextBool(c, panicRecoveredContextKey)
		if panicRecovered {
			if !hasErrorCode {
				attributes = append(attributes, slog.String("error_code", string(apperror.CodeInternal)))
			}
			attributes = append(attributes,
				slog.Bool("panic_recovered", true),
				slog.Bool("response_committed", contextBool(c, responseCommittedContextKey)),
			)
		}

		switch {
		case panicRecovered || status >= stdhttp.StatusInternalServerError:
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
				committed := c.Writer.Written()
				c.Set(panicRecoveredContextKey, true)
				c.Set(responseCommittedContextKey, committed)
				requestLogger := logging.FromContext(c.Request.Context(), logger)
				requestLogger.Error("http panic recovered",
					slog.String("error_code", string(apperror.CodeInternal)),
					slog.Bool("response_committed", committed),
				)
				if !committed {
					response.Error(c, errors.New("panic recovered"))
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

func contextBool(c *gin.Context, key string) bool {
	value, ok := c.Get(key)
	boolean, valid := value.(bool)
	return ok && valid && boolean
}
