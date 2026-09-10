package httpserver

import (
	"context"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
	"io"
	"mime"
	"net/http"
)

type configuredManager interface {
	Catalog() []plugin.PublicCatalogEntry
	Configure(context.Context, string, []byte, bool) (plugin.Status, error)
	ConnectionTest(context.Context, string, []byte) error
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	m, ok := s.manager.(configuredManager)
	if !ok {
		writePluginError(w, plugin.NewError(plugin.CodeFailed, "plugin operation failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": m.Catalog()})
}
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) {
	m, ok := s.manager.(configuredManager)
	if !ok {
		writePluginError(w, plugin.NewError(plugin.CodeFailed, "plugin operation failed"))
		return
	}
	content, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" || len(params) != 0 || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) != 0 {
		writePluginError(w, plugin.NewError(plugin.CodePackageInvalid, "plugin configuration is invalid"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		writePluginError(w, plugin.NewError(plugin.CodePackageInvalid, "plugin configuration is invalid"))
		return
	}
	id := r.PathValue("pluginId")
	if r.Pattern == "POST /internal/v1/exporter-plugins/{pluginId}/connection-test" {
		if err = m.ConnectionTest(r.Context(), id, body); err != nil {
			writePluginError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"reachable": true}})
		return
	}
	install := r.Method == http.MethodPost
	status, err := m.Configure(r.Context(), id, body, install)
	if err != nil {
		writePluginError(w, err)
		return
	}
	code := http.StatusOK
	if install {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"data": status})
}
