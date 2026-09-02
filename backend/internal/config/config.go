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
	defaultAppEnv                = "development"
	defaultHTTPHost              = "127.0.0.1"
	defaultHTTPPort              = 8080
	defaultMySQLHost             = "127.0.0.1"
	defaultMySQLPort             = 3306
	defaultRedisHost             = "127.0.0.1"
	defaultRedisPort             = 6379
	defaultRedisDB               = 0
	defaultElasticsearchURL      = "http://127.0.0.1:9200"
	defaultElasticsearchTimeout  = 3 * time.Second
	defaultSearchReindexBatch    = 500
	defaultAuthJWTTTL            = 2 * time.Hour
	defaultAuthCookieName        = "gopulse_session"
	defaultRedisPostDetailTTL    = 5 * time.Minute
	defaultRedisOperationTimeout = 200 * time.Millisecond
	defaultOutboxPollInterval    = time.Second
	defaultOutboxClaimBatch      = 10
	defaultOutboxLeaseDuration   = time.Minute
	defaultOutboxPublishTimeout  = 5 * time.Second
	defaultOutboxRetryDelay      = 30 * time.Second
	defaultOutboxCleanupInterval = time.Hour
	defaultOutboxRetention       = 7 * 24 * time.Hour
	defaultOutboxCleanupBatch    = 500
	outboxLeaseSafetyMargin      = time.Second
	minimumJWTSecretBytes        = 32
	minimumAuthJWTTTL            = 5 * time.Minute
	maximumAuthJWTTTL            = 24 * time.Hour
	minimumRedisPostDetailTTL    = time.Second
	maximumRedisPostDetailTTL    = 24 * time.Hour
	minimumRedisOperationTimeout = 10 * time.Millisecond
	maximumRedisOperationTimeout = 5 * time.Second
	minimumOutboxPollInterval    = 10 * time.Millisecond
	maximumOutboxPollInterval    = time.Minute
	minimumOutboxClaimBatch      = 1
	maximumOutboxClaimBatch      = 100
	minimumOutboxLeaseDuration   = time.Second
	maximumOutboxLeaseDuration   = 10 * time.Minute
	minimumOutboxPublishTimeout  = 10 * time.Millisecond
	maximumOutboxPublishTimeout  = 30 * time.Second
	minimumOutboxRetryDelay      = time.Second
	maximumOutboxRetryDelay      = 24 * time.Hour
	minimumOutboxCleanupInterval = time.Minute
	maximumOutboxCleanupInterval = 24 * time.Hour
	minimumOutboxRetention       = time.Hour
	maximumOutboxRetention       = 365 * 24 * time.Hour
	minimumOutboxCleanupBatch    = 1
	maximumOutboxCleanupBatch    = 1000
	minimumElasticsearchTimeout  = 100 * time.Millisecond
	maximumElasticsearchTimeout  = 30 * time.Second
	minimumSearchReindexBatch    = 1
	maximumSearchReindexBatch    = 5000
)

// LookupFunc makes configuration loading deterministic in tests without
// changing the process environment.
type LookupFunc func(string) (string, bool)

type Config struct {
	AppEnv        string
	HTTPHost      string
	HTTPPort      int
	MySQL         MySQLConfig
	Redis         RedisConfig
	RabbitMQURL   string
	Outbox        OutboxConfig
	Auth          AuthConfig
	Elasticsearch ElasticsearchConfig
}

type MySQLConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

type RedisConfig struct {
	Host             string
	Port             int
	Password         string
	DB               int
	PostDetailTTL    time.Duration
	OperationTimeout time.Duration
}

type OutboxConfig struct {
	PollInterval    time.Duration
	ClaimBatch      int
	LeaseDuration   time.Duration
	PublishTimeout  time.Duration
	RetryDelay      time.Duration
	CleanupInterval time.Duration
	Retention       time.Duration
	CleanupBatch    int
}

type ElasticsearchConfig struct {
	URL            string
	RequestTimeout time.Duration
	ReindexBatch   int
}

type ReindexConfig struct {
	MySQL         MySQLConfig
	Elasticsearch ElasticsearchConfig
}

type AuthConfig struct {
	JWTSecret    string
	JWTTTL       time.Duration
	CookieName   string
	CookieSecure bool
}

func Load() (Config, error) {
	return LoadFrom(os.LookupEnv)
}

