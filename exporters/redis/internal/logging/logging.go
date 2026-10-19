package logging

import (
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"io"
	"log/slog"
)

const SchemaVersion = 1

func New(service string, w io.Writer) *slog.Logger { return runtimekit.NewLogger(service, w) }
func Module(logger *slog.Logger, module string) *slog.Logger {
	return runtimekit.ModuleLogger(logger, module)
}
func Discard(service string) *slog.Logger { return New(service, io.Discard) }
