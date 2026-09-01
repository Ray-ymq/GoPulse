package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	defaultAppEnv    = "development"
	defaultHTTPHost  = "127.0.0.1"
	defaultHTTPPort  = 8080
	defaultMySQLHost = "127.0.0.1"
	defaultMySQLPort = 3306
	defaultRedisHost = "127.0.0.1"
	defaultRedisPort = 6379
	defaultRedisDB   = 0
)

// LookupFunc makes configuration loading deterministic in tests without
// changing the process environment.
type LookupFunc func(string) (string, bool)

type Config struct {
	AppEnv      string
	HTTPHost    string
	HTTPPort    int
	MySQL       MySQLConfig
	Redis       RedisConfig
	RabbitMQURL string
}

type MySQLConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

func Load() (Config, error) {
	return LoadFrom(os.LookupEnv)
}

func LoadFrom(lookup LookupFunc) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}

	httpPort, err := integerValue(lookup, "HTTP_PORT", defaultHTTPPort)
	if err != nil {
		return Config{}, err
	}
	if err := validatePort("HTTP_PORT", httpPort); err != nil {
		return Config{}, err
	}

	mysqlPort, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return Config{}, err
	}
	if err := validatePort("MYSQL_PORT", mysqlPort); err != nil {
		return Config{}, err
	}

	redisPort, err := integerValue(lookup, "REDIS_PORT", defaultRedisPort)
	if err != nil {
		return Config{}, err
	}
	if err := validatePort("REDIS_PORT", redisPort); err != nil {
		return Config{}, err
	}

	redisDB, err := integerValue(lookup, "REDIS_DB", defaultRedisDB)
	if err != nil {
		return Config{}, err
	}
	if redisDB < 0 {
		return Config{}, errors.New("REDIS_DB must be a non-negative integer")
	}

	mysqlDatabase, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return Config{}, err
	}
	mysqlUser, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return Config{}, err
	}
	mysqlPassword, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	redisPassword, err := requiredValue(lookup, "REDIS_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	rabbitMQURL, err := requiredValue(lookup, "RABBITMQ_URL")
	if err != nil {
		return Config{}, err
	}
	if err := validateRabbitMQURL(rabbitMQURL); err != nil {
		return Config{}, err
	}

	return Config{
		AppEnv:   valueOrDefault(lookup, "APP_ENV", defaultAppEnv),
		HTTPHost: valueOrDefault(lookup, "HTTP_HOST", defaultHTTPHost),
		HTTPPort: httpPort,
		MySQL: MySQLConfig{
			Host:     valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost),
			Port:     mysqlPort,
			Database: mysqlDatabase,
			User:     mysqlUser,
			Password: mysqlPassword,
		},
		Redis: RedisConfig{
			Host:     valueOrDefault(lookup, "REDIS_HOST", defaultRedisHost),
			Port:     redisPort,
			Password: redisPassword,
			DB:       redisDB,
		},
		RabbitMQURL: rabbitMQURL,
	}, nil
}

func (cfg Config) HTTPAddress() string {
	return net.JoinHostPort(cfg.HTTPHost, strconv.Itoa(cfg.HTTPPort))
}

func valueOrDefault(lookup LookupFunc, key, fallback string) string {
	if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func requiredValue(lookup LookupFunc, key string) (string, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func integerValue(lookup LookupFunc, key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func validatePort(key string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", key)
	}
	return nil
}

func validateRabbitMQURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.New("RABBITMQ_URL must be a valid AMQP URL")
	}
	if parsed.Scheme != "amqp" && parsed.Scheme != "amqps" {
		return errors.New("RABBITMQ_URL must use the amqp or amqps scheme")
	}
	if parsed.Host == "" {
		return errors.New("RABBITMQ_URL must include a host")
	}
	return nil
}
