package collector

import (
	"context"
	"encoding/json"
	"io"

	"net"
	"net/http"

	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/config"
)

func NewClient(c *config.Config) (*Client, error) {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext, DisableCompression: true, ResponseHeaderTimeout: c.ScrapeTimeout}
	client := &http.Client{Transport: transport, Timeout: c.ScrapeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	port := c.Port
	if port == "" {
		port = "9200"
	}
	return &Client{HTTP: client, Origin: "http://" + net.JoinHostPort(c.Host, port), Username: c.Username, Password: c.Password}, nil
}

type Client struct {
	HTTP                       *http.Client
	Origin, Username, Password string
}

func (c *Client) CloseIdleConnections() { c.HTTP.CloseIdleConnections() }
func (c *Client) read(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Origin+path, nil)
	if err != nil {
		return ErrUnavailable
	}
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		return ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return ErrUnavailable
	}
	if json.Unmarshal(raw, dst) != nil {
		return ErrUnavailable
	}
	return nil
}

// Pointers distinguish a real zero from an incomplete upstream snapshot.
