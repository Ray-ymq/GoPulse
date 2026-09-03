package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const Topic = "gopulse-observability-v1"

type Config struct {
	HTTPHost                string
	HTTPPort                int
	APIToken                string
	RequestTimeout          time.Duration
	ShutdownTimeout         time.Duration
	MaxMessageBytes         int64
	KafkaBrokers            []string
	KafkaTopic              string
	KafkaProduceTimeout     time.Duration
	KafkaMaxBufferedRecords int
	KafkaMaxBufferedBytes   int
}

func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return strings.TrimSpace(value)
		}
		return fallback
	}

	token, _ := lookup("ROUTER_API_TOKEN")
	cfg := Config{
		HTTPHost:   get("ROUTER_HTTP_HOST", "127.0.0.1"),
		APIToken:   token,
		KafkaTopic: get("ROUTER_KAFKA_TOPIC", Topic),
	}
	var err error
	if ip := net.ParseIP(cfg.HTTPHost); ip == nil {
		return Config{}, errors.New("ROUTER_HTTP_HOST must be an IP address")
	}
	if len(cfg.APIToken) < 32 || strings.ContainsAny(cfg.APIToken, "\r\n") {
		return Config{}, errors.New("ROUTER_API_TOKEN must contain at least 32 bytes")
	}
	if cfg.HTTPPort, err = integer(get("ROUTER_HTTP_PORT", "9091"), 1, 65535, "ROUTER_HTTP_PORT"); err != nil {
		return Config{}, err
	}
	if cfg.RequestTimeout, err = duration(get("ROUTER_REQUEST_TIMEOUT", "5s"), time.Second, 30*time.Second, "ROUTER_REQUEST_TIMEOUT"); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration(get("ROUTER_SHUTDOWN_TIMEOUT", "10s"), time.Second, time.Minute, "ROUTER_SHUTDOWN_TIMEOUT"); err != nil {
		return Config{}, err
	}
	messageBytes, err := integer(get("ROUTER_MAX_MESSAGE_BYTES", "1048576"), 1024, 1048576, "ROUTER_MAX_MESSAGE_BYTES")
	if err != nil {
		return Config{}, err
	}
	cfg.MaxMessageBytes = int64(messageBytes)
	cfg.KafkaBrokers, err = brokers(get("ROUTER_KAFKA_BROKERS", "127.0.0.1:9092"))
	if err != nil {
		return Config{}, err
	}
	if cfg.KafkaTopic != Topic {
		return Config{}, fmt.Errorf("ROUTER_KAFKA_TOPIC must be %s", Topic)
	}
	if cfg.KafkaProduceTimeout, err = duration(get("ROUTER_KAFKA_PRODUCE_TIMEOUT", "3s"), 100*time.Millisecond, 10*time.Second, "ROUTER_KAFKA_PRODUCE_TIMEOUT"); err != nil {
		return Config{}, err
	}
	if cfg.KafkaProduceTimeout >= cfg.RequestTimeout {
		return Config{}, errors.New("ROUTER_KAFKA_PRODUCE_TIMEOUT must be less than ROUTER_REQUEST_TIMEOUT")
	}
	if cfg.KafkaMaxBufferedRecords, err = integer(get("ROUTER_KAFKA_MAX_BUFFERED_RECORDS", "256"), 1, 1024, "ROUTER_KAFKA_MAX_BUFFERED_RECORDS"); err != nil {
		return Config{}, err
	}
	if cfg.KafkaMaxBufferedBytes, err = integer(get("ROUTER_KAFKA_MAX_BUFFERED_BYTES", "8388608"), 1048576, 67108864, "ROUTER_KAFKA_MAX_BUFFERED_BYTES"); err != nil {
		return Config{}, err
	}
	if int64(cfg.KafkaMaxBufferedBytes) < cfg.MaxMessageBytes {
		return Config{}, errors.New("ROUTER_KAFKA_MAX_BUFFERED_BYTES must not be smaller than ROUTER_MAX_MESSAGE_BYTES")
	}
	return cfg, nil
}

func (c Config) Address() string { return net.JoinHostPort(c.HTTPHost, strconv.Itoa(c.HTTPPort)) }

func integer(value string, min, max int, name string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, min, max)
	}
	return n, nil
}

func duration(value string, min, max time.Duration, name string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil || d < min || d > max {
		return 0, fmt.Errorf("%s must be between %s and %s", name, min, max)
	}
	return d, nil
}

func brokers(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	if len(parts) == 0 || len(parts) > 16 {
		return nil, errors.New("ROUTER_KAFKA_BROKERS must contain 1 to 16 brokers")
	}
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, raw := range parts {
		broker := strings.TrimSpace(raw)
		host, portText, err := net.SplitHostPort(broker)
		if err != nil || host == "" {
			return nil, errors.New("ROUTER_KAFKA_BROKERS must contain valid host:port entries")
		}
		if _, err := integer(portText, 1, 65535, "broker port"); err != nil {
			return nil, errors.New("ROUTER_KAFKA_BROKERS must contain valid host:port entries")
		}
		if strings.ContainsAny(host, "\r\n\t /?#@") {
			return nil, errors.New("ROUTER_KAFKA_BROKERS must contain valid host:port entries")
		}
		canonical := net.JoinHostPort(host, portText)
		if _, ok := seen[canonical]; ok {
			return nil, errors.New("ROUTER_KAFKA_BROKERS must not contain duplicates")
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result, nil
}
