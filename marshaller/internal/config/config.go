package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	Topic          = "gopulse-observability-v1"
	Group          = "gopulse-marshaller-metrics-v1"
	MaxRecordBytes = 1 << 20
	MaxOutputBytes = 2 << 20
)

type Config struct {
	HTTPHost             string
	HTTPPort             int
	APIToken             string
	KafkaBrokers         []string
	KafkaTopic           string
	KafkaGroup           string
	KafkaCommitTimeout   time.Duration
	VMURL                string
	VMUsername           string
	VMPassword           string
	VMTimeout            time.Duration
	ElasticsearchURL     string
	ElasticsearchTimeout time.Duration
	RetryMin             time.Duration
	RetryMax             time.Duration
	ReadinessTimeout     time.Duration
	ShutdownTimeout      time.Duration
	FutureSkew           time.Duration
	MaxRecordBytes       int
	MaxOutputBytes       int
}

func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}
	get := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	token, _ := lookup("MARSHALLER_API_TOKEN")
	password, _ := lookup("MARSHALLER_VM_PASSWORD")
	cfg := Config{
		HTTPHost: get("MARSHALLER_HTTP_HOST", "127.0.0.1"), APIToken: token,
		KafkaTopic: get("MARSHALLER_KAFKA_TOPIC", Topic), KafkaGroup: get("MARSHALLER_KAFKA_GROUP", Group),
		VMURL: get("MARSHALLER_VM_URL", "http://127.0.0.1:8428"), VMUsername: get("MARSHALLER_VM_USERNAME", "gopulse-marshaller"), VMPassword: password,
		ElasticsearchURL: get("MARSHALLER_ELASTICSEARCH_URL", "http://127.0.0.1:9200"),
		MaxRecordBytes:   MaxRecordBytes, MaxOutputBytes: MaxOutputBytes,
	}
	ip := net.ParseIP(cfg.HTTPHost)
	if ip == nil || !ip.IsLoopback() {
		return Config{}, errors.New("MARSHALLER_HTTP_HOST must be a loopback IP address")
	}
	if len(cfg.APIToken) < 32 || strings.ContainsAny(cfg.APIToken, "\r\n") {
		return Config{}, errors.New("MARSHALLER_API_TOKEN must contain at least 32 bytes")
	}
	if cfg.HTTPPort = parseInt(get("MARSHALLER_HTTP_PORT", "9093"), 1, 65535); cfg.HTTPPort == 0 {
		return Config{}, errors.New("MARSHALLER_HTTP_PORT must be an integer from 1 to 65535")
	}
	var err error
	if cfg.KafkaBrokers, err = parseBrokers(get("MARSHALLER_KAFKA_BROKERS", "127.0.0.1:9092")); err != nil {
		return Config{}, err
	}
	if cfg.KafkaTopic != Topic {
		return Config{}, fmt.Errorf("MARSHALLER_KAFKA_TOPIC must be %s", Topic)
	}
	if cfg.KafkaGroup != Group {
		return Config{}, fmt.Errorf("MARSHALLER_KAFKA_GROUP must be %s", Group)
	}
	if err := validateVMURL(cfg.VMURL); err != nil {
		return Config{}, err
	}
	if err := validateOrigin("MARSHALLER_ELASTICSEARCH_URL", cfg.ElasticsearchURL); err != nil {
		return Config{}, err
	}
	if get("MARSHALLER_LOG_TEMPLATE", "gopulse-logs-v1-template") != "gopulse-logs-v1-template" {
		return Config{}, errors.New("MARSHALLER_LOG_TEMPLATE must be gopulse-logs-v1-template")
	}
	if get("MARSHALLER_LOG_INDEX_PREFIX", "gopulse-logs-v1-") != "gopulse-logs-v1-" {
		return Config{}, errors.New("MARSHALLER_LOG_INDEX_PREFIX must be gopulse-logs-v1-")
	}
	if get("MARSHALLER_EVENT_TEMPLATE", "gopulse-events-v1-template") != "gopulse-events-v1-template" {
		return Config{}, errors.New("MARSHALLER_EVENT_TEMPLATE must be gopulse-events-v1-template")
	}
	if get("MARSHALLER_EVENT_INDEX_PREFIX", "gopulse-events-v1-") != "gopulse-events-v1-" {
		return Config{}, errors.New("MARSHALLER_EVENT_INDEX_PREFIX must be gopulse-events-v1-")
	}
	if cfg.VMUsername == "" || strings.ContainsAny(cfg.VMUsername, "\r\n:") {
		return Config{}, errors.New("MARSHALLER_VM_USERNAME is invalid")
	}
	if len(cfg.VMPassword) < 16 || strings.ContainsAny(cfg.VMPassword, "\r\n") {
		return Config{}, errors.New("MARSHALLER_VM_PASSWORD must contain at least 16 bytes")
	}
	for _, item := range []struct {
		target        *time.Duration
		key, fallback string
		min, max      time.Duration
	}{
		{&cfg.KafkaCommitTimeout, "MARSHALLER_KAFKA_COMMIT_TIMEOUT", "3s", 100 * time.Millisecond, 10 * time.Second},
		{&cfg.VMTimeout, "MARSHALLER_VM_TIMEOUT", "3s", 100 * time.Millisecond, 10 * time.Second},
		{&cfg.ElasticsearchTimeout, "MARSHALLER_ELASTICSEARCH_TIMEOUT", "3s", 100 * time.Millisecond, 10 * time.Second},
		{&cfg.RetryMin, "MARSHALLER_RETRY_MIN", "250ms", 10 * time.Millisecond, 10 * time.Second},
		{&cfg.RetryMax, "MARSHALLER_RETRY_MAX", "5s", 100 * time.Millisecond, time.Minute},
		{&cfg.ReadinessTimeout, "MARSHALLER_READINESS_TIMEOUT", "2s", 100 * time.Millisecond, 10 * time.Second},
		{&cfg.ShutdownTimeout, "MARSHALLER_SHUTDOWN_TIMEOUT", "10s", time.Second, time.Minute},
		{&cfg.FutureSkew, "MARSHALLER_FUTURE_SKEW", "5m", 0, 10 * time.Minute},
	} {
		if *item.target, err = parseDuration(get(item.key, item.fallback), item.min, item.max); err != nil {
			return Config{}, fmt.Errorf("%s is invalid", item.key)
		}
	}
	if cfg.RetryMax < cfg.RetryMin {
		return Config{}, errors.New("MARSHALLER_RETRY_MAX must be at least MARSHALLER_RETRY_MIN")
	}
	return cfg, nil
}

