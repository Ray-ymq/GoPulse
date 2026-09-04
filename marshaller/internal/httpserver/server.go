package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Checker interface{ Ready(context.Context) error }
type Server struct {
	server         *http.Server
	token          string
	timeout        time.Duration
	kafka, storage Checker
	logger         *slog.Logger
}

func New(host string, port int, token string, timeout time.Duration, kafka, storage Checker, logger *slog.Logger) *Server {
	s := &Server{token: token, timeout: timeout, kafka: kafka, storage: storage, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	s.server = &http.Server{Addr: host + ":" + strconv.Itoa(port), Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	return s
}
func (s *Server) ListenAndServe() error              { return s.server.ListenAndServe() }
func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "marshaller"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if !authorized(r.Header.Get("Authorization"), s.token) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="marshaller"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	if s.kafka == nil || s.storage == nil || s.kafka.Ready(ctx) != nil || s.storage.Ready(ctx) != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
func authorized(header, token string) bool {
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	candidate := strings.TrimPrefix(header, "Bearer ")
	return len(candidate) == len(token) && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
