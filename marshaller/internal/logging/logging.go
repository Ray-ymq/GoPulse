package logging

import (
	"io"
	"log/slog"
	"strings"
	"time"
)

const SchemaVersion = 1

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
	handler := slog.NewJSONHandler(writer, options).WithAttrs([]slog.Attr{slog.Int("log_schema_version", SchemaVersion), slog.String("service", service)})
	return slog.New(handler)
}
