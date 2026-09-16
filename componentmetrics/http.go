package componentmetrics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type requestIDKey struct{}

func ValidRequestID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func NewRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
func NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, url, body)
	if err == nil {
		if id := RequestID(ctx); ValidRequestID(id) {
			r.Header.Set("X-Request-ID", id)
		}
	}
	return r, err
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}{Error: struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}{code, message, w.Header().Get("X-Request-ID")}})
}

type runtimeWriter struct {
	http.ResponseWriter
	status   int
	replaced bool
}

func (w *runtimeWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if (status == 404 || status == 405) && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.replaced = true
		code := "not_found"
		message := "route not found"
		if status == 405 {
			code = "method_not_allowed"
			message = "method not allowed"
		}
		WriteError(w.ResponseWriter, status, code, message)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *runtimeWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if w.replaced {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}
func (w *runtimeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// HTTP provides correlation, safe fallback errors and panic recovery. The edge
// always replaces external IDs; private callers may propagate a valid ID.
func HTTP(next http.Handler, logger *slog.Logger) http.Handler {
	logger = ModuleLogger(logger, "http")
	var sourceUnavailable atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if len(r.Header.Values("X-Request-ID")) != 1 || !ValidRequestID(id) {
			var err error
			id, err = NewRequestID()
			if err != nil {
				WriteError(w, 500, "internal_error", "an internal error occurred")
				return
			}
		}
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(WithRequestID(r.Context(), id))
		out := &runtimeWriter{ResponseWriter: w}
		started := time.Now()
		defer func() {
			if recover() != nil {
				if out.status == 0 {
					WriteError(out, 500, "internal_error", "an internal error occurred")
				}
				if logger != nil {
					logger.Error("http panic recovered", "event", "http_panic", "request_id", id)
				}
			}
			if logger != nil {
				status := out.status
				if status == 0 {
					status = 200
				}
				if r.URL.Path == "/metrics" {
					if status >= 500 && sourceUnavailable.CompareAndSwap(false, true) {
						logger.Warn("source unavailable", "request_id", id)
					}
					if status == 200 && sourceUnavailable.CompareAndSwap(true, false) {
						logger.Info("source restored", "request_id", id)
					}
				}
				logger.Info("http request completed", "event", "http_request", "request_id", id, "status", status, "duration_ms", time.Since(started).Milliseconds())
			}
		}()
		next.ServeHTTP(out, r)
	})
}
