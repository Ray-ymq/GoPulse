package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	defaultWorkerPrefetch          = 10
	defaultWorkerMaxRetries        = 3
	defaultWorkerPublishTimeout    = 5 * time.Second
	defaultWorkerShutdownTimeout   = 10 * time.Second
	defaultWorkerReconnectMinimum  = 500 * time.Millisecond
	defaultWorkerReconnectMaximum  = 30 * time.Second
	minimumWorkerPrefetch          = 1
	maximumWorkerPrefetch          = 100
	minimumWorkerMaxRetries        = 0
	maximumWorkerMaxRetries        = 20
	minimumWorkerShutdownTimeout   = time.Second
	maximumWorkerShutdownTimeout   = 5 * time.Minute
	minimumWorkerReconnectDuration = 100 * time.Millisecond
	maximumWorkerReconnectDuration = 5 * time.Minute
)

type WorkerConfig struct {
	MySQL       MySQLConfig
	RabbitMQURL string
	Worker      BusinessWorkerConfig
}

type BusinessWorkerConfig struct {
	Prefetch         int
	MaxRetries       int
	RetryDelay       time.Duration
	PublishTimeout   time.Duration
	ShutdownTimeout  time.Duration
	ReconnectMinimum time.Duration
	ReconnectMaximum time.Duration
}

func LoadWorker() (WorkerConfig, error) {
	return LoadWorkerFrom(os.LookupEnv)
}

// LoadWorkerFrom intentionally reads only MySQL, RabbitMQ, and worker settings.
// It does not require HTTP, Redis, JWT, or Cookie configuration.
func LoadWorkerFrom(lookup LookupFunc) (WorkerConfig, error) {
	if lookup == nil {
		return WorkerConfig{}, errors.New("configuration lookup is required")
	}
	mysqlPort, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return WorkerConfig{}, err
	}
	if err := validatePort("MYSQL_PORT", mysqlPort); err != nil {
		return WorkerConfig{}, err
	}
	mysqlDatabase, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return WorkerConfig{}, err
	}
	mysqlUser, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return WorkerConfig{}, err
	}
	mysqlPassword, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return WorkerConfig{}, err
	}
	rabbitMQURL, err := requiredValue(lookup, "RABBITMQ_URL")
	if err != nil {
		return WorkerConfig{}, err
	}
	if err := validateRabbitMQURL(rabbitMQURL); err != nil {
		return WorkerConfig{}, err
	}
	prefetch, err := integerValue(lookup, "BUSINESS_WORKER_PREFETCH", defaultWorkerPrefetch)
	if err != nil || prefetch < minimumWorkerPrefetch || prefetch > maximumWorkerPrefetch {
		return WorkerConfig{}, fmt.Errorf("BUSINESS_WORKER_PREFETCH must be an integer between %d and %d", minimumWorkerPrefetch, maximumWorkerPrefetch)
	}
	maxRetries, err := integerValue(lookup, "BUSINESS_WORKER_MAX_RETRIES", defaultWorkerMaxRetries)
	if err != nil || maxRetries < minimumWorkerMaxRetries || maxRetries > maximumWorkerMaxRetries {
		return WorkerConfig{}, fmt.Errorf("BUSINESS_WORKER_MAX_RETRIES must be an integer between %d and %d", minimumWorkerMaxRetries, maximumWorkerMaxRetries)
	}
	retryDelay, err := durationValue(lookup, "OUTBOX_RETRY_DELAY", defaultOutboxRetryDelay, minimumOutboxRetryDelay, maximumOutboxRetryDelay)
	if err != nil {
		return WorkerConfig{}, err
	}
	publishTimeout, err := durationValue(lookup, "BUSINESS_WORKER_PUBLISH_TIMEOUT", defaultWorkerPublishTimeout, minimumOutboxPublishTimeout, maximumOutboxPublishTimeout)
	if err != nil {
		return WorkerConfig{}, err
	}
	shutdownTimeout, err := durationValue(lookup, "BUSINESS_WORKER_SHUTDOWN_TIMEOUT", defaultWorkerShutdownTimeout, minimumWorkerShutdownTimeout, maximumWorkerShutdownTimeout)
	if err != nil {
		return WorkerConfig{}, err
	}
	reconnectMinimum, err := durationValue(lookup, "BUSINESS_WORKER_RECONNECT_MIN", defaultWorkerReconnectMinimum, minimumWorkerReconnectDuration, maximumWorkerReconnectDuration)
	if err != nil {
		return WorkerConfig{}, err
	}
	reconnectMaximum, err := durationValue(lookup, "BUSINESS_WORKER_RECONNECT_MAX", defaultWorkerReconnectMaximum, minimumWorkerReconnectDuration, maximumWorkerReconnectDuration)
	if err != nil {
		return WorkerConfig{}, err
	}
	if reconnectMaximum < reconnectMinimum {
		return WorkerConfig{}, errors.New("BUSINESS_WORKER_RECONNECT_MAX must be greater than or equal to BUSINESS_WORKER_RECONNECT_MIN")
	}
	return WorkerConfig{
		MySQL: MySQLConfig{
			Host: valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost), Port: mysqlPort,
			Database: mysqlDatabase, User: mysqlUser, Password: mysqlPassword,
		},
		RabbitMQURL: rabbitMQURL,
		Worker: BusinessWorkerConfig{
			Prefetch: prefetch, MaxRetries: maxRetries, RetryDelay: retryDelay,
			PublishTimeout: publishTimeout, ShutdownTimeout: shutdownTimeout,
			ReconnectMinimum: reconnectMinimum, ReconnectMaximum: reconnectMaximum,
		},
	}, nil
}
