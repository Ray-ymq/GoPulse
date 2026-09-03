package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
type Server struct {
	token   string
	root    string
	manager pluginManager
	logger  *slog.Logger
	handler http.Handler
}
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(token, root string, manager pluginManager, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	s := &Server{token: token, root: root, manager: manager, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.auth(s.ready))
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
