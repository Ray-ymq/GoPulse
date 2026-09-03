package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPHost        = "127.0.0.1"
	defaultHTTPPort        = 9121
	defaultScrapeTimeout   = 2 * time.Second
	defaultShutdownTimeout = 5 * time.Second
)

type Config struct {
	RedisHost       string
	RedisPort       int
	RedisPassword   string
	RedisDB         int
	HTTPHost        string
	HTTPPort        int
	ScrapeTimeout   time.Duration
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var err error

	cfg.RedisHost, err = requiredHost("REDIS_HOST")
	if err != nil {
		return Config{}, err
	}
	cfg.RedisPort, err = requiredPort("REDIS_PORT")
	if err != nil {
		return Config{}, err
	}
	cfg.RedisPassword = os.Getenv("REDIS_PASSWORD")
	cfg.RedisDB, err = requiredNonNegativeInt("REDIS_DB")
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPHost, err = optionalHost("REDIS_EXPORTER_HTTP_HOST", defaultHTTPHost)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPPort, err = optionalPort("REDIS_EXPORTER_HTTP_PORT", defaultHTTPPort)
	if err != nil {
		return Config{}, err
	}
	cfg.ScrapeTimeout, err = boundedDuration("REDIS_EXPORTER_SCRAPE_TIMEOUT", defaultScrapeTimeout, 100*time.Millisecond, 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.ShutdownTimeout, err = boundedDuration("REDIS_EXPORTER_SHUTDOWN_TIMEOUT", defaultShutdownTimeout, time.Second, 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) RedisAddress() string {
	return net.JoinHostPort(c.RedisHost, strconv.Itoa(c.RedisPort))
}
func (c Config) HTTPAddress() string { return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort)) }

func requiredHost(name string) (string, error) { return validateHost(name, os.Getenv(name)) }
func optionalHost(name, fallback string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		value = fallback
	}
	return validateHost(name, value)
}
func validateHost(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fieldError(name, "required")
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fieldError(name, "invalid")
	}
	if strings.Contains(value, ":") && net.ParseIP(strings.Trim(value, "[]")) == nil {
		return "", fieldError(name, "invalid")
	}
	return strings.Trim(value, "[]"), nil
}

func requiredPort(name string) (int, error) { return parsePort(name, os.Getenv(name)) }
func optionalPort(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	return parsePort(name, value)
}
func parsePort(name, value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fieldError(name, "invalid")
	}
	return port, nil
}
func requiredNonNegativeInt(name string) (int, error) {
	value := os.Getenv(name)
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 0 {
		return 0, fieldError(name, "invalid")
	}
	return int(parsed), nil
}
func boundedDuration(name string, fallback, min, max time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < min || parsed > max {
		return 0, fieldError(name, "invalid")
	}
	return parsed, nil
}
func fieldError(field, reason string) error {
	return fmt.Errorf("configuration field %s: %s", field, reason)
}

func Field(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	const prefix = "configuration field "
	if !strings.HasPrefix(message, prefix) {
		return "unknown"
	}
	rest := strings.TrimPrefix(message, prefix)
	field, _, ok := strings.Cut(rest, ":")
	if !ok || field == "" {
		return "unknown"
	}
	return field
}
