package victoriametrics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL, username, password string
	client                      *http.Client
}

func New(baseURL, username, password string, timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = timeout
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), username: username, password: password, client: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Write(ctx context.Context, body []byte) error {
	return c.do(ctx, http.MethodPost, "/api/v1/import/prometheus", body, "text/plain; version=0.0.4; charset=utf-8", http.StatusNoContent)
}
func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return errors.New("VictoriaMetrics health request creation failed")
	}
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New("VictoriaMetrics health request failed")
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if readErr != nil || len(body) > 4096 || resp.StatusCode != http.StatusOK {
		return errors.New("VictoriaMetrics unavailable")
	}
	return nil
}
func (c *Client) do(ctx context.Context, method, path string, body []byte, contentType string, expected int) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("VictoriaMetrics request creation failed")
	}
	req.SetBasicAuth(c.username, c.password)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || isTimeout(err) {
			return errors.New("VictoriaMetrics request timed out")
		}
		return errors.New("VictoriaMetrics request failed")
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if readErr != nil {
		return errors.New("VictoriaMetrics response could not be read")
	}
	if len(responseBody) > 4096 {
		return errors.New("VictoriaMetrics response exceeded limit")
	}
	if resp.StatusCode != expected {
		return errors.New("VictoriaMetrics rejected request")
	}
	if len(responseBody) != 0 {
		return errors.New("VictoriaMetrics returned an unexpected response body")
	}
	return nil
}
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
