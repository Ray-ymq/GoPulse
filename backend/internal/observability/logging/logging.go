package logging

import (
	"context"
	"io"
	"log/slog"

	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
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

// WithTrace adds only the bounded W3C identifiers from the active span. It
// never copies baggage or request values into a log record.
func WithTrace(logger *slog.Logger, ctx context.Context) *slog.Logger {
	if logger == nil {
		return logger
	}
	traceID, spanID, ok := tracing.TraceFields(ctx)
	if !ok {
		return logger
	}
	return logger.With(slog.String("trace_id", traceID), slog.String("span_id", spanID))
}

// Discard returns a schema-compatible logger for tests or optional wiring.
func Discard(service string) *slog.Logger {
	return New(service, io.Discard)
}
