package runtime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"time"

	"github.com/Ray-ymq/GoPulse/exporters/kafka/internal/collector"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
)

type Config struct {
	Host, Port, Listen, JMXURL    string
	ConnectTimeout, ScrapeTimeout time.Duration
}

var ErrConfig = errors.New("invalid_configuration")

func Load() (Config, error) {
	if err := runtimekit.ValidateRuntimeEnvironment("kafka-exporter"); err != nil {
		return Config{}, err
	}

	c := Config{Host: os.Getenv("KAFKA_HOST"), Port: os.Getenv("KAFKA_PORT"), JMXURL: strings.TrimSpace(os.Getenv("KAFKA_EXPORTER_JMX_URL"))}
	mode := os.Getenv("GOPULSE_RUNTIME_MODE")
	if !validTarget(mode, c.Host, c.Port) {
		return c, ErrConfig
	}
	if c.JMXURL != "" && !validJMXURL(mode, c.JMXURL) {
		return c, ErrConfig
	}
	if os.Getenv("KAFKA_TOPIC") != collector.Topic || os.Getenv("KAFKA_CONSUMER_GROUP") != collector.Group {
		return c, ErrConfig
	}
	var err error
	c.ConnectTimeout, err = time.ParseDuration(os.Getenv("KAFKA_EXPORTER_CONNECT_TIMEOUT"))
	if err != nil {
		return c, ErrConfig
	}
	c.ScrapeTimeout, err = time.ParseDuration(os.Getenv("KAFKA_EXPORTER_SCRAPE_TIMEOUT"))
	if err != nil || c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > c.ScrapeTimeout || c.ScrapeTimeout > 10*time.Second {
		return c, ErrConfig
	}
	host, port := os.Getenv("KAFKA_EXPORTER_HTTP_HOST"), os.Getenv("KAFKA_EXPORTER_HTTP_PORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "9124"
	}
	if !validListen(mode, host, port) {
		return c, ErrConfig
	}
	c.Listen = net.JoinHostPort(host, port)
	return c, nil
}

func validTarget(mode, host, port string) bool {
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 || host == "" || strings.ContainsAny(host, "\x00\r\n \t/@") {
		return false
	}
	ip := net.ParseIP(host)
	if mode == "host" || mode == "" {
		return host == "localhost" || ip != nil && ip.IsLoopback()
	}
	return mode == "container" && ip == nil
}

func validJMXURL(mode, raw string) bool {
	if len(raw) > 512 || strings.ContainsAny(raw, "\x00\r\n \t") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return false
	}
	if strings.ContainsAny(u.Host, "\x00\r\n/@") || u.Hostname() == "" {
		return false
	}
	if port := u.Port(); port != "" {
		parsed, err := strconv.Atoi(port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return false
		}
	}
	if mode == "host" || mode == "" {
		ip := net.ParseIP(u.Hostname())
		return u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
	}
	return mode == "container"
}

func validListen(mode, host, port string) bool {
	ip := net.ParseIP(host)
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 || ip == nil {
		return false
	}
	return (mode == "container" && (ip.IsLoopback() || ip.IsUnspecified())) || ((mode == "host" || mode == "") && ip.IsLoopback())
}
func Open(c Config) (*collector.Client, error) {
	// Pin the public request shape, notably OffsetFetch v7 (Kafka 2.8).
	// The supported single broker must never redirect the client to an arbitrary origin.
	address := net.JoinHostPort(c.Host, c.Port)
	client, err := kgo.NewClient(kgo.SeedBrokers(address), kgo.RequestTimeoutOverhead(c.ScrapeTimeout), kgo.RetryTimeout(c.ScrapeTimeout), kgo.ClientID("gopulse-kafka-exporter"), kgo.MaxVersions(kversion.V2_8_0()), kgo.BrokerMaxReadBytes(1<<20), kgo.FetchMaxBytes(512<<10), kgo.Dialer(func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != address {
			return nil, collector.ErrUnavailable
		}
		return (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext(ctx, network, addr)
	}))
	if err != nil {
		return nil, ErrConfig
	}
	var jmxHTTP *http.Client
	if c.JMXURL != "" {
		transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext, DisableCompression: true, ResponseHeaderTimeout: c.ScrapeTimeout}
		jmxHTTP = &http.Client{Transport: transport, Timeout: c.ScrapeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &collector.Client{Kafka: client, JMXURL: c.JMXURL, JMXHTTP: jmxHTTP}, nil
}

func Handler(db *collector.Client, timeout time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"kafka-exporter"}`))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		body, err := collector.Collect(ctx, db)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			body = collector.Unavailable
		}
		w.Write([]byte(body))
	})
	return mux
}
