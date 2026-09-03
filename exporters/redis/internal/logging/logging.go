package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"
)

const (
	SchemaVersion = 1
	moduleKey     = "__gopulse_module"
)

var reservedKeys = map[string]struct{}{
	"log_schema_version": {}, "service": {}, "timestamp": {}, "time": {},
	"level": {}, "module": {}, "message": {}, "msg": {},
}

type handler struct {
	delegate slog.Handler
	module   string
}

func New(service string, writer io.Writer) *slog.Logger {
	options := &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
		switch attribute.Key {
		case slog.TimeKey:
			attribute.Key = "timestamp"
			if timestamp, ok := attribute.Value.Any().(time.Time); ok {
				attribute.Value = slog.StringValue(timestamp.UTC().Format(time.RFC3339Nano))
			}
		case slog.LevelKey:
			attribute.Value = slog.StringValue(strings.ToLower(attribute.Value.Any().(slog.Level).String()))
		case slog.MessageKey:
			attribute.Key = "message"
		}
		return attribute
	}}
	delegate := slog.NewJSONHandler(writer, options).WithAttrs([]slog.Attr{slog.Int("log_schema_version", SchemaVersion), slog.String("service", service)})
	return slog.New(&handler{delegate: delegate})
}
func Module(logger *slog.Logger, module string) *slog.Logger {
	if logger == nil {
		return nil
	}
	return logger.With(slog.String(moduleKey, module))
}
func Discard(service string) *slog.Logger { return New(service, io.Discard) }
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.delegate.Enabled(ctx, level)
}
func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	if h.module != "" {
		clean.AddAttrs(slog.String("module", h.module))
	}
	record.Attrs(func(attribute slog.Attr) bool {
		if _, reserved := reservedKeys[attribute.Key]; !reserved {
			clean.AddAttrs(attribute)
		}
		return true
	})
	return h.delegate.Handle(ctx, clean)
}
func (h *handler) WithAttrs(attributes []slog.Attr) slog.Handler {
	clone := &handler{delegate: h.delegate, module: h.module}
	clean := make([]slog.Attr, 0, len(attributes))
	for _, attribute := range attributes {
		if attribute.Key == moduleKey {
			clone.module = attribute.Value.String()
			continue
		}
		if _, reserved := reservedKeys[attribute.Key]; !reserved {
			clean = append(clean, attribute)
		}
	}
	if len(clean) > 0 {
		clone.delegate = clone.delegate.WithAttrs(clean)
	}
	return clone
}
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{delegate: h.delegate.WithGroup(name), module: h.module}
}
