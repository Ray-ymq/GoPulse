package apperror

import "errors"

// Code is a stable, client-visible application error identifier.
type Code string

const (
	CodeValidationFailed          Code = "validation_failed"
	CodeAuthenticationRequired    Code = "authentication_required"
	CodePermissionDenied          Code = "permission_denied"
	CodeInvalidCredentials        Code = "invalid_credentials"
	CodeUsernameConflict          Code = "username_conflict"
	CodePostNotFound              Code = "post_not_found"
	CodeNotificationNotFound      Code = "notification_not_found"
	CodeSearchUnavailable         Code = "search_unavailable"
	CodeLogsUnavailable           Code = "logs_unavailable"
	CodeEventsUnavailable         Code = "events_unavailable"
	CodePluginPackageInvalid      Code = "plugin_package_invalid"
	CodePluginNotFound            Code = "plugin_not_found"
	CodePluginConflict            Code = "plugin_conflict"
	CodePluginOperationInProgress Code = "plugin_operation_in_progress"
	CodePluginOperationFailed     Code = "plugin_operation_failed"
	CodeMonitorUnavailable        Code = "monitor_unavailable"
	CodeInternal                  Code = "internal_error"
)

// Error carries a stable code and a message that is safe to expose to clients.
type Error struct {
	Code    Code
	Message string
	cause   error
}

func (err *Error) Error() string {
	return err.Message
}

func (err *Error) Unwrap() error {
	return err.cause
}

// New creates an application error with a client-safe message.
func New(code Code, message string) error {
	return &Error{Code: code, Message: message}
}

// WrapInternal records an internal cause without exposing it to HTTP clients.
func WrapInternal(cause error) error {
	return &Error{Code: CodeInternal, Message: "an internal error occurred", cause: cause}
}

// As extracts a typed application error from an error chain.
func As(err error) (*Error, bool) {
	var appError *Error
	if !errors.As(err, &appError) {
		return nil, false
	}
	return appError, true
}
