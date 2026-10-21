package collector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Ray-ymq/GoPulse/exporters/std_exporter/config"
)

var ErrUnavailable = errors.New("target_unavailable")

// Client 是 HTTP 示例客户端。SQL 或 SDK 插件在此替换目标访问实现。
type Client struct {
	config     *config.Config
	httpClient *http.Client
	baseURL    string
	transport  *http.Transport
}

func NewClient(conf *config.Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: conf.ConnectTimeout}).DialContext
	transport.ResponseHeaderTimeout = conf.ScrapeTimeout
	transport.DisableCompression = true
	return &Client{config: conf, transport: transport,
		baseURL: (&url.URL{Scheme: "http", Host: net.JoinHostPort(conf.Host, strconv.Itoa(conf.Port))}).String(),
		httpClient: &http.Client{Transport: transport, Timeout: conf.ScrapeTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) request(ctx context.Context, endpoint string) (*http.Response, error) {
	if endpoint == "" || endpoint[0] != '/' {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.SetBasicAuth(c.config.User, c.config.Password)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" {
		_ = resp.Body.Close()
		return nil, ErrUnavailable
	}
	return resp, nil
}

func (c *Client) Connect(ctx context.Context) error {
	resp, err := c.request(ctx, "/")
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) FetchJSON(ctx context.Context, endpoint string) (map[string]interface{}, error) {
	resp, err := c.request(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	const maxBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return nil, ErrUnavailable
	}
	var result map[string]interface{}
	if json.Unmarshal(body, &result) != nil || result == nil {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (c *Client) Close()                    { c.transport.CloseIdleConnections() }
func (c *Client) GetConfig() *config.Config { return c.config }
func (c *Client) GetBaseURL() string        { return c.baseURL }
