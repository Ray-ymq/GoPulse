package componentmetrics

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion = 1
	moduleKey     = "__gopulse_module"
)

var reservedKeys = map[string]struct{}{
	"log_schema_version": {},
	"service":            {},
	"timestamp":          {},
	"time":               {},
	"level":              {},
	"module":             {},
	"message":            {},
	"msg":                {},
	"version":            {}, "revision": {}, "event": {},
}

type handler struct {
	delegate slog.Handler
	module   string
	limiter  *runtimeLogLimiter
}

// New constructs the shared single-line JSON logger used by GoPulse processes.
func NewLogger(service string, writer io.Writer) *slog.Logger {
	options := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
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
		},
	}
	delegate := slog.NewJSONHandler(writer, options).WithAttrs([]slog.Attr{
		slog.Int("log_schema_version", SchemaVersion),
		slog.String("runtime_contract_version", RuntimeContractVersion),
		slog.String("runtime_mode", safeRuntimeMode()),
		slog.String("service", service),
		slog.String("version", safeBuildValue("GOPULSE_VERSION", `^[0-9]+\.[0-9]+\.[0-9]+$`)),
		slog.String("revision", safeBuildValue("GOPULSE_REVISION", `^[0-9a-f]{40}$`)),
	})
	return slog.New(&handler{delegate: delegate, module: "lifecycle", limiter: &runtimeLogLimiter{down: map[string]time.Time{}}})
}

// Module returns a child logger with the fixed module field.
func ModuleLogger(logger *slog.Logger, module string) *slog.Logger {
	if logger == nil {
		return nil
	}
	return logger.With(slog.String(moduleKey, module))
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.delegate.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	event := runtimeEvent(record.Message)
	if h.limiter != nil && !h.limiter.allow(event, record.Message, record.Time) {
		return nil
	}
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	module := h.module
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == "module" && h.module == "lifecycle" {
			module = a.Value.String()
		}
		return true
	})
	clean.AddAttrs(slog.String("module", module), slog.String("event", runtimeEvent(record.Message)))
	record.Attrs(func(attribute slog.Attr) bool {
		if _, reserved := reservedKeys[attribute.Key]; !reserved {
			clean.AddAttrs(attribute)
		}
		return true
	})
	return h.delegate.Handle(ctx, clean)
}

func (h *handler) WithAttrs(attributes []slog.Attr) slog.Handler {
	clone := &handler{delegate: h.delegate, module: h.module, limiter: h.limiter}
	clean := make([]slog.Attr, 0, len(attributes))
	for _, attribute := range attributes {
		if attribute.Key == moduleKey || (attribute.Key == "module" && h.module == "lifecycle") {
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
	return &handler{delegate: h.delegate.WithGroup(name), module: h.module, limiter: h.limiter}
}

func safeBuildValue(key, pattern string) string {
	v := os.Getenv(key)
	if !regexp.MustCompile(pattern).MatchString(v) {
		return "development"
	}
	return v
}
func runtimeEvent(message string) string {
	m := strings.ToLower(message)
	switch {
	case strings.Contains(m, "http request"):
		return "http_request"
	case strings.Contains(m, "panic"):
		return "http_panic"
	case strings.Contains(m, "shutdown") || strings.Contains(m, "stopped"):
		return "shutdown"
	case strings.Contains(m, "started") || strings.Contains(m, "listening"):
		return "startup"
	case strings.Contains(m, "restored"):
		return "dependency_up"
	case strings.Contains(m, "unavailable") || strings.Contains(m, "failed"):
		return "dependency_down"
	default:
		return "operation"
	}
}

type runtimeLogLimiter struct {
	mu   sync.Mutex
	down map[string]time.Time
}

func (l *runtimeLogLimiter) allow(event, message string, now time.Time) bool {
	if event != "dependency_down" && event != "dependency_up" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if event == "dependency_up" {
		clear(l.down)
		return true
	}
	if previous, ok := l.down[message]; ok && now.Sub(previous) < 30*time.Second {
		return false
	}
	l.down[message] = now
	return true
}

func safeRuntimeMode() string {
	m := Mode()
	if m != "host" && m != "container" {
		return "invalid"
	}
	return m
}
