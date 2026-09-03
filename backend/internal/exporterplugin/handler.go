package exporterplugin

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

const pluginID = "redis-exporter"

type Handler struct{ client *Client }

func NewHandler(client *Client) *Handler { return &Handler{client: client} }
func (h *Handler) List(c *gin.Context) {
	items, err := h.client.List(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, http.StatusOK, items)
}
func (h *Handler) Get(c *gin.Context) {
	id, ok := pluginIdentifier(c)
	if !ok {
		return
	}
	item, err := h.client.Get(c.Request.Context(), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, http.StatusOK, item)
}
func (h *Handler) Start(c *gin.Context) {
	id, ok := pluginIdentifier(c)
	if !ok {
		return
	}
	item, err := h.client.Start(c.Request.Context(), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, http.StatusOK, item)
}
func (h *Handler) Stop(c *gin.Context) {
	id, ok := pluginIdentifier(c)
	if !ok {
		return
	}
	item, err := h.client.Stop(c.Request.Context(), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Data(c, http.StatusOK, item)
}
func (h *Handler) Install(c *gin.Context) {
	h.upload(c, "/internal/v1/exporter-plugins/install", http.StatusCreated)
}
func (h *Handler) Update(c *gin.Context) {
	id, ok := pluginIdentifier(c)
	if !ok {
		return
	}
	h.upload(c, "/internal/v1/exporter-plugins/"+id+"/update", http.StatusOK)
}
func pluginIdentifier(c *gin.Context) (string, bool) {
	if c.Param("pluginId") != pluginID {
		response.Error(c, apperror.New(apperror.CodePluginNotFound, "plugin was not found"))
		return "", false
	}
	return pluginID, true
}
func (h *Handler) upload(c *gin.Context, path string, successStatus int) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxPackageBytes+1<<20)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		response.Error(c, apperror.New(apperror.CodePluginPackageInvalid, "plugin package is invalid"))
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "package" || part.FileName() == "" {
		response.Error(c, apperror.New(apperror.CodePluginPackageInvalid, "plugin package is invalid"))
		return
	}
	defer part.Close()
	item, status, err := h.client.Upload(c.Request.Context(), path, func(writer *multipart.Writer) error {
		target, createErr := writer.CreateFormFile("package", "plugin.tar.gz")
		if createErr != nil {
			return createErr
		}
		written, copyErr := io.Copy(target, io.LimitReader(part, MaxPackageBytes+1))
		if copyErr != nil {
			return copyErr
		}
		if written > MaxPackageBytes {
			return errors.New("package too large")
		}
		next, nextErr := reader.NextPart()
		if next != nil || !errors.Is(nextErr, io.EOF) {
			return errors.New("unexpected multipart field")
		}
		return nil
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	if status != successStatus {
		response.Error(c, apperror.New(apperror.CodeMonitorUnavailable, "monitor service is unavailable"))
		return
	}
	response.Data(c, successStatus, item)
}
