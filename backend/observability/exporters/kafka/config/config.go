package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Ray-ymq/GoPulse/exporters/kafka/common"

	"time"
)

type Config struct {
	Host, Port, Listen, JMXURL    string
	ConnectTimeout, ScrapeTimeout time.Duration
	ShutdownTimeout               time.Duration
}

var ErrConfig = errors.New("invalid_configuration")

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	if err := validateEnvironment(get); err != nil {
		return Config{}, err
	}

	c := Config{Host: get("KAFKA_HOST"), Port: get("KAFKA_PORT"), JMXURL: strings.TrimSpace(get("KAFKA_EXPORTER_JMX_URL"))}
	mode := get("GOPULSE_RUNTIME_MODE")
	if !validTarget(mode, c.Host, c.Port) {
		return c, ErrConfig
	}
	if c.JMXURL != "" && !validJMXURL(mode, c.JMXURL) {
		return c, ErrConfig
	}
	if get("KAFKA_TOPIC") != common.Topic || get("KAFKA_CONSUMER_GROUP") != common.Group {
		return c, ErrConfig
	}
	var err error
	c.ConnectTimeout, err = time.ParseDuration(get("KAFKA_EXPORTER_CONNECT_TIMEOUT"))
	if err != nil {
		return c, ErrConfig
	}
	c.ScrapeTimeout, err = time.ParseDuration(get("KAFKA_EXPORTER_SCRAPE_TIMEOUT"))
	if err != nil || c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > c.ScrapeTimeout || c.ScrapeTimeout > 10*time.Second {
		return c, ErrConfig
	}
	host, port := get("KAFKA_EXPORTER_HTTP_HOST"), get("KAFKA_EXPORTER_HTTP_PORT")
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
	c.ShutdownTimeout = 5 * time.Second
	if raw := get("KAFKA_EXPORTER_SHUTDOWN_TIMEOUT"); raw != "" {
		c.ShutdownTimeout, err = time.ParseDuration(raw)
		if err != nil || c.ShutdownTimeout <= 0 || c.ShutdownTimeout > 5*time.Second {
			return Config{}, ErrConfig
		}
	}
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

func (c Config) Redacted() Config { return c }
