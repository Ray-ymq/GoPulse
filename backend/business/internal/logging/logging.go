package logging

import (
	"context"
	"io"
	"log/slog"
)

func New(service string, w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil)).With("service", service)
}
func Module(logger *slog.Logger, module string) *slog.Logger {
	if logger == nil {
		return nil
	}
	return logger.With("module", module)
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

// Discard 返回丢弃输出的日志实例，用于测试或未配置日志的调用。
func Discard(service string) *slog.Logger {
	return New(service, io.Discard)
}
