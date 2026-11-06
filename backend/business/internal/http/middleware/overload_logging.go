package middleware

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type overloadKey struct {
	route string
	code  apperror.Code
}
type overloadWindow struct {
	second           int64
	kept, suppressed int
}

// One limiter belongs to one router. Each window schedules its own flush so
// the final suppressed second is reported even when traffic stops entirely.
type overloadLogs struct {
	mu      sync.Mutex
	windows map[overloadKey]*overloadWindow
}

func (l *overloadLogs) allow(logger *slog.Logger, route string, code apperror.Code) bool {
	key := overloadKey{route, code}
	second := time.Now().Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windows == nil {
		l.windows = map[overloadKey]*overloadWindow{}
	}
	window := l.windows[key]
	if window == nil || window.second != second {
		window = &overloadWindow{second: second}
		l.windows[key] = window
		time.AfterFunc(time.Until(time.Unix(second+1, 0)), func() {
			l.mu.Lock()
			count := window.suppressed
			if l.windows[key] == window {
				delete(l.windows, key)
			}
			l.mu.Unlock()
			if count > 0 {
				logger.LogAttrs(context.Background(), slog.LevelWarn, "http overload logs suppressed", slog.String("route", route), slog.String("error_code", string(code)), slog.Int("suppressed", count), slog.Int64("window_unix", second))
			}
		})
	}
	if window.kept < 5 {
		window.kept++
		return true
	}
	window.suppressed++
	return false
}
func expectedOverload(code apperror.Code) bool {
	switch code {
	case apperror.CodeBackendBusy, apperror.CodeRateLimitUnavailable, apperror.CodePostReadUnavailable:
		return true
	default:
		return false
	}
}