func LoadFrom(lookup LookupFunc) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}

	appEnv, err := applicationEnvironment(lookup)
	if err != nil {
		return Config{}, err
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

	elasticsearch, err := loadElasticsearchConfig(lookup)
	if err != nil {
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

	authJWTSecret, err := requiredValue(lookup, "AUTH_JWT_SECRET")
	if err != nil {
		return Config{}, err
	}
	if len([]byte(authJWTSecret)) < minimumJWTSecretBytes {
		return Config{}, fmt.Errorf("AUTH_JWT_SECRET must be at least %d bytes", minimumJWTSecretBytes)
	}
	authJWTTTL, err := durationValue(lookup, "AUTH_JWT_TTL", defaultAuthJWTTTL, minimumAuthJWTTTL, maximumAuthJWTTTL)
	if err != nil {
		return Config{}, err
	}
	authCookieName := valueOrDefault(lookup, "AUTH_COOKIE_NAME", defaultAuthCookieName)
	if !isCookieName(authCookieName) {
		return Config{}, errors.New("AUTH_COOKIE_NAME must be a valid HTTP cookie name")
	}
	authCookieSecure, err := booleanValue(lookup, "AUTH_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	if !isLocalEnvironment(appEnv) && !authCookieSecure {
		return Config{}, errors.New("AUTH_COOKIE_SECURE must be true outside local development and test environments")
	}

	postDetailTTL, err := durationValue(lookup, "REDIS_POST_DETAIL_TTL", defaultRedisPostDetailTTL, minimumRedisPostDetailTTL, maximumRedisPostDetailTTL)
	if err != nil {
		return Config{}, err
	}
	operationTimeout, err := durationValue(lookup, "REDIS_OPERATION_TIMEOUT", defaultRedisOperationTimeout, minimumRedisOperationTimeout, maximumRedisOperationTimeout)
	if err != nil {
		return Config{}, err
	}

	outboxPollInterval, err := durationValue(lookup, "OUTBOX_POLL_INTERVAL", defaultOutboxPollInterval, minimumOutboxPollInterval, maximumOutboxPollInterval)
	if err != nil {
		return Config{}, err
	}
	outboxClaimBatch, err := integerValue(lookup, "OUTBOX_CLAIM_BATCH", defaultOutboxClaimBatch)
	if err != nil {
		return Config{}, err
	}
	if outboxClaimBatch < minimumOutboxClaimBatch || outboxClaimBatch > maximumOutboxClaimBatch {
		return Config{}, fmt.Errorf("OUTBOX_CLAIM_BATCH must be between %d and %d", minimumOutboxClaimBatch, maximumOutboxClaimBatch)
	}
	outboxLeaseDuration, err := durationValue(lookup, "OUTBOX_LEASE_DURATION", defaultOutboxLeaseDuration, minimumOutboxLeaseDuration, maximumOutboxLeaseDuration)
	if err != nil {
		return Config{}, err
	}
	outboxPublishTimeout, err := durationValue(lookup, "OUTBOX_PUBLISH_TIMEOUT", defaultOutboxPublishTimeout, minimumOutboxPublishTimeout, maximumOutboxPublishTimeout)
	if err != nil {
		return Config{}, err
	}
	requiredOutboxLease := time.Duration(outboxClaimBatch)*outboxPublishTimeout + outboxLeaseSafetyMargin
	if outboxLeaseDuration < requiredOutboxLease {
		return Config{}, fmt.Errorf("OUTBOX_LEASE_DURATION must be at least OUTBOX_CLAIM_BATCH * OUTBOX_PUBLISH_TIMEOUT + %s", outboxLeaseSafetyMargin)
	}
	outboxRetryDelay, err := durationValue(lookup, "OUTBOX_RETRY_DELAY", defaultOutboxRetryDelay, minimumOutboxRetryDelay, maximumOutboxRetryDelay)
	if err != nil {
		return Config{}, err
	}
	outboxCleanupInterval, err := durationValue(lookup, "OUTBOX_CLEANUP_INTERVAL", defaultOutboxCleanupInterval, minimumOutboxCleanupInterval, maximumOutboxCleanupInterval)
	if err != nil {
		return Config{}, err
	}
	outboxRetention, err := durationValue(lookup, "OUTBOX_PUBLISHED_RETENTION", defaultOutboxRetention, minimumOutboxRetention, maximumOutboxRetention)
	if err != nil {
		return Config{}, err
	}
	outboxCleanupBatch, err := integerValue(lookup, "OUTBOX_CLEANUP_BATCH", defaultOutboxCleanupBatch)
	if err != nil {
		return Config{}, err
	}
	if outboxCleanupBatch < minimumOutboxCleanupBatch || outboxCleanupBatch > maximumOutboxCleanupBatch {
		return Config{}, fmt.Errorf("OUTBOX_CLEANUP_BATCH must be between %d and %d", minimumOutboxCleanupBatch, maximumOutboxCleanupBatch)
	}

	return Config{
		AppEnv:   appEnv,
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
			Host:             valueOrDefault(lookup, "REDIS_HOST", defaultRedisHost),
			Port:             redisPort,
			Password:         redisPassword,
			DB:               redisDB,
			PostDetailTTL:    postDetailTTL,
			OperationTimeout: operationTimeout,
		},
		RabbitMQURL: rabbitMQURL,
		Outbox: OutboxConfig{
			PollInterval:    outboxPollInterval,
			ClaimBatch:      outboxClaimBatch,
			LeaseDuration:   outboxLeaseDuration,
			PublishTimeout:  outboxPublishTimeout,
			RetryDelay:      outboxRetryDelay,
			CleanupInterval: outboxCleanupInterval,
			Retention:       outboxRetention,
			CleanupBatch:    outboxCleanupBatch,
		},
		Elasticsearch: elasticsearch,
		Auth: AuthConfig{
			JWTSecret:    authJWTSecret,
			JWTTTL:       authJWTTTL,
			CookieName:   authCookieName,
			CookieSecure: authCookieSecure,
		},
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

func booleanValue(lookup LookupFunc, key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
}

func durationValue(lookup LookupFunc, key string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid Go duration", key)
	}
	if parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", key, minimum, maximum)
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

func applicationEnvironment(lookup LookupFunc) (string, error) {
	value := strings.ToLower(valueOrDefault(lookup, "APP_ENV", defaultAppEnv))
	switch value {
	case "development", "test", "production":
		return value, nil
	default:
		return "", errors.New("APP_ENV must be one of development, test, or production")
	}
}

func isLocalEnvironment(appEnv string) bool {
	return appEnv == "development" || appEnv == "test"
}

func isCookieName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		character := name[i]
		if character < 0x21 || character > 0x7e || strings.ContainsRune("()<>@,;:\\\"/[]?={} \t", rune(character)) {
			return false
		}
	}
	return true
}

