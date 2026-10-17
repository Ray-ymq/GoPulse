package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Checker interface{ Ready(context.Context) error }
type Server struct {
	probes         *componentmetrics.Probes
	server         *http.Server
	token          string
	timeout        time.Duration
	kafka, storage Checker
	logger         *slog.Logger
	delivery       func() any
}

func New(host string, port int, token string, timeout time.Duration, kafka, storage Checker, logger *slog.Logger) *Server {
	s := &Server{token: token, timeout: timeout, kafka: kafka, storage: storage, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	mux.HandleFunc("GET /internal/v1/telemetry-delivery", s.auth(s.deliveryStatus))
	s.server = &http.Server{Addr: net.JoinHostPort(host, strconv.Itoa(port)), Handler: componentmetrics.HTTP(mux, logger), ReadHeaderTimeout: 2 * time.Second}
	p, _ := componentmetrics.NewProbes(context.Background(), timeout, 250*time.Millisecond, s.Check)
	p.Started()
	s.SetProbes(p)
	return s
}
func (s *Server) SetDeliveryStatus(status func() any) { s.delivery = status }
func (s *Server) ListenAndServe() error               { return s.server.ListenAndServe() }
func (s *Server) Shutdown(ctx context.Context) error {
	s.probes.Stop()
	err := s.server.Shutdown(ctx)
	if err != nil {
		_ = s.server.Close()
	}
	return err
}
func (s *Server) SetProbes(p *componentmetrics.Probes) {
	s.probes = p
	s.server.Handler = p.Wrap(s.server.Handler)
}
func (s *Server) Check(ctx context.Context) error {
	if s.kafka == nil || s.storage == nil {
		return errors.New("dependency_unavailable")
	}
	if err := s.kafka.Ready(ctx); err != nil {
		return err
	}
	return s.storage.Ready(ctx)
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Authorization")) != 1 || !authorized(r.Header.Get("Authorization"), s.token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="marshaller"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
			return
		}
		if r.URL.RawQuery != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid_request"})
			return
		}
		next(w, r)
	}
}
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
func (s *Server) deliveryStatus(w http.ResponseWriter, _ *http.Request) {
	if s.delivery == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "unknown", "reason_code": "not_observed"}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.delivery()})
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
