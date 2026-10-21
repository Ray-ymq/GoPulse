package collector

import (
	"context"
	"net"
	"net/http"

	"github.com/Ray-ymq/GoPulse/exporters/kafka/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

func NewClient(c *config.Config) (*Client, error) {
	// Pin the public request shape, notably OffsetFetch v7 (Kafka 2.8).
	// The supported single broker must never redirect the client to an arbitrary origin.
	address := net.JoinHostPort(c.Host, c.Port)
	client, err := kgo.NewClient(kgo.SeedBrokers(address), kgo.RequestTimeoutOverhead(c.ScrapeTimeout), kgo.RetryTimeout(c.ScrapeTimeout), kgo.ClientID("gopulse-kafka-exporter"), kgo.MaxVersions(kversion.V2_8_0()), kgo.BrokerMaxReadBytes(1<<20), kgo.FetchMaxBytes(512<<10), kgo.Dialer(func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != address {
			return nil, ErrUnavailable
		}
		return (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext(ctx, network, addr)
	}))
	if err != nil {
		return nil, config.ErrConfig
	}
	var jmxHTTP *http.Client
	if c.JMXURL != "" {
		transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext, DisableCompression: true, ResponseHeaderTimeout: c.ScrapeTimeout}
		jmxHTTP = &http.Client{Transport: transport, Timeout: c.ScrapeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{Kafka: client, JMXURL: c.JMXURL, JMXHTTP: jmxHTTP}, nil
}

type readClient interface {
	kmsg.Requestor
	Close()
}
type Client struct {
	Kafka   readClient
	JMXHTTP *http.Client
	JMXURL  string
}

func (c *Client) CloseIdleConnections() {
	if c.Kafka != nil {
		c.Kafka.Close()
	}
	if c.JMXHTTP != nil {
		c.JMXHTTP.CloseIdleConnections()
	}
}
