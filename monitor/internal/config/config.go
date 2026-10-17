package config

import (
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	RuntimeMode             RuntimeMode
	HTTPHost                string
	HTTPPort                int
	APIToken                string
	PluginRoot              string
	ReleaseCatalog          string
	BootstrapPackage        string
	RequestTimeout          time.Duration
	ShutdownTimeout         time.Duration
	StartupTimeout          time.Duration
	StopTimeout             time.Duration
	ScrapeInterval          time.Duration
	ScrapeTimeout           time.Duration
	PublishTimeout          time.Duration
	LogIngestToken          string
	LogMaxBytes             int64
	LogFutureSkew           time.Duration
	EventQueueCapacity      int
	EventRetryMin           time.Duration
	EventRetryMax           time.Duration
	EventShutdownTimeout    time.Duration
	KafkaBrokers            []string
	KafkaProduceTimeout     time.Duration
	KafkaMaxBufferedRecords int
	KafkaMaxBufferedBytes   int
	ExporterEnv             map[string]string
}

func Load() (Config, error) {
	if err := componentmetrics.ValidateRuntimeEnvironment("monitor"); err != nil {
		return Config{}, err
	}
	return LoadFrom(os.LookupEnv)
}
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}
	runtimeMode, err := loadRuntimeMode(lookup)
	if err != nil {
		return Config{}, err
	}
	for _, key := range []string{"MARSHALLER_ENDPOINTS"} {
		if raw, ok := lookup(key); ok && strings.TrimSpace(raw) != "" {
			if err := validateReplicaEndpoints(key, raw); err != nil {
				return Config{}, err
			}
		}
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
	if strings.ContainsAny(host, "\x00\r\n") {
		return Config{}, errors.New("MONITOR_HTTP_HOST must not contain control characters")
	}
	if err := validateListenHost(runtimeMode, "MONITOR_HTTP_HOST", host); err != nil {
		return Config{}, err
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
	bootstrapPackage := value("MONITOR_BOOTSTRAP_PACKAGE", "")
	if bootstrapPackage != "" {
		if strings.ContainsAny(bootstrapPackage, "\x00\r\n") || !filepath.IsAbs(bootstrapPackage) {
			return Config{}, errors.New("MONITOR_BOOTSTRAP_PACKAGE must be an absolute path")
		}
		bootstrapPackage = filepath.Clean(bootstrapPackage)
	}
	releaseCatalog := value("MONITOR_RELEASE_CATALOG", "")
	if releaseCatalog != "" {
		if strings.ContainsAny(releaseCatalog, "\x00\r\n") || !filepath.IsAbs(releaseCatalog) {
			return Config{}, errors.New("MONITOR_RELEASE_CATALOG must be an absolute path")
		}
		releaseCatalog = filepath.Clean(releaseCatalog)
	}
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
	kafkaDefault := "127.0.0.1:9092"
	if runtimeMode == RuntimeModeContainer {
		kafkaDefault = "kafka:19092"
	}
	kafkaBrokers, err := parseKafkaBrokers(value("MONITOR_KAFKA_BROKERS", kafkaDefault), runtimeMode)
	if err != nil {
		return Config{}, err
	}
	kafkaProduceTimeout, err := parseDuration("MONITOR_KAFKA_PRODUCE_TIMEOUT", 3*time.Second, 100*time.Millisecond, 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	kafkaMaxBufferedRecords, err := strconv.Atoi(value("MONITOR_KAFKA_MAX_BUFFERED_RECORDS", "128"))
	if err != nil || kafkaMaxBufferedRecords < 1 || kafkaMaxBufferedRecords > 1024 {
		return Config{}, errors.New("MONITOR_KAFKA_MAX_BUFFERED_RECORDS must be between 1 and 1024")
	}
	kafkaMaxBufferedBytes, err := strconv.Atoi(value("MONITOR_KAFKA_MAX_BUFFERED_BYTES", "8388608"))
	if err != nil || kafkaMaxBufferedBytes < 1<<20 || kafkaMaxBufferedBytes > 64<<20 {
		return Config{}, errors.New("MONITOR_KAFKA_MAX_BUFFERED_BYTES must be between 1048576 and 67108864")
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
	for _, key := range []string{"GOPULSE_RUNTIME_MODE", "REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB", "REDIS_EXPORTER_HTTP_HOST", "REDIS_EXPORTER_HTTP_PORT", "REDIS_EXPORTER_SCRAPE_TIMEOUT", "REDIS_EXPORTER_SHUTDOWN_TIMEOUT"} {
		if v, ok := lookup(key); ok {
			env[key] = v
		}
	}
	for _, key := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_DB"} {
		if strings.TrimSpace(env[key]) == "" {
			switch key {
			case "REDIS_HOST":
				env[key] = "127.0.0.1"
			case "REDIS_PORT":
				env[key] = "6379"
			case "REDIS_DB":
				env[key] = "0"
			}
		}
	}
	if err := validateDependencyHost(runtimeMode, "REDIS_HOST", env["REDIS_HOST"]); err != nil {
		return Config{}, err
	}
	if env["REDIS_EXPORTER_HTTP_HOST"] == "" {
		env["REDIS_EXPORTER_HTTP_HOST"] = "127.0.0.1"
	}
	if env["REDIS_EXPORTER_HTTP_PORT"] == "" {
		env["REDIS_EXPORTER_HTTP_PORT"] = "9121"
	}
	exporterIP := net.ParseIP(strings.TrimSpace(env["REDIS_EXPORTER_HTTP_HOST"]))
	if exporterIP == nil || !exporterIP.IsLoopback() {
		return Config{}, errors.New("REDIS_EXPORTER_HTTP_HOST must use a loopback IP address")
	}
	exporterPort, err := strconv.Atoi(strings.TrimSpace(env["REDIS_EXPORTER_HTTP_PORT"]))
	if err != nil || exporterPort < 1 || exporterPort > 65535 {
		return Config{}, errors.New("REDIS_EXPORTER_HTTP_PORT must be between 1 and 65535")
	}
	return Config{RuntimeMode: runtimeMode, HTTPHost: host, HTTPPort: port, APIToken: token, PluginRoot: root, ReleaseCatalog: releaseCatalog, BootstrapPackage: bootstrapPackage, RequestTimeout: requestTimeout, ShutdownTimeout: shutdownTimeout, StartupTimeout: startupTimeout, StopTimeout: stopTimeout, ScrapeInterval: scrapeInterval, ScrapeTimeout: scrapeTimeout, PublishTimeout: publishTimeout, LogIngestToken: logToken, LogMaxBytes: logMax, LogFutureSkew: logFutureSkew, EventQueueCapacity: eventCapacity, EventRetryMin: eventRetryMin, EventRetryMax: eventRetryMax, EventShutdownTimeout: eventShutdownTimeout, KafkaBrokers: kafkaBrokers, KafkaProduceTimeout: kafkaProduceTimeout, KafkaMaxBufferedRecords: kafkaMaxBufferedRecords, KafkaMaxBufferedBytes: kafkaMaxBufferedBytes, ExporterEnv: env}, nil
}

func parseKafkaBrokers(value string, runtimeMode RuntimeMode) ([]string, error) {
	parts := strings.Split(value, ",")
	if len(parts) < 1 || len(parts) > 16 {
		return nil, errors.New("MONITOR_KAFKA_BROKERS must contain 1 to 16 brokers")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		broker := strings.TrimSpace(raw)
		host, port, err := net.SplitHostPort(broker)
		if err != nil || host == "" || parsePort(port) == 0 || strings.ContainsAny(host, "\r\n\t /?#@") {
			return nil, errors.New("MONITOR_KAFKA_BROKERS must contain valid host:port entries")
		}
		if err := validateDependencyHost(runtimeMode, "MONITOR_KAFKA_BROKERS", host); err != nil {
			return nil, err
		}
		canonical := net.JoinHostPort(host, port)
		if _, ok := seen[canonical]; ok {
			return nil, errors.New("MONITOR_KAFKA_BROKERS must not contain duplicates")
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

func parsePort(value string) int {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

func validateReplicaEndpoints(key, raw string) error {
	parts := strings.Split(raw, ",")
	if len(parts) < 1 || len(parts) > 8 {
		return fmt.Errorf("%s must contain 1 to 8 endpoints", key)
	}
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		endpoint := strings.TrimSpace(part)
		if endpoint == "" || len(endpoint) > 63 || strings.ContainsAny(endpoint, " \t\r\n/:@") {
			return fmt.Errorf("%s contains an invalid endpoint", key)
		}
		for _, character := range endpoint {
			if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '.') {
				return fmt.Errorf("%s contains an invalid endpoint", key)
			}
		}
		if _, ok := seen[endpoint]; ok {
			return fmt.Errorf("%s contains duplicate endpoints", key)
		}
		seen[endpoint] = struct{}{}
	}
	return nil
}
func (c Config) HTTPAddress() string { return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort)) }
func (c Config) ExporterHealthURL() string {
	return "http://" + net.JoinHostPort(c.ExporterEnv["REDIS_EXPORTER_HTTP_HOST"], c.ExporterEnv["REDIS_EXPORTER_HTTP_PORT"]) + "/health"
}
