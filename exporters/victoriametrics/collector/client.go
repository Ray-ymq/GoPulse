package collector

import (
	"net"
	"net/http"

	"github.com/Ray-ymq/GoPulse/exporters/victoriametrics/config"
)

func NewClient(c *config.Config) (*Client, error) {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext, DisableCompression: true, ResponseHeaderTimeout: c.ScrapeTimeout}
	client := &http.Client{Transport: transport, Timeout: c.ScrapeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	port := c.Port
	if port == "" {
		port = "8428"
	}
	return &Client{HTTP: client, Origin: "http://" + net.JoinHostPort(c.Host, port), Username: c.Username, Password: c.Password}, nil
}

type Client struct {
	HTTP                       *http.Client
	Origin, Username, Password string
}

func (c *Client) CloseIdleConnections() { c.HTTP.CloseIdleConnections() }
