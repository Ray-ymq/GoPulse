package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPHost        string
	HTTPPort        int
	APIToken        string
	PluginRoot      string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	StartupTimeout  time.Duration
	StopTimeout     time.Duration
	ScrapeInterval  time.Duration
	ScrapeTimeout   time.Duration
	PublishTimeout  time.Duration
	RouterURL       string
	RouterToken     string
	ExporterEnv     map[string]string
}

func Load() (Config, error) { return LoadFrom(os.LookupEnv) }
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}
	value := func(k, fallback string) string {
		if v, ok := lookup(k); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	port, err := strconv.Atoi(value("MONITOR_HTTP_PORT", "9090"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, errors.New("MONITOR_HTTP_PORT must be between 1 and 65535")
	}
	host := value("MONITOR_HTTP_HOST", "127.0.0.1")
	if strings.ContainsAny(host, "\x00\r\n") || net.ParseIP(host) == nil {
		return Config{}, errors.New("MONITOR_HTTP_HOST must be an IP address")
	}
	token, ok := lookup("MONITOR_API_TOKEN")
	if !ok || len(token) < 32 || strings.ContainsAny(token, "\r\n") {
		return Config{}, errors.New("MONITOR_API_TOKEN must contain at least 32 bytes")
	}
	root := value("MONITOR_PLUGIN_ROOT", "")
	if root == "" || !filepath.IsAbs(root) {
		return Config{}, errors.New("MONITOR_PLUGIN_ROOT must be an absolute path")
	}
	root = filepath.Clean(root)
	parseDuration := func(key string, fallback, min, max time.Duration) (time.Duration, error) {
		raw := value(key, fallback.String())
		d, e := time.ParseDuration(raw)
		if e != nil || d < min || d > max {
			return 0, fmt.Errorf("%s is outside the allowed duration range", key)
		}
		return d, nil
	}
	requestTimeout, err := parseDuration("MONITOR_REQUEST_TIMEOUT", 70*time.Second, time.Second, 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := parseDuration("MONITOR_SHUTDOWN_TIMEOUT", 10*time.Second, time.Second, time.Minute)
	if err != nil {
		return Config{}, err
	}
	startupTimeout, err := parseDuration("MONITOR_PLUGIN_STARTUP_TIMEOUT", 10*time.Second, 100*time.Millisecond, time.Minute)
	if err != nil {
		return Config{}, err
	}
	stopTimeout, err := parseDuration("MONITOR_PLUGIN_STOP_TIMEOUT", 5*time.Second, 100*time.Millisecond, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	scrapeInterval, err := parseDuration("MONITOR_SCRAPE_INTERVAL", 15*time.Second, time.Second, 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	scrapeTimeout, err := parseDuration("MONITOR_SCRAPE_TIMEOUT", 3*time.Second, 100*time.Millisecond, time.Minute)
	if err != nil {
		return Config{}, err
	}
	if scrapeTimeout >= scrapeInterval {
		return Config{}, errors.New("MONITOR_SCRAPE_TIMEOUT must be less than MONITOR_SCRAPE_INTERVAL")
	}
	publishTimeout, err := parseDuration("MONITOR_PUBLISH_TIMEOUT", 3*time.Second, 100*time.Millisecond, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	routerURL := value("MONITOR_ROUTER_URL", "")
	routerToken, _ := lookup("MONITOR_ROUTER_TOKEN")
	if routerURL != "" && (len(routerToken) < 32 || strings.ContainsAny(routerToken, "\r\n")) {
		return Config{}, errors.New("MONITOR_ROUTER_TOKEN must contain at least 32 bytes when MONITOR_ROUTER_URL is set")
	}
	env := map[string]string{}
	for _, key := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB", "REDIS_EXPORTER_HTTP_HOST", "REDIS_EXPORTER_HTTP_PORT", "REDIS_EXPORTER_SCRAPE_TIMEOUT", "REDIS_EXPORTER_SHUTDOWN_TIMEOUT"} {
		if v, ok := lookup(key); ok {
			env[key] = v
		}
	}
	for _, key := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_DB"} {
		if strings.TrimSpace(env[key]) == "" {
			return Config{}, fmt.Errorf("%s is required", key)
		}
	}
	if env["REDIS_EXPORTER_HTTP_HOST"] == "" {
		env["REDIS_EXPORTER_HTTP_HOST"] = "127.0.0.1"
	}
	if env["REDIS_EXPORTER_HTTP_PORT"] == "" {
		env["REDIS_EXPORTER_HTTP_PORT"] = "9121"
	}
	return Config{HTTPHost: host, HTTPPort: port, APIToken: token, PluginRoot: root, RequestTimeout: requestTimeout, ShutdownTimeout: shutdownTimeout, StartupTimeout: startupTimeout, StopTimeout: stopTimeout, ScrapeInterval: scrapeInterval, ScrapeTimeout: scrapeTimeout, PublishTimeout: publishTimeout, RouterURL: routerURL, RouterToken: routerToken, ExporterEnv: env}, nil
}
func (c Config) HTTPAddress() string { return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort)) }
func (c Config) ExporterHealthURL() string {
	return "http://" + net.JoinHostPort(c.ExporterEnv["REDIS_EXPORTER_HTTP_HOST"], c.ExporterEnv["REDIS_EXPORTER_HTTP_PORT"]) + "/health"
}
