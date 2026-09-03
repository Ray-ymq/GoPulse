package plugin

import "errors"

type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string            { return e.Message }
func (e *Error) Unwrap() error            { return e.Cause }
func NewError(code, message string) error { return &Error{Code: code, Message: message} }
func wrap(code, message string, cause error) error {
	return &Error{Code: code, Message: message, Cause: cause}
}
func AsError(err error) (*Error, bool) { var target *Error; return target, errors.As(err, &target) }

const (
	CodePackageInvalid = "plugin_package_invalid"
	CodeNotFound       = "plugin_not_found"
	CodeConflict       = "plugin_conflict"
	CodeInProgress     = "plugin_operation_in_progress"
	CodeFailed         = "plugin_operation_failed"
)
