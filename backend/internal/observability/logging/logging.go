package logging

import (
	"context"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"log/slog"
)

const SchemaVersion = 1

func New(service string, w io.Writer) *slog.Logger { return componentmetrics.NewLogger(service, w) }
func Module(logger *slog.Logger, module string) *slog.Logger {
	return componentmetrics.ModuleLogger(logger, module)
}

type contextKey struct{}

// WithContext stores a request-scoped logger without changing other context values.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the request logger or the explicitly supplied fallback.
func FromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	return fallback
}

// Discard returns a schema-compatible logger for tests or optional wiring.
func Discard(service string) *slog.Logger {
	return New(service, io.Discard)
}
