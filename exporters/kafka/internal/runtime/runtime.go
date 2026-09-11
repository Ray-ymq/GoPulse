package runtime

import (
	"context"

	"errors"
	"net"
	"net/http"
	"os"

	"time"

	"github.com/Ray-ymq/GoPulse/exporters/kafka/internal/collector"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
)

type Config struct {
	Host, Port, Listen            string
	ConnectTimeout, ScrapeTimeout time.Duration
}

var ErrConfig = errors.New("invalid_configuration")

func Load() (Config, error) {
	c := Config{Host: os.Getenv("KAFKA_HOST"), Port: os.Getenv("KAFKA_PORT")}
	mode := os.Getenv("GOPULSE_RUNTIME_MODE")
	if mode == "container" {
		if c.Host != "kafka" || c.Port != "19092" {
			return c, ErrConfig
		}
	} else if mode == "host" || mode == "" {
		if (c.Host != "127.0.0.1" && c.Host != "::1") || c.Port != "9092" {
			return c, ErrConfig
		}
	} else {
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
	if (host != "127.0.0.1" && host != "::1") || port != "9124" {
		return c, ErrConfig
	}
	c.Listen = net.JoinHostPort(host, port)
	return c, nil
}
func Open(c Config) (*collector.Client, error) {
	// Pin the public request shape, notably OffsetFetch v7 (Kafka 2.8).
	// The supported single broker must never redirect the client to an arbitrary origin.
	address := net.JoinHostPort(c.Host, c.Port)
	client, err := kgo.NewClient(kgo.SeedBrokers(address), kgo.ClientID("gopulse-kafka-exporter"), kgo.MaxVersions(kversion.V2_8_0()), kgo.BrokerMaxReadBytes(1<<20), kgo.FetchMaxBytes(512<<10), kgo.Dialer(func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != address {
			return nil, collector.ErrUnavailable
		}
		return (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext(ctx, network, addr)
	}))
	if err != nil {
		return nil, ErrConfig
	}
	return &collector.Client{Kafka: client}, nil
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