func parseInt(s string, min, max int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v < min || v > max {
		return 0
	}
	return v
}
func parseDuration(s string, min, max time.Duration) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d < min || d > max {
		return 0, errors.New("invalid duration")
	}
	return d, nil
}
func parseBrokers(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	if len(parts) < 1 || len(parts) > 16 {
		return nil, errors.New("MARSHALLER_KAFKA_BROKERS must contain 1 to 16 brokers")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		broker := strings.TrimSpace(raw)
		host, port, err := net.SplitHostPort(broker)
		if err != nil || host == "" || parseInt(port, 1, 65535) == 0 || strings.ContainsAny(host, "\r\n\t /?#@") {
			return nil, errors.New("MARSHALLER_KAFKA_BROKERS must contain valid host:port entries")
		}
		canonical := net.JoinHostPort(host, port)
		if _, ok := seen[canonical]; ok {
			return nil, errors.New("MARSHALLER_KAFKA_BROKERS must not contain duplicates")
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}
func validateVMURL(raw string) error {
	return validateOrigin("MARSHALLER_VM_URL", raw)
}

func validateOrigin(key, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("%s must be an HTTP origin without credentials, query, or fragment", key)
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s must use a loopback IP address", key)
	}
	if u.Port() == "" || parseInt(u.Port(), 1, 65535) == 0 {
		return fmt.Errorf("%s must include a valid port", key)
	}
	return nil
}