// LoadReindex loads only the MySQL and Elasticsearch settings required by the
// role-isolated search rebuild command.
func LoadReindex() (ReindexConfig, error) {
	return LoadReindexFrom(os.LookupEnv)
}

func LoadReindexFrom(lookup LookupFunc) (ReindexConfig, error) {
	if lookup == nil {
		return ReindexConfig{}, errors.New("configuration lookup is required")
	}
	mysqlPort, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return ReindexConfig{}, err
	}
	if err := validatePort("MYSQL_PORT", mysqlPort); err != nil {
		return ReindexConfig{}, err
	}
	database, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return ReindexConfig{}, err
	}
	user, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return ReindexConfig{}, err
	}
	password, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return ReindexConfig{}, err
	}
	elasticsearch, err := loadElasticsearchConfig(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	return ReindexConfig{
		MySQL: MySQLConfig{
			Host: valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost), Port: mysqlPort,
			Database: database, User: user, Password: password,
		},
		Elasticsearch: elasticsearch,
	}, nil
}

func loadElasticsearchConfig(lookup LookupFunc) (ElasticsearchConfig, error) {
	rawURL := valueOrDefault(lookup, "ELASTICSEARCH_URL", defaultElasticsearchURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must be an HTTP(S) URL without userinfo")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must not include a query or fragment")
	}
	timeout, err := durationValue(lookup, "ELASTICSEARCH_REQUEST_TIMEOUT", defaultElasticsearchTimeout, minimumElasticsearchTimeout, maximumElasticsearchTimeout)
	if err != nil {
		return ElasticsearchConfig{}, err
	}
	batch, err := integerValue(lookup, "SEARCH_REINDEX_BATCH", defaultSearchReindexBatch)
	if err != nil {
		return ElasticsearchConfig{}, err
	}
	if batch < minimumSearchReindexBatch || batch > maximumSearchReindexBatch {
		return ElasticsearchConfig{}, fmt.Errorf("SEARCH_REINDEX_BATCH must be between %d and %d", minimumSearchReindexBatch, maximumSearchReindexBatch)
	}
	return ElasticsearchConfig{URL: strings.TrimRight(rawURL, "/"), RequestTimeout: timeout, ReindexBatch: batch}, nil
}
