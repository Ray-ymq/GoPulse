package runtime

import (
	"context"

	"errors"
	"net"
	"net/http"
	"os"

	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/exporters/victoriametrics/internal/collector"
)

type Config struct {
	Host, Username, Password, Listen string
	ConnectTimeout, ScrapeTimeout    time.Duration
}

var ErrConfig = errors.New("invalid_configuration")

func Load() (Config, error) {
	c := Config{Host: os.Getenv("VICTORIAMETRICS_HOST"), Username: os.Getenv("VICTORIAMETRICS_USERNAME"), Password: os.Getenv("VICTORIAMETRICS_PASSWORD")}
	mode := os.Getenv("GOPULSE_RUNTIME_MODE")
	if (mode == "container" && c.Host != "victoriametrics") || ((mode == "host" || mode == "") && c.Host != "127.0.0.1" && c.Host != "::1") || (mode != "" && mode != "host" && mode != "container") || os.Getenv("VICTORIAMETRICS_PORT") != "8428" {
		return Config{}, ErrConfig
	}
	for _, s := range []string{c.Username} {
		if s == "" {
			continue
		}
		if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 64 {
			return Config{}, ErrConfig
		}
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-", r) {
				return Config{}, ErrConfig
			}
		}
	}
	if !utf8.ValidString(c.Password) || len(c.Password) > 256 || strings.ContainsRune(c.Password, 0) {
		return Config{}, ErrConfig
	}
	if c.Username == "" || c.Password == "" {
		return Config{}, ErrConfig
	}
	var err error
	c.ConnectTimeout, err = time.ParseDuration(os.Getenv("VICTORIAMETRICS_EXPORTER_CONNECT_TIMEOUT"))
	if err != nil {
		return Config{}, ErrConfig
	}
	c.ScrapeTimeout, err = time.ParseDuration(os.Getenv("VICTORIAMETRICS_EXPORTER_SCRAPE_TIMEOUT"))
	if err != nil || c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > c.ScrapeTimeout || c.ScrapeTimeout > 10*time.Second {
		return Config{}, ErrConfig
	}
	host := os.Getenv("VICTORIAMETRICS_EXPORTER_HTTP_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("VICTORIAMETRICS_EXPORTER_HTTP_PORT")
	if port == "" {
		port = "9126"
	}
	if (host != "127.0.0.1" && host != "::1") || port != "9126" {
		return Config{}, ErrConfig
	}
	c.Listen = net.JoinHostPort(host, port)
	return c, nil
}

func Open(c Config) (*collector.Client, error) {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: c.ConnectTimeout}).DialContext, DisableCompression: true, ResponseHeaderTimeout: c.ScrapeTimeout}
	client := &http.Client{Transport: transport, Timeout: c.ScrapeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &collector.Client{HTTP: client, Origin: "http://" + net.JoinHostPort(c.Host, "8428"), Username: c.Username, Password: c.Password}, nil
}

func Handler(db *collector.Client, timeout time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"victoriametrics-exporter"}`))
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
