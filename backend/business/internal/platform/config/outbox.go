package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBusinessAsyncMaxOpenRequirements = 10000
	defaultOutboxPollInterval               = time.Second
	defaultOutboxClaimBatch                 = 50
	defaultOutboxLeaseDuration              = 5 * time.Minute
	defaultOutboxPublishTimeout             = 5 * time.Second
	defaultOutboxRetryDelay                 = 30 * time.Second
	defaultOutboxCleanupInterval            = time.Hour
	defaultOutboxRetention                  = 7 * 24 * time.Hour
	defaultOutboxCleanupBatch               = 500
	outboxLeaseSafetyMargin                 = time.Second
	minimumOutboxPollInterval               = 10 * time.Millisecond
	maximumOutboxPollInterval               = time.Minute
	minimumOutboxClaimBatch                 = 1
	maximumOutboxClaimBatch                 = 50
	minimumOutboxLeaseDuration              = time.Second
	maximumOutboxLeaseDuration              = 10 * time.Minute
	minimumOutboxPublishTimeout             = 10 * time.Millisecond
	maximumOutboxPublishTimeout             = 30 * time.Second
	minimumOutboxRetryDelay                 = time.Second
	maximumOutboxRetryDelay                 = 24 * time.Hour
	minimumOutboxCleanupInterval            = time.Minute
	maximumOutboxCleanupInterval            = 24 * time.Hour
	minimumOutboxRetention                  = time.Hour
	maximumOutboxRetention                  = 365 * 24 * time.Hour
	minimumOutboxCleanupBatch               = 1
	maximumOutboxCleanupBatch               = 500
	minimumBusinessAsyncMaxOpenRequirements = 1
	maximumBusinessAsyncMaxOpenRequirements = 10000
)

type OutboxConfig struct {
	DatabaseTimeout    time.Duration
	MaxOpenConns       int
	PublishConcurrency int
	PollInterval       time.Duration
	ClaimBatch         int
	LeaseDuration      time.Duration
	PublishTimeout     time.Duration
	RetryDelay         time.Duration
	SearchRetryDelay   time.Duration
	CleanupInterval    time.Duration
	Retention          time.Duration
	CleanupBatch       int
}

func businessAsyncMaxOpenRequirementsValue(lookup LookupFunc) (uint64, error) {
	value, err := integerValue(lookup, "BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS", defaultBusinessAsyncMaxOpenRequirements)
	if err != nil || value < minimumBusinessAsyncMaxOpenRequirements || value > maximumBusinessAsyncMaxOpenRequirements {
		return 0, fmt.Errorf("BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS must be an integer between %d and %d", minimumBusinessAsyncMaxOpenRequirements, maximumBusinessAsyncMaxOpenRequirements)
	}
	return uint64(value), nil
}

func validateRabbitMQURL(rawURL string, runtimeMode RuntimeMode) error {
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
	if parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(rawURL, "\r\n") {
		return errors.New("RABBITMQ_URL must not include a query, fragment, or control characters")
	}
	return validateDependencyHost(runtimeMode, "RABBITMQ_URL", parsed.Hostname())
}

func businessAsyncRetentionValues(lookup LookupFunc) (uint64, uint64, error) {
	groups, err := integerValue(lookup, "BUSINESS_ASYNC_MAX_RETAINED_GROUPS", 100000)
	if err != nil || groups < 1 {
		return 0, 0, errors.New("BUSINESS_ASYNC_MAX_RETAINED_GROUPS must be a positive integer")
	}
	bytes, err := integerValue(lookup, "BUSINESS_ASYNC_MAX_RETAINED_BYTES", 1<<30)
	if err != nil || bytes <= 1024 {
		return 0, 0, errors.New("BUSINESS_ASYNC_MAX_RETAINED_BYTES must be an integer greater than 1024")
	}
	return uint64(groups), uint64(bytes), nil
}
