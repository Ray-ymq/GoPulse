package httpserver

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/router/internal/config"
	"github.com/Ray-ymq/GoPulse/router/internal/envelope"
	"github.com/Ray-ymq/GoPulse/router/internal/routing"
)

type Producer interface {
	Produce(context.Context, string, string, []byte) error
	Ready(context.Context, string) error
}

type Server struct {
	httpServer     *http.Server
	producer       Producer
	token          string
	requestTimeout time.Duration
	maxBodyBytes   int64
	logger         *slog.Logger
}

func New(cfg config.Config, producer Producer, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		producer: producer, token: cfg.APIToken, requestTimeout: cfg.RequestTimeout,
		maxBodyBytes: cfg.MaxMessageBytes, logger: logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	mux.HandleFunc("POST /internal/v1/messages", s.publish)
	s.httpServer = &http.Server{
		Addr:              cfg.Address(),
		Handler:           mux,
		ReadHeaderTimeout: cfg.RequestTimeout,
		ReadTimeout:       cfg.RequestTimeout,
		WriteTimeout:      cfg.RequestTimeout + time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	return s
}

func (s *Server) Handler() http.Handler              { return s.httpServer.Handler }
func (s *Server) ListenAndServe() error              { return s.httpServer.ListenAndServe() }
func (s *Server) Serve(listener net.Listener) error  { return s.httpServer.Serve(listener) }
func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "router"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "internal_authentication_required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()
	if err := s.producer.Ready(ctx, config.Topic); err != nil {
		s.logger.Warn("readiness check failed", "event", "router_not_ready")
		writeError(w, http.StatusServiceUnavailable, "kafka_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "service": "router"})
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "internal_authentication_required")
		return
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || len(r.Header.Values("Content-Encoding")) != 0 {
		writeError(w, http.StatusBadRequest, "message_invalid")
		return
	}
	mediaType, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" || len(params) != 0 {
		writeError(w, http.StatusBadRequest, "message_invalid")
		return
	}
	if r.ContentLength > s.maxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "message_too_large")
		return
	}
	body, tooLarge, err := readBody(r.Body, s.maxBodyBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "message_invalid")
		return
	}
	if tooLarge {
		writeError(w, http.StatusRequestEntityTooLarge, "message_too_large")
		return
	}
	message, err := envelope.Validate(body)
	if err != nil {
		var unsupported envelope.UnsupportedError
		if errors.As(err, &unsupported) {
			writeError(w, http.StatusUnprocessableEntity, "message_type_unsupported")
			return
		}
		writeError(w, http.StatusBadRequest, "message_invalid")
		return
	}
	idempotencyValues := r.Header.Values("Idempotency-Key")
	if len(idempotencyValues) != 1 || idempotencyValues[0] != message.MessageID {
		writeError(w, http.StatusBadRequest, "message_invalid")
		return
	}
	topic, ok := routing.Topic(message.Type)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "message_type_unsupported")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()
	if err := s.producer.Produce(ctx, topic, message.MessageID, message.Body); err != nil {
		s.logger.Warn("message production failed", "event", "kafka_unavailable")
		writeError(w, http.StatusServiceUnavailable, "kafka_unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) authorized(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return false
	}
	provided := sha256.Sum256([]byte(strings.TrimPrefix(values[0], "Bearer ")))
	expected := sha256.Sum256([]byte(s.token))
	return subtle.ConstantTimeCompare(provided[:], expected[:]) == 1
}

func readBody(body io.Reader, limit int64) ([]byte, bool, error) {
	limited := io.LimitReader(body, limit+1)
	value, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(value)) > limit {
		return nil, true, nil
	}
	return value, false, nil
}

func writeError(w http.ResponseWriter, status int, code string) {
	messages := map[string]string{
		"internal_authentication_required": "internal authentication is required",
		"message_invalid":                  "message is invalid",
		"message_too_large":                "message is too large",
		"message_type_unsupported":         "message type is unsupported",
		"kafka_unavailable":                "message transport is unavailable",
		"internal_error":                   "internal error",
	}
	message, ok := messages[code]
	if !ok {
		code, message, status = "internal_error", messages["internal_error"], http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
