package exporterplugin

import (
	"bytes"
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
)

type CatalogItem struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Source           string       `json:"source"`
	Available        bool         `json:"available"`
	Schema           ConfigSchema `json:"schema"`
	Configured       bool         `json:"configured"`
	SecretConfigured bool         `json:"secret_configured"`
	Revision         string       `json:"revision"`
	Summary          string       `json:"summary"`
}

func (c *Client) Catalog(ctx context.Context) ([]CatalogItem, error) {
	body, _, err := c.request(ctx, http.MethodGet, "/internal/v1/exporter-plugins/catalog", nil, "", http.StatusOK)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []CatalogItem `json:"data"`
	}
	if !uniqueJSON(body) || decodeStrict(body, &result) != nil || len(result.Data) == 0 || len(result.Data) > 64 {
		return nil, monitorUnavailable()
	}
	seen := map[string]bool{}
	for _, item := range result.Data {
		if !validPluginID(item.ID) || !validSafeName(item.Name, 80) || !validSource(item.Source) || item.Schema.PluginID != item.ID || item.Schema.SchemaVersion != 1 || len(item.Schema.Fields) == 0 || !validConfigSchema(item.Schema) || seen[item.ID] || (!item.Available && item.Configured) || (item.SecretConfigured && !item.Configured) {
			return nil, monitorUnavailable()
		}
		seen[item.ID] = true
		if !item.Configured {
			if item.Revision != "" || item.Summary != "not_configured" {
				return nil, monitorUnavailable()
			}
		} else {
			if len(item.Revision) != 32 || strings.Trim(item.Revision, "0123456789abcdef") != "" || (item.Summary != "configured" && item.Summary != "upgrade_required") {
				return nil, monitorUnavailable()
			}
		}
	}
	return result.Data, nil
}

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,127}$`)

func validPluginID(id string) bool { return pluginIDPattern.MatchString(id) }

func validSource(source string) bool {
	if source == "" || len(source) > 128 {
		return false
	}
	for index, r := range source {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
		if index == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func validSafeName(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validConfigSchema(schema ConfigSchema) bool {
	seen := map[string]bool{}
	for _, field := range schema.Fields {
		if !validConfigField(field) || seen[field.Name] {
			return false
		}
		seen[field.Name] = true
	}
	return true
}

func validConfigField(field ConfigField) bool {
	if !validFieldName(field.Name) || (field.Type == "secret") != field.Secret {
		return false
	}
	switch field.Type {
	case "hostname", "port", "integer", "string", "secret", "duration", "url":
	default:
		return false
	}
	if field.Minimum != nil && field.Maximum != nil && *field.Minimum > *field.Maximum {
		return false
	}
	seen := map[string]bool{}
	for _, item := range field.Enum {
		if item == "" || seen[item] {
			return false
		}
		seen[item] = true
	}
	return true
}

func validFieldName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
		if index == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}
func (h *Handler) Catalog(c *gin.Context) {
	items, err := h.client.Catalog(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, http.StatusOK, items)
}
func (h *Handler) Configuration(c *gin.Context) {
	id, ok := pluginIdentifier(c)
	if !ok {
		return
	}
	content, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || content != "application/json" || len(params) != 0 || len(c.Request.Header.Values("Content-Type")) != 1 || len(c.Request.Header.Values("Content-Encoding")) != 0 {
		response.Error(c, invalidConfig())
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		response.Error(c, invalidConfig())
		return
	}
	action := "configuration"
	if c.Request.Method == http.MethodPost {
		action = "install"
	}
	if strings.HasSuffix(c.Request.URL.Path, "/connection-test") {
		action = "connection-test"
	}
	items, catalogErr := h.client.Catalog(c.Request.Context())
	if catalogErr != nil {
		response.Error(c, catalogErr)
		return
	}
	var catalogItem *CatalogItem
	for i := range items {
		if items[i].ID == id {
			catalogItem = &items[i]
			break
		}
	}
	if catalogItem == nil || !catalogItem.Available || !validateConfigurationRequest(body, catalogItem.Schema, action == "configuration") {
		response.Error(c, invalidConfig())
		return
	}

	path := "/internal/v1/exporter-plugins/" + id + "/" + action
	if action == "connection-test" {
		raw, _, err := h.client.request(c.Request.Context(), http.MethodPost, path, bytes.NewReader(body), "application/json", http.StatusOK)
		if err == nil {
			var result struct {
				Data struct {
					Reachable bool `json:"reachable"`
				} `json:"data"`
			}
			if !uniqueJSON(raw) || decodeStrict(raw, &result) != nil || !result.Data.Reachable {
				err = monitorUnavailable()
			}
		}
		if err != nil {
			response.Error(c, err)
			return
		}
		response.Data(c, http.StatusOK, map[string]bool{"reachable": true})
		return
	}
	status := http.StatusOK
	if action == "install" {
		status = http.StatusCreated
	}
	item, err := h.client.statusRequest(c.Request.Context(), c.Request.Method, path, bytes.NewReader(body), "application/json", status)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, status, item)
}

// RequestShape rejects query/body decoration before any upstream operation.
func (h *Handler) RequestShape(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		response.Error(c, invalidConfig())
		c.Abort()
		return
	}
	if c.Request.Method == http.MethodGet || strings.HasSuffix(c.Request.URL.Path, "/start") || strings.HasSuffix(c.Request.URL.Path, "/stop") {
		data, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
		if err != nil || len(data) != 0 {
			response.Error(c, invalidConfig())
			c.Abort()
			return
		}
	}
	c.Next()
}
