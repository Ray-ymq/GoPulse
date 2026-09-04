package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/logs"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

type pluginManager interface {
	List() []plugin.Status
	Get(string) (plugin.Status, error)
	Install(context.Context, string) (plugin.Status, error)
	Start(context.Context, string) (plugin.Status, error)
	Stop(context.Context, string) (plugin.Status, error)
	Update(context.Context, string, string) (plugin.Status, error)
}
type logPublisher interface {
	PublishRaw(context.Context, string, any) error
}

type LogOptions struct {
	Token      string
	MaxBytes   int64
	FutureSkew time.Duration
	Publisher  logPublisher
	Now        func() time.Time
}

type Server struct {
	token   string
	root    string
	manager pluginManager
	logger  *slog.Logger
	handler http.Handler
	logs    LogOptions
}
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(token, root string, manager pluginManager, logger *slog.Logger, logOptions ...LogOptions) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	s := &Server{token: token, root: root, manager: manager, logger: logger}
	if len(logOptions) > 0 {
		s.logs = logOptions[0]
		if s.logs.Now == nil {
			s.logs.Now = time.Now
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.auth(s.ready))
	if s.logs.Token != "" {
		mux.HandleFunc("POST /internal/v1/logs", s.ingestLog)
	}
	mux.HandleFunc("GET /internal/v1/exporter-plugins", s.auth(s.list))
	mux.HandleFunc("GET /internal/v1/exporter-plugins/{pluginId}", s.auth(s.get))
	mux.HandleFunc("POST /internal/v1/exporter-plugins/install", s.auth(s.install))
	mux.HandleFunc("POST /internal/v1/exporter-plugins/{pluginId}/start", s.auth(s.start))
	mux.HandleFunc("POST /internal/v1/exporter-plugins/{pluginId}/stop", s.auth(s.stop))
	mux.HandleFunc("POST /internal/v1/exporter-plugins/{pluginId}/update", s.auth(s.update))
	s.handler = mux
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := "Bearer "
		header := r.Header.Get("Authorization")
		provided := ""
		if strings.HasPrefix(header, prefix) {
			provided = strings.TrimPrefix(header, prefix)
		}
		if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "internal_authentication_required", "internal authentication is required")
			return
		}
		next(w, r)
	}
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "monitor"})
}
func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "service": "monitor"})
}
func (s *Server) list(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.manager.List()})
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	status, err := s.manager.Get(r.PathValue("pluginId"))
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	status, err := s.manager.Start(r.Context(), r.PathValue("pluginId"))
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	status, err := s.manager.Stop(r.Context(), r.PathValue("pluginId"))
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}
func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	path, err := s.readPackage(w, r)
	if err != nil {
		writePluginError(w, err)
		return
	}
	defer os.Remove(path)
	status, err := s.manager.Install(r.Context(), path)
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": status})
}
func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	path, err := s.readPackage(w, r)
	if err != nil {
		writePluginError(w, err)
		return
	}
	defer os.Remove(path)
	status, err := s.manager.Update(r.Context(), r.PathValue("pluginId"), path)
	if err != nil {
		writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}
func (s *Server) readPackage(w http.ResponseWriter, r *http.Request) (string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, plugin.MaxPackageBytes+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		return "", plugin.NewError(plugin.CodePackageInvalid, "plugin package is invalid")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "package" || part.FileName() == "" {
		return "", plugin.NewError(plugin.CodePackageInvalid, "plugin package is invalid")
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, ".staging"), "upload-")
	if err != nil {
		return "", plugin.NewError(plugin.CodeFailed, "plugin operation failed")
	}
	name := tmp.Name()
	ok := false
	defer func() {
		part.Close()
		tmp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	written, copyErr := io.Copy(tmp, io.LimitReader(part, plugin.MaxPackageBytes+1))
	if copyErr != nil || written > plugin.MaxPackageBytes {
		return "", plugin.NewError(plugin.CodePackageInvalid, "plugin package is invalid")
	}
	if err = tmp.Sync(); err != nil {
		return "", plugin.NewError(plugin.CodeFailed, "plugin operation failed")
	}
	if err = tmp.Close(); err != nil {
		return "", plugin.NewError(plugin.CodeFailed, "plugin operation failed")
	}
	next, err := reader.NextPart()
	if !errors.Is(err, io.EOF) || next != nil {
		return "", plugin.NewError(plugin.CodePackageInvalid, "plugin package is invalid")
	}
	ok = true
	return name, nil
}
func writePluginError(w http.ResponseWriter, err error) {
	pe, ok := plugin.AsError(err)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	status := http.StatusUnprocessableEntity
	switch pe.Code {
	case plugin.CodePackageInvalid:
		status = http.StatusBadRequest
	case plugin.CodeNotFound:
		status = http.StatusNotFound
	case plugin.CodeConflict, plugin.CodeInProgress:
		status = http.StatusConflict
	}
	writeError(w, status, pe.Code, pe.Message)
}
func (s *Server) ingestLog(w http.ResponseWriter, r *http.Request) {
	values := r.Header.Values("Authorization")
	provided := ""
	if len(values) == 1 && strings.HasPrefix(values[0], "Bearer ") {
		provided = strings.TrimPrefix(values[0], "Bearer ")
	}
	if len(provided) != len(s.logs.Token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.logs.Token)) != 1 {
		writeError(w, http.StatusUnauthorized, "internal_authentication_required", "internal authentication is required")
		return
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || len(r.Header.Values("Content-Encoding")) != 0 {
		writeError(w, http.StatusBadRequest, "log_invalid", "log entry is invalid")
		return
	}
	mediaType, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" || len(params) != 0 {
		writeError(w, http.StatusBadRequest, "log_invalid", "log entry is invalid")
		return
	}
	ids := r.Header.Values("Idempotency-Key")
	if len(ids) != 1 {
		writeError(w, http.StatusBadRequest, "log_invalid", "log entry is invalid")
		return
	}
	if r.ContentLength > s.logs.MaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "log_too_large", "log entry is too large")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, s.logs.MaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "log_invalid", "log entry is invalid")
		return
	}
	if int64(len(body)) > s.logs.MaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "log_too_large", "log entry is too large")
		return
	}
	validated, err := logs.Validate(body, s.logs.Now(), s.logs.FutureSkew)
	if err != nil {
		writeError(w, http.StatusBadRequest, "log_invalid", "log entry is invalid")
		return
	}
	envelope, err := logs.NewEnvelope(ids[0], validated)
	if err != nil {
		writeError(w, http.StatusBadRequest, "log_invalid", "log entry is invalid")
		return
	}
	if s.logs.Publisher == nil || s.logs.Publisher.PublishRaw(r.Context(), envelope.MessageID, envelope) != nil {
		s.logger.Warn("log transport unavailable", "event", "log_publish_failed")
		writeError(w, http.StatusServiceUnavailable, "transport_unavailable", "message transport is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
