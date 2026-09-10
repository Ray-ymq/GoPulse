package exporterplugin

import (
	"bytes"
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"reflect"
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
	if !uniqueJSON(body) || decodeStrict(body, &result) != nil || len(result.Data) != 6 {
		return nil, monitorUnavailable()
	}
	for i, item := range result.Data {
		entry := OfficialCatalog()[i]
		schema, _ := OfficialSchema(entry.ID)
		if item.ID != entry.ID || item.Source != entry.Source || item.Name != "GoPulse "+entry.Source+" Exporter" || !reflect.DeepEqual(item.Schema, schema) || (!entry.Available && item.Available) || (!item.Available && item.Configured) || item.SecretConfigured != item.Configured {
			return nil, monitorUnavailable()
		}
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
	// Both boundaries enforce the same config DTO. Backend permits either known
	// runtime origin; Monitor enforces its actual deployment mode and saved Secret.
	var previous *RedisSecret
	if action == "configuration" {
		previous = &RedisSecret{Password: "preserve-placeholder"}
	}
	_, _, err = ParseRedisConfigurationRequest(body, "container", previous)
	if err != nil {
		_, _, err = ParseRedisConfigurationRequest(body, "host", previous)
	}
	if err != nil {
		response.Error(c, invalidConfig())
		return
	}
	if id != "redis-exporter" {
		response.Error(c, apperror.New(apperror.CodePluginNotFound, "plugin was not found"))
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
