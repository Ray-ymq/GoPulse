package logging

import (
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"log/slog"
)

const SchemaVersion = 1

func New(service string, w io.Writer) *slog.Logger { return componentmetrics.NewLogger(service, w) }
func Module(logger *slog.Logger, module string) *slog.Logger {
	return componentmetrics.ModuleLogger(logger, module)
}
func Discard(service string) *slog.Logger { return New(service, io.Discard) }
