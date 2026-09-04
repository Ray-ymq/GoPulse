package response

import (
	stdhttp "net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/gin-gonic/gin"
)

type dataEnvelope struct {
	Data any `json:"data"`
}

type pageEnvelope struct {
	Data any      `json:"data"`
	Meta pageMeta `json:"meta"`
}

type pageMeta struct {
	NextCursor *string `json:"next_cursor"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

const errorCodeContextKey = "gopulse.error_code"

type errorBody struct {
	Code    apperror.Code `json:"code"`
	Message string        `json:"message"`
}

// Data writes the common successful response envelope.
func Data(c *gin.Context, status int, data any) {
	c.JSON(status, dataEnvelope{Data: data})
}

// Page writes a successful response with cursor pagination metadata.
func Page(c *gin.Context, status int, data any, nextCursor *string) {
	c.JSON(status, pageEnvelope{Data: data, Meta: pageMeta{NextCursor: nextCursor}})
}

// Error maps application errors to stable HTTP status codes and payloads.
func Error(c *gin.Context, err error) {
	status, code, message := mapError(err)
	c.Set(errorCodeContextKey, code)
	c.JSON(status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

// ErrorCode returns the final public error code recorded for the response.
func ErrorCode(c *gin.Context) (apperror.Code, bool) {
	value, ok := c.Get(errorCodeContextKey)
	if !ok {
		return "", false
	}
	code, ok := value.(apperror.Code)
	return code, ok && code != ""
}

func mapError(err error) (int, apperror.Code, string) {
	appError, ok := apperror.As(err)
	if !ok {
		return stdhttp.StatusInternalServerError, apperror.CodeInternal, "an internal error occurred"
	}

	switch appError.Code {
	case apperror.CodeValidationFailed, apperror.CodePluginPackageInvalid:
		return stdhttp.StatusBadRequest, appError.Code, appError.Message
	case apperror.CodeAuthenticationRequired, apperror.CodeInvalidCredentials:
		return stdhttp.StatusUnauthorized, appError.Code, appError.Message
	case apperror.CodePermissionDenied:
		return stdhttp.StatusForbidden, appError.Code, appError.Message
	case apperror.CodeUsernameConflict, apperror.CodePluginConflict, apperror.CodePluginOperationInProgress:
		return stdhttp.StatusConflict, appError.Code, appError.Message
	case apperror.CodePostNotFound, apperror.CodeNotificationNotFound, apperror.CodePluginNotFound:
		return stdhttp.StatusNotFound, appError.Code, appError.Message
	case apperror.CodePluginOperationFailed:
		return stdhttp.StatusUnprocessableEntity, appError.Code, appError.Message
	case apperror.CodeSearchUnavailable, apperror.CodeMonitorUnavailable, apperror.CodeLogsUnavailable:
		return stdhttp.StatusServiceUnavailable, appError.Code, appError.Message
	default:
		return stdhttp.StatusInternalServerError, apperror.CodeInternal, "an internal error occurred"
	}
}
