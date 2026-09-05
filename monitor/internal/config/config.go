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
	HTTPHost             string
	HTTPPort             int
	APIToken             string
	PluginRoot           string
	RequestTimeout       time.Duration
	ShutdownTimeout      time.Duration
	StartupTimeout       time.Duration
	StopTimeout          time.Duration
	ScrapeInterval       time.Duration
	ScrapeTimeout        time.Duration
	PublishTimeout       time.Duration
	RouterURL            string
	RouterToken          string
	LogIngestToken       string
	LogMaxBytes          int64
	LogFutureSkew        time.Duration
	EventQueueCapacity   int
	EventRetryMin        time.Duration
	EventRetryMax        time.Duration
	EventShutdownTimeout time.Duration
	ExporterEnv          map[string]string
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
	hostIP := net.ParseIP(host)
	if strings.ContainsAny(host, "\x00\r\n") || hostIP == nil || !hostIP.IsLoopback() {
		return Config{}, errors.New("MONITOR_HTTP_HOST must be a loopback IP address")
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
	requestTimeout, err := parseDuration("MONITOR_REQUEST_TIMEOUT", 30*time.Second, time.Second, time.Minute)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := parseDuration("MONITOR_SHUTDOWN_TIMEOUT", 10*time.Second, time.Second, time.Minute)
	if err != nil {
		return Config{}, err
	}
	startupTimeout, err := parseDuration("MONITOR_PLUGIN_STARTUP_TIMEOUT", 10*time.Second, time.Second, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	stopTimeout, err := parseDuration("MONITOR_PLUGIN_STOP_TIMEOUT", 5*time.Second, time.Second, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	scrapeInterval, err := parseDuration("MONITOR_SCRAPE_INTERVAL", 15*time.Second, time.Second, 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	scrapeTimeout, err := parseDuration("MONITOR_SCRAPE_TIMEOUT", 3*time.Second, 100*time.Millisecond, 30*time.Second)
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
	logToken, ok := lookup("LOG_MONITOR_INGEST_TOKEN")
	if !ok || len(logToken) < 32 || strings.ContainsAny(logToken, "\r\n") || logToken == token {
		return Config{}, errors.New("LOG_MONITOR_INGEST_TOKEN must be a distinct token of at least 32 bytes")
	}
	logMax, err := strconv.ParseInt(value("MONITOR_LOG_MAX_BYTES", "65536"), 10, 64)
	if err != nil || logMax < 1024 || logMax > 65536 {
		return Config{}, errors.New("MONITOR_LOG_MAX_BYTES must be between 1024 and 65536")
	}
	logFutureSkew, err := parseDuration("MONITOR_LOG_FUTURE_SKEW", 5*time.Minute, 0, 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	eventCapacity, err := strconv.Atoi(value("MONITOR_EVENT_QUEUE_CAPACITY", "256"))
	if err != nil || eventCapacity < 1 || eventCapacity > 4096 {
		return Config{}, errors.New("MONITOR_EVENT_QUEUE_CAPACITY must be between 1 and 4096")
	}
	eventRetryMin, err := parseDuration("MONITOR_EVENT_RETRY_MIN", 250*time.Millisecond, 100*time.Millisecond, 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	eventRetryMax, err := parseDuration("MONITOR_EVENT_RETRY_MAX", 5*time.Second, eventRetryMin, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	eventShutdownTimeout, err := parseDuration("MONITOR_EVENT_SHUTDOWN_TIMEOUT", 5*time.Second, 0, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	if value("MONITOR_EVENT_MAX_BYTES", "16384") != "16384" {
		return Config{}, errors.New("MONITOR_EVENT_MAX_BYTES must be 16384")
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
	return Config{HTTPHost: host, HTTPPort: port, APIToken: token, PluginRoot: root, RequestTimeout: requestTimeout, ShutdownTimeout: shutdownTimeout, StartupTimeout: startupTimeout, StopTimeout: stopTimeout, ScrapeInterval: scrapeInterval, ScrapeTimeout: scrapeTimeout, PublishTimeout: publishTimeout, RouterURL: routerURL, RouterToken: routerToken, LogIngestToken: logToken, LogMaxBytes: logMax, LogFutureSkew: logFutureSkew, EventQueueCapacity: eventCapacity, EventRetryMin: eventRetryMin, EventRetryMax: eventRetryMax, EventShutdownTimeout: eventShutdownTimeout, ExporterEnv: env}, nil
}
func (c Config) HTTPAddress() string { return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort)) }
func (c Config) ExporterHealthURL() string {
	return "http://" + net.JoinHostPort(c.ExporterEnv["REDIS_EXPORTER_HTTP_HOST"], c.ExporterEnv["REDIS_EXPORTER_HTTP_PORT"]) + "/health"
}
