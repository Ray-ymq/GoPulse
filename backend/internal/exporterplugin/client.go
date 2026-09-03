package exporterplugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const MaxPackageBytes int64 = 64 << 20

type Client struct {
	baseURL string
	token   string
	client  *http.Client
}
type Status struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Version       string     `json:"version"`
	Kind          string     `json:"kind"`
	Source        string     `json:"source"`
	DesiredState  string     `json:"desired_state"`
	ObservedState string     `json:"observed_state"`
	InstalledAt   time.Time  `json:"installed_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `json:"started_at"`
	LastScrapeAt  *time.Time `json:"last_scrape_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     any        `json:"last_error,omitempty"`
}
type dataEnvelope[T any] struct {
	Data T `json:"data"`
}
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewClient(baseURL, token string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("invalid monitor URL")
	}
	if len(token) < 32 {
		return nil, errors.New("monitor token must contain at least 32 bytes")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: timeout}}, nil
}
func (c *Client) List(ctx context.Context) ([]Status, error) {
	var out dataEnvelope[[]Status]
	err := c.do(ctx, http.MethodGet, "/internal/v1/exporter-plugins", nil, "", &out)
	return out.Data, err
}
func (c *Client) Get(ctx context.Context, id string) (Status, error) {
	var out dataEnvelope[Status]
	err := c.do(ctx, http.MethodGet, "/internal/v1/exporter-plugins/"+url.PathEscape(id), nil, "", &out)
	return out.Data, err
}
func (c *Client) Start(ctx context.Context, id string) (Status, error) {
	return c.action(ctx, id, "start")
}
func (c *Client) Stop(ctx context.Context, id string) (Status, error) {
	return c.action(ctx, id, "stop")
}
func (c *Client) action(ctx context.Context, id, action string) (Status, error) {
	var out dataEnvelope[Status]
	err := c.do(ctx, http.MethodPost, "/internal/v1/exporter-plugins/"+url.PathEscape(id)+"/"+action, http.NoBody, "", &out)
	return out.Data, err
}
func (c *Client) Upload(ctx context.Context, path string, boundaryWriter func(*multipart.Writer) error) (Status, int, error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	contentType := multipartWriter.FormDataContentType()
	go func() {
		err := boundaryWriter(multipartWriter)
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
	}()
	var out dataEnvelope[Status]
	status, err := c.doStatus(ctx, http.MethodPost, path, reader, contentType, &out)
	return out.Data, status, err
}
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	_, err := c.doStatus(ctx, method, path, body, contentType, out)
	return err
}
func (c *Client) doStatus(ctx context.Context, method, path string, body io.Reader, contentType string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, monitorUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return 0, monitorUnavailable()
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 1<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope errorEnvelope
		if json.NewDecoder(limited).Decode(&envelope) != nil {
			return response.StatusCode, monitorUnavailable()
		}
		return response.StatusCode, mapMonitorError(envelope.Error.Code)
	}
	if err = json.NewDecoder(limited).Decode(out); err != nil {
		return response.StatusCode, monitorUnavailable()
	}
	return response.StatusCode, nil
}
func mapMonitorError(code string) error {
	switch code {
	case "plugin_package_invalid":
		return apperror.New(apperror.CodePluginPackageInvalid, "plugin package is invalid")
	case "plugin_not_found":
		return apperror.New(apperror.CodePluginNotFound, "plugin was not found")
	case "plugin_conflict":
		return apperror.New(apperror.CodePluginConflict, "plugin conflicts with the installed state")
	case "plugin_operation_in_progress":
		return apperror.New(apperror.CodePluginOperationInProgress, "plugin operation is already in progress")
	case "plugin_operation_failed":
		return apperror.New(apperror.CodePluginOperationFailed, "plugin operation failed")
	default:
		return monitorUnavailable()
	}
}
func monitorUnavailable() error {
	return apperror.New(apperror.CodeMonitorUnavailable, "monitor service is unavailable")
}
