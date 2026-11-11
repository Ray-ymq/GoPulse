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

	"github.com/Ray-ymq/GoPulse/runtimekit"
)

const (
	defaultAppEnv                           = "development"
	defaultHTTPHost                         = "127.0.0.1"
	defaultHTTPPort                         = 8080
	defaultHTTPMaxConcurrency               = 128
	defaultPlatformHTTPMaxConcurrency       = 32
	defaultReplicaCount                     = 1
	defaultMySQLHost                        = "127.0.0.1"
	defaultMySQLPort                        = 3306
	defaultMySQLMaxOpenConns                = 10
	defaultPlatformMySQLMaxOpenConns        = 4
	defaultMySQLMaxIdleConns                = 2
	defaultMySQLConnMaxLifetime             = 3 * time.Minute
	defaultMySQLTotalOpenConns              = 60
	defaultRedisHost                        = "127.0.0.1"
	defaultRedisPort                        = 6379
	defaultRedisDB                          = 0
	defaultElasticsearchURL                 = "http://127.0.0.1:9200"
	defaultElasticsearchTimeout             = 3 * time.Second
	defaultSearchReindexBatch               = 500
	defaultBusinessAsyncMaxOpenRequirements = 10000
	defaultAuthJWTTTL                       = 2 * time.Hour
	defaultAuthCookieName                   = "gopulse_session"
	defaultRedisPostDetailTTL               = 5 * time.Minute
	defaultRedisOperationTimeout            = 200 * time.Millisecond
	defaultRedisPoolSize                    = 16
	defaultRateLimitLocalMaxIdentities      = 1024
	defaultResourceAuth                     = 16
	defaultResourceRead                     = 64
	defaultResourceWrite                    = 32
	defaultResourceDatabase                 = 32
	defaultResourceRebuild                  = 8
	defaultResourceWaiter                   = 256
	defaultOutboxPollInterval               = time.Second
	defaultOutboxClaimBatch                 = 50
	defaultOutboxLeaseDuration              = 5 * time.Minute
	defaultOutboxPublishTimeout             = 5 * time.Second
	defaultOutboxRetryDelay                 = 30 * time.Second
	defaultOutboxCleanupInterval            = time.Hour
	defaultOutboxRetention                  = 7 * 24 * time.Hour
	defaultOutboxCleanupBatch               = 500
	outboxLeaseSafetyMargin                 = time.Second
	minimumJWTSecretBytes                   = 32
	minimumAuthJWTTTL                       = 5 * time.Minute
	maximumAuthJWTTTL                       = 24 * time.Hour
	minimumRedisPostDetailTTL               = time.Second
	maximumRedisPostDetailTTL               = 24 * time.Hour
	minimumRedisOperationTimeout            = 10 * time.Millisecond
	maximumRedisOperationTimeout            = 5 * time.Second
	minimumRedisPoolSize                    = 1
	maximumRedisPoolSize                    = 256
	minimumRateLimitLocalMaxIdentities      = 1
	maximumRateLimitLocalMaxIdentities      = 100000
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
	minimumElasticsearchTimeout             = 100 * time.Millisecond
	maximumElasticsearchTimeout             = 30 * time.Second
	minimumSearchReindexBatch               = 1
	maximumSearchReindexBatch               = 5000
	minimumBusinessAsyncMaxOpenRequirements = 1
	maximumBusinessAsyncMaxOpenRequirements = 10000
	minimumHTTPMaxConcurrency               = 1
	maximumHTTPMaxConcurrency               = 1024
	minimumReplicaCount                     = 1
	maximumReplicaCount                     = 8
	minimumMySQLMaxOpenConns                = 1
	maximumMySQLMaxOpenConns                = 100
	minimumMySQLConnLifetime                = time.Minute
	maximumMySQLConnLifetime                = time.Hour
	minimumMySQLTotalOpenConns              = 1
	maximumMySQLTotalOpenConns              = 1000
	minimumBusinessStageCredentialBytes     = 32
)

// LookupFunc makes configuration loading deterministic in tests without
// changing the process environment.
type LookupFunc func(string) (string, bool)

type Config struct {
	ServiceRole                      ServiceRole
	RuntimeMode                      RuntimeMode
	AppEnv                           string
	InstanceID                       string
	ReplicaCount                     int
	HTTPHost                         string
	HTTPPort                         int
	HTTPMaxConcurrency               int
	MySQL                            MySQLConfig
	Redis                            RedisConfig
	RedisDisabled                    bool
	RabbitMQURL                      string
	Outbox                           OutboxConfig
	Auth                             AuthConfig
	Elasticsearch                    ElasticsearchConfig
	Trace                            TraceConfig
	BusinessAsyncMaxOpenRequirements uint64
	BusinessAsyncMaxRetainedGroups   uint64
	BusinessAsyncMaxRetainedBytes    uint64
	Protection                       ProtectionConfig
}

type MySQLConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Host             string
	Port             int
	Password         string
	DB               int
	PoolSize         int
	PoolTimeout      time.Duration
	PostDetailTTL    time.Duration
	OperationTimeout time.Duration
}

// ProtectionConfig is deliberately static. It is loaded once with the
// process configuration and is never consulted per request.
type ProtectionConfig struct {
	RateLimitEnabled          bool
	RateLimitOperationTimeout time.Duration
	LocalMaxIdentities        int
	ResourceAuth              int
	ResourceRead              int
	ResourceWrite             int
	ResourceDatabase          int
	ResourceRebuild           int
	ResourceWaiter            int
	TrustedProxyCIDRs         []string
}

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

type ElasticsearchConfig struct {
	URL            string
	RequestTimeout time.Duration
	ReindexBatch   int
	Purpose        string
}

type ReindexConfig struct {
	MySQL                            MySQLConfig
	Elasticsearch                    ElasticsearchConfig
	BusinessAsyncMaxOpenRequirements uint64
	BusinessAsyncMaxRetainedGroups   uint64
	BusinessAsyncMaxRetainedBytes    uint64
}

type TraceConfig struct {
	Enabled         bool
	Endpoint        string
	ServiceName     string
	SampleRatio     float64
	QueueCapacity   int
	BatchSize       int
	BatchTimeout    time.Duration
	ExportTimeout   time.Duration
	ShutdownTimeout time.Duration
}

type AuthConfig struct {
	JWTSecret    string
	JWTTTL       time.Duration
	CookieName   string
	CookieSecure bool
}

func Load() (Config, error) {
	if _, err := LoadServiceRoleFrom(os.LookupEnv); err != nil {
		return Config{}, err
	}
	if err := runtimekit.ValidateRuntimeEnvironment("backend"); err != nil {
		return Config{}, err
	}

	return LoadFrom(os.LookupEnv)
}

func LoadFrom(lookup LookupFunc) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("configuration lookup is required")
	}

	serviceRole, err := LoadServiceRoleFrom(lookup)
	if err != nil {
		return Config{}, err
	}
	runtimeMode, err := loadRuntimeMode(lookup)
	if err != nil {
		return Config{}, err
	}
	appEnv, err := applicationEnvironment(lookup)
	if err != nil {
		return Config{}, err
	}
	instanceID := valueOrDefault(lookup, "GOPULSE_INSTANCE_ID", "backend-local")
	if err := runtimekit.ValidateInstanceID(instanceID); err != nil {
		return Config{}, errors.New("GOPULSE_INSTANCE_ID must be a bounded lowercase identity")
	}
	replicaCount, err := replicaCountValue(lookup)
	if err != nil {
		return Config{}, err
	}
	httpMaxConcurrency, err := httpMaxConcurrencyValue(lookup, serviceRole)
	if err != nil || httpMaxConcurrency < minimumHTTPMaxConcurrency || httpMaxConcurrency > maximumHTTPMaxConcurrency {
		return Config{}, fmt.Errorf("%s must be between %d and %d", httpMaxConcurrencyKey(serviceRole), minimumHTTPMaxConcurrency, maximumHTTPMaxConcurrency)
	}
	mysqlMaxOpenConns, mysqlMaxIdleConns, mysqlConnMaxLifetime, err := mysqlPoolValuesForRole(lookup, replicaCount, serviceRole)
	if err != nil {
		return Config{}, err
	}
	businessAsyncMaxOpenRequirements, err := businessAsyncMaxOpenRequirementsValue(lookup)
	if err != nil {
		return Config{}, err
	}
	retainedGroups, retainedBytes, err := businessAsyncRetentionValues(lookup)
	if err != nil {
		return Config{}, err
	}
	httpHost := valueOrDefault(lookup, "HTTP_HOST", defaultHTTPHost)
	if err := validateListenHost(runtimeMode, "HTTP_HOST", httpHost); err != nil {
		return Config{}, err
	}
	mysqlHost := valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost)
	if err := validateDependencyHost(runtimeMode, "MYSQL_HOST", mysqlHost); err != nil {
		return Config{}, err
	}
	redisHost := ""
	redisPort := 0
	redisDB := 0
	redisPassword := ""
	redisPoolSize := 0
	redisPoolTimeout := time.Duration(0)
	postDetailTTL := defaultRedisPostDetailTTL
	operationTimeout := defaultRedisOperationTimeout
	redisDisabled, err := booleanValue(lookup, "BACKEND_REDIS_DISABLED", false)
	if err != nil {
		return Config{}, err
	}
	protection := ProtectionConfig{}
	if serviceRole.RunsBusiness() && !redisDisabled {
		redisHost = valueOrDefault(lookup, "REDIS_HOST", defaultRedisHost)
		if err := validateDependencyHost(runtimeMode, "REDIS_HOST", redisHost); err != nil {
			return Config{}, err
		}
		redisPort, err = integerValue(lookup, "REDIS_PORT", defaultRedisPort)
		if err != nil {
			return Config{}, err
		}
		if err := validatePort("REDIS_PORT", redisPort); err != nil {
			return Config{}, err
		}
		redisDB, err = integerValue(lookup, "REDIS_DB", defaultRedisDB)
		if err != nil {
			return Config{}, err
		}
		if redisDB < 0 {
			return Config{}, errors.New("REDIS_DB must be a non-negative integer")
		}
		redisPassword, err = requiredValue(lookup, "REDIS_PASSWORD")
		if err != nil {
			return Config{}, err
		}
		postDetailTTL, err = durationValue(lookup, "REDIS_POST_DETAIL_TTL", defaultRedisPostDetailTTL, minimumRedisPostDetailTTL, maximumRedisPostDetailTTL)
		if err != nil {
			return Config{}, err
		}
		operationTimeout, err = durationValue(lookup, "REDIS_OPERATION_TIMEOUT", defaultRedisOperationTimeout, minimumRedisOperationTimeout, maximumRedisOperationTimeout)
		if err != nil {
			return Config{}, err
		}
		redisPoolSize, err = integerValue(lookup, "REDIS_POOL_SIZE", defaultRedisPoolSize)
		if err != nil || redisPoolSize < minimumRedisPoolSize || redisPoolSize > maximumRedisPoolSize {
			return Config{}, fmt.Errorf("REDIS_POOL_SIZE must be between %d and %d", minimumRedisPoolSize, maximumRedisPoolSize)
		}
		redisPoolTimeout, err = durationValue(lookup, "REDIS_POOL_TIMEOUT", operationTimeout, minimumRedisOperationTimeout, maximumRedisOperationTimeout)
		if err != nil {
			return Config{}, err
		}
	}
	if serviceRole.RunsBusiness() {
		protection, err = loadProtectionConfig(lookup, operationTimeout)
		if err != nil {
			return Config{}, err
		}
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

	var elasticsearch ElasticsearchConfig
	if serviceRole.RunsBusiness() {
		elasticsearch, err = loadElasticsearchConfig(lookup, runtimeMode)
		if err != nil {
			return Config{}, err
		}
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
	rabbitMQURL := ""
	if serviceRole.RunsBusiness() {
		rabbitMQURL, err = requiredValue(lookup, "RABBITMQ_URL")
		if err != nil {
			return Config{}, err
		}
		if err := validateRabbitMQURL(rabbitMQURL, runtimeMode); err != nil {
			return Config{}, err
		}
	}

	traceConfig, err := loadTraceConfig(lookup, runtimeMode, "backend")
	if err != nil {
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

	outboxConfig := OutboxConfig{}
	if serviceRole.RunsBusiness() {
		outboxPollInterval, outboxErr := durationValue(lookup, "OUTBOX_POLL_INTERVAL", defaultOutboxPollInterval, minimumOutboxPollInterval, maximumOutboxPollInterval)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		outboxClaimBatch, outboxErr := integerValue(lookup, "OUTBOX_CLAIM_BATCH", defaultOutboxClaimBatch)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		if outboxClaimBatch < minimumOutboxClaimBatch || outboxClaimBatch > maximumOutboxClaimBatch {
			return Config{}, fmt.Errorf("OUTBOX_CLAIM_BATCH must be between %d and %d", minimumOutboxClaimBatch, maximumOutboxClaimBatch)
		}
		outboxLeaseDuration, outboxErr := durationValue(lookup, "OUTBOX_LEASE_DURATION", defaultOutboxLeaseDuration, minimumOutboxLeaseDuration, maximumOutboxLeaseDuration)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		outboxPublishTimeout, outboxErr := durationValue(lookup, "OUTBOX_PUBLISH_TIMEOUT", defaultOutboxPublishTimeout, minimumOutboxPublishTimeout, maximumOutboxPublishTimeout)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		dbTimeout, outboxErr := durationValue(lookup, "OUTBOX_DATABASE_TIMEOUT", 2*time.Second, 10*time.Millisecond, 30*time.Second)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		poolSize, outboxErr := integerValue(lookup, "OUTBOX_MYSQL_MAX_OPEN_CONNS", 4)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		if poolSize < 1 || poolSize > 32 {
			return Config{}, errors.New("OUTBOX_MYSQL_MAX_OPEN_CONNS must be between 1 and 32")
		}
		concurrency, outboxErr := integerValue(lookup, "OUTBOX_PUBLISH_CONCURRENCY", 8)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		if concurrency < 1 || concurrency > 32 {
			return Config{}, errors.New("OUTBOX_PUBLISH_CONCURRENCY must be between 1 and 32")
		}
		requiredOutboxLease := time.Duration((outboxClaimBatch+concurrency-1)/concurrency)*outboxPublishTimeout + time.Duration(outboxClaimBatch+2)*dbTimeout + outboxLeaseSafetyMargin
		if outboxLeaseDuration < requiredOutboxLease {
			return Config{}, fmt.Errorf("OUTBOX_LEASE_DURATION must be at least ceil(OUTBOX_CLAIM_BATCH / OUTBOX_PUBLISH_CONCURRENCY) * OUTBOX_PUBLISH_TIMEOUT + (OUTBOX_CLAIM_BATCH + 2) * OUTBOX_DATABASE_TIMEOUT + %s", outboxLeaseSafetyMargin)
		}
		outboxRetryDelay, outboxErr := durationValue(lookup, "OUTBOX_RETRY_DELAY", defaultOutboxRetryDelay, minimumOutboxRetryDelay, maximumOutboxRetryDelay)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		searchRetryDelay, outboxErr := durationValue(lookup, "SEARCH_INDEXER_RETRY_DELAY", defaultOutboxRetryDelay, minimumOutboxRetryDelay, maximumOutboxRetryDelay)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		outboxCleanupInterval, outboxErr := durationValue(lookup, "OUTBOX_CLEANUP_INTERVAL", defaultOutboxCleanupInterval, minimumOutboxCleanupInterval, maximumOutboxCleanupInterval)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		outboxRetention, outboxErr := durationValue(lookup, "OUTBOX_PUBLISHED_RETENTION", defaultOutboxRetention, minimumOutboxRetention, maximumOutboxRetention)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		outboxCleanupBatch, outboxErr := integerValue(lookup, "OUTBOX_CLEANUP_BATCH", defaultOutboxCleanupBatch)
		if outboxErr != nil {
			return Config{}, outboxErr
		}
		if outboxCleanupBatch < minimumOutboxCleanupBatch || outboxCleanupBatch > maximumOutboxCleanupBatch {
			return Config{}, fmt.Errorf("OUTBOX_CLEANUP_BATCH must be between %d and %d", minimumOutboxCleanupBatch, maximumOutboxCleanupBatch)
		}
		outboxConfig = OutboxConfig{
			DatabaseTimeout: dbTimeout, MaxOpenConns: poolSize, PublishConcurrency: concurrency,
			PollInterval: outboxPollInterval, ClaimBatch: outboxClaimBatch, LeaseDuration: outboxLeaseDuration,
			PublishTimeout: outboxPublishTimeout, RetryDelay: outboxRetryDelay, SearchRetryDelay: searchRetryDelay,
			CleanupInterval: outboxCleanupInterval, Retention: outboxRetention, CleanupBatch: outboxCleanupBatch,
		}
	}

	// Include dedicated background and readiness pools in the configured per-role
	// MySQL budget; API writes still use only mysqlMaxOpenConns.
	totalConnections, budgetErr := integerValue(lookup, "MYSQL_TOTAL_MAX_OPEN_CONNS", defaultMySQLTotalOpenConns)
	if budgetErr != nil {
		return Config{}, budgetErr
	}
	roleConnections := mysqlMaxOpenConns + 1
	if serviceRole.RunsBusiness() {
		roleConnections += outboxConfig.MaxOpenConns
	}
	if roleConnections*replicaCount > totalConnections {
		return Config{}, errors.New("MYSQL_TOTAL_MAX_OPEN_CONNS must cover API, outbox and probe pools times GOPULSE_REPLICA_COUNT")
	}

	return Config{
		ServiceRole:        serviceRole,
		RuntimeMode:        runtimeMode,
		AppEnv:             appEnv,
		InstanceID:         instanceID,
		ReplicaCount:       replicaCount,
		HTTPHost:           httpHost,
		HTTPPort:           httpPort,
		HTTPMaxConcurrency: httpMaxConcurrency,
		MySQL: MySQLConfig{
			Host:            mysqlHost,
			Port:            mysqlPort,
			Database:        mysqlDatabase,
			User:            mysqlUser,
			Password:        mysqlPassword,
			MaxOpenConns:    mysqlMaxOpenConns,
			MaxIdleConns:    mysqlMaxIdleConns,
			ConnMaxLifetime: mysqlConnMaxLifetime,
		},
		RedisDisabled: redisDisabled,
		Redis: RedisConfig{
			Host:             redisHost,
			Port:             redisPort,
			Password:         redisPassword,
			DB:               redisDB,
			PoolSize:         redisPoolSize,
			PoolTimeout:      redisPoolTimeout,
			PostDetailTTL:    postDetailTTL,
			OperationTimeout: operationTimeout,
		},
		RabbitMQURL:                      rabbitMQURL,
		Outbox:                           outboxConfig,
		Elasticsearch:                    elasticsearch,
		Trace:                            traceConfig,
		BusinessAsyncMaxOpenRequirements: businessAsyncMaxOpenRequirements,
		BusinessAsyncMaxRetainedGroups:   retainedGroups,
		BusinessAsyncMaxRetainedBytes:    retainedBytes,
		Protection:                       protection,
		Auth: AuthConfig{
			JWTSecret:    authJWTSecret,
			JWTTTL:       authJWTTTL,
			CookieName:   authCookieName,
			CookieSecure: authCookieSecure,
		},
	}, nil
}

func loadProtectionConfig(lookup LookupFunc, redisTimeout time.Duration) (ProtectionConfig, error) {
	protection := ProtectionConfig{}
	enabled, err := booleanValue(lookup, "BACKEND_RATE_LIMIT_ENABLED", true)
	if err != nil {
		return ProtectionConfig{}, err
	}
	operationTimeout, err := durationValue(lookup, "BACKEND_RATE_LIMIT_OPERATION_TIMEOUT", redisTimeout, minimumRedisOperationTimeout, maximumRedisOperationTimeout)
	if err != nil {
		return ProtectionConfig{}, err
	}
	localMax, err := integerValue(lookup, "BACKEND_RATE_LIMIT_LOCAL_MAX_IDENTITIES", defaultRateLimitLocalMaxIdentities)
	if err != nil || localMax < minimumRateLimitLocalMaxIdentities || localMax > maximumRateLimitLocalMaxIdentities {
		return ProtectionConfig{}, fmt.Errorf("BACKEND_RATE_LIMIT_LOCAL_MAX_IDENTITIES must be between %d and %d", minimumRateLimitLocalMaxIdentities, maximumRateLimitLocalMaxIdentities)
	}
	values := []struct {
		key      string
		fallback int
		assign   func(int)
	}{
		{"BACKEND_RESOURCE_AUTH", defaultResourceAuth, func(value int) { protection.ResourceAuth = value }},
		{"BACKEND_RESOURCE_READ", defaultResourceRead, func(value int) { protection.ResourceRead = value }},
		{"BACKEND_RESOURCE_WRITE", defaultResourceWrite, func(value int) { protection.ResourceWrite = value }},
		{"BACKEND_RESOURCE_DATABASE", defaultResourceDatabase, func(value int) { protection.ResourceDatabase = value }},
		{"BACKEND_RESOURCE_REBUILD", defaultResourceRebuild, func(value int) { protection.ResourceRebuild = value }},
		{"BACKEND_RESOURCE_WAITER", defaultResourceWaiter, func(value int) { protection.ResourceWaiter = value }},
	}
	for _, item := range values {
		value, valueErr := integerValue(lookup, item.key, item.fallback)
		if valueErr != nil || value < 1 || value > maximumHTTPMaxConcurrency*4 {
			return ProtectionConfig{}, fmt.Errorf("%s must be between 1 and %d", item.key, maximumHTTPMaxConcurrency*4)
		}
		item.assign(value)
	}
	protection.RateLimitEnabled = enabled
	protection.RateLimitOperationTimeout = operationTimeout
	protection.LocalMaxIdentities = localMax
	rawProxies := valueOrDefault(lookup, "BACKEND_TRUSTED_PROXY_CIDRS", "")
	if rawProxies != "" {
		for _, raw := range strings.Split(rawProxies, ",") {
			cidr := strings.TrimSpace(raw)
			if cidr == "" {
				return ProtectionConfig{}, errors.New("BACKEND_TRUSTED_PROXY_CIDRS contains an empty entry")
			}
			if _, _, parseErr := net.ParseCIDR(cidr); parseErr != nil {
				return ProtectionConfig{}, errors.New("BACKEND_TRUSTED_PROXY_CIDRS must contain CIDR networks")
			}
			protection.TrustedProxyCIDRs = append(protection.TrustedProxyCIDRs, cidr)
		}
	}
	return protection, nil
}

func businessAsyncMaxOpenRequirementsValue(lookup LookupFunc) (uint64, error) {
	value, err := integerValue(lookup, "BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS", defaultBusinessAsyncMaxOpenRequirements)
	if err != nil || value < minimumBusinessAsyncMaxOpenRequirements || value > maximumBusinessAsyncMaxOpenRequirements {
		return 0, fmt.Errorf("BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS must be an integer between %d and %d", minimumBusinessAsyncMaxOpenRequirements, maximumBusinessAsyncMaxOpenRequirements)
	}
	return uint64(value), nil
}

func (cfg Config) HTTPAddress() string {
	return net.JoinHostPort(cfg.HTTPHost, strconv.Itoa(cfg.HTTPPort))
}

func httpMaxConcurrencyKey(role ServiceRole) string {

	return "BACKEND_HTTP_MAX_CONCURRENCY"
}

func httpMaxConcurrencyValue(lookup LookupFunc, role ServiceRole) (int, error) {

	return integerValue(lookup, httpMaxConcurrencyKey(role), defaultHTTPMaxConcurrency)
}

func loadTraceConfig(lookup LookupFunc, runtimeMode RuntimeMode, serviceName string) (TraceConfig, error) {
	enabled, err := booleanValue(lookup, "GOPULSE_TRACE_ENABLED", false)
	if err != nil {
		return TraceConfig{}, err
	}
	configuredService := valueOrDefault(lookup, "GOPULSE_TRACE_SERVICE_NAME", serviceName)
	if configuredService != serviceName || configuredService == "" || len(configuredService) > 64 || strings.ContainsAny(configuredService, "\r\n\t /\\") {
		return TraceConfig{}, fmt.Errorf("GOPULSE_TRACE_SERVICE_NAME must be %s", serviceName)
	}
	ratioRaw := valueOrDefault(lookup, "GOPULSE_TRACE_SAMPLE_RATIO", "0.10")
	ratio, err := strconv.ParseFloat(strings.TrimSpace(ratioRaw), 64)
	if err != nil || ratio <= 0 || ratio > 1 {
		return TraceConfig{}, errors.New("GOPULSE_TRACE_SAMPLE_RATIO must be greater than 0 and at most 1")
	}
	queueCapacity, err := integerValue(lookup, "GOPULSE_TRACE_QUEUE_CAPACITY", 2048)
	if err != nil || queueCapacity < 1 || queueCapacity > 2048 {
		return TraceConfig{}, errors.New("GOPULSE_TRACE_QUEUE_CAPACITY must be between 1 and 2048")
	}
	batchSize, err := integerValue(lookup, "GOPULSE_TRACE_BATCH_SIZE", 256)
	if err != nil || batchSize < 1 || batchSize > queueCapacity {
		return TraceConfig{}, errors.New("GOPULSE_TRACE_BATCH_SIZE must be between 1 and GOPULSE_TRACE_QUEUE_CAPACITY")
	}
	batchTimeout, err := durationValue(lookup, "GOPULSE_TRACE_BATCH_TIMEOUT", time.Second, time.Millisecond, 10*time.Second)
	if err != nil {
		return TraceConfig{}, err
	}
	exportTimeout, err := durationValue(lookup, "GOPULSE_TRACE_EXPORT_TIMEOUT", 2*time.Second, time.Millisecond, 10*time.Second)
	if err != nil {
		return TraceConfig{}, err
	}
	shutdownTimeout, err := durationValue(lookup, "GOPULSE_TRACE_SHUTDOWN_TIMEOUT", 5*time.Second, time.Millisecond, 30*time.Second)
	if err != nil {
		return TraceConfig{}, err
	}
	endpoint := strings.TrimSpace(valueOrDefault(lookup, "GOPULSE_TRACE_ENDPOINT", ""))
	if endpoint != "" {
		host, port, splitErr := net.SplitHostPort(endpoint)
		if splitErr != nil || host == "" {
			return TraceConfig{}, errors.New("GOPULSE_TRACE_ENDPOINT must be a host:port value")
		}
		if err := validateDependencyHost(runtimeMode, "GOPULSE_TRACE_ENDPOINT", host); err != nil {
			return TraceConfig{}, err
		}
		portNumber, portErr := strconv.Atoi(port)
		if portErr != nil {
			return TraceConfig{}, errors.New("GOPULSE_TRACE_ENDPOINT port must be between 1 and 65535")
		}
		if err := validatePort("GOPULSE_TRACE_ENDPOINT port", portNumber); err != nil {
			return TraceConfig{}, err
		}
	}
	if enabled && endpoint == "" {
		return TraceConfig{}, errors.New("GOPULSE_TRACE_ENDPOINT is required when tracing is enabled")
	}
	return TraceConfig{
		Enabled: enabled, Endpoint: endpoint, ServiceName: configuredService,
		SampleRatio: ratio, QueueCapacity: queueCapacity, BatchSize: batchSize,
		BatchTimeout: batchTimeout, ExportTimeout: exportTimeout, ShutdownTimeout: shutdownTimeout,
	}, nil
}

func replicaCountValue(lookup LookupFunc) (int, error) {
	count, err := integerValue(lookup, "GOPULSE_REPLICA_COUNT", defaultReplicaCount)
	if err != nil || count < minimumReplicaCount || count > maximumReplicaCount {
		return 0, fmt.Errorf("GOPULSE_REPLICA_COUNT must be between %d and %d", minimumReplicaCount, maximumReplicaCount)
	}
	return count, nil
}

func mysqlPoolValues(lookup LookupFunc, replicaCount int) (int, int, time.Duration, error) {
	return mysqlPoolValuesWithKey(lookup, replicaCount, "MYSQL_MAX_OPEN_CONNS", defaultMySQLMaxOpenConns)
}

func mysqlPoolValuesForRole(lookup LookupFunc, replicaCount int, role ServiceRole) (int, int, time.Duration, error) {

	return mysqlPoolValues(lookup, replicaCount)
}

func mysqlPoolValuesWithKey(lookup LookupFunc, replicaCount int, maxOpenKey string, maxOpenDefault int) (int, int, time.Duration, error) {
	maxOpen, err := integerValue(lookup, maxOpenKey, maxOpenDefault)
	if err != nil || maxOpen < minimumMySQLMaxOpenConns || maxOpen > maximumMySQLMaxOpenConns {
		return 0, 0, 0, fmt.Errorf("%s must be between %d and %d", maxOpenKey, minimumMySQLMaxOpenConns, maximumMySQLMaxOpenConns)
	}
	maxIdle, err := integerValue(lookup, "MYSQL_MAX_IDLE_CONNS", defaultMySQLMaxIdleConns)
	if err != nil || maxIdle < 1 || maxIdle > maxOpen {
		return 0, 0, 0, errors.New("MYSQL_MAX_IDLE_CONNS must be between 1 and MYSQL_MAX_OPEN_CONNS")
	}
	lifetime, err := durationValue(lookup, "MYSQL_CONN_MAX_LIFETIME", defaultMySQLConnMaxLifetime, minimumMySQLConnLifetime, maximumMySQLConnLifetime)
	if err != nil {
		return 0, 0, 0, err
	}
	total, err := integerValue(lookup, "MYSQL_TOTAL_MAX_OPEN_CONNS", defaultMySQLTotalOpenConns)
	if err != nil || total < minimumMySQLTotalOpenConns || total > maximumMySQLTotalOpenConns {
		return 0, 0, 0, fmt.Errorf("MYSQL_TOTAL_MAX_OPEN_CONNS must be between %d and %d", minimumMySQLTotalOpenConns, maximumMySQLTotalOpenConns)
	}
	if maxOpen*replicaCount > total {
		return 0, 0, 0, errors.New("MYSQL_TOTAL_MAX_OPEN_CONNS must cover MYSQL_MAX_OPEN_CONNS times GOPULSE_REPLICA_COUNT")
	}
	return maxOpen, maxIdle, lifetime, nil
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

// LoadMySQL loads only the database settings required by role-isolated
// migration and administrator operations.
func LoadMySQL() (MySQLConfig, error) {
	return LoadMySQLFrom(os.LookupEnv)
}

func LoadMySQLFrom(lookup LookupFunc) (MySQLConfig, error) {
	if lookup == nil {
		return MySQLConfig{}, errors.New("configuration lookup is required")
	}
	runtimeMode, err := loadRuntimeMode(lookup)
	if err != nil {
		return MySQLConfig{}, err
	}
	host := valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost)
	if err := validateDependencyHost(runtimeMode, "MYSQL_HOST", host); err != nil {
		return MySQLConfig{}, err
	}
	port, err := integerValue(lookup, "MYSQL_PORT", defaultMySQLPort)
	if err != nil {
		return MySQLConfig{}, err
	}
	if err := validatePort("MYSQL_PORT", port); err != nil {
		return MySQLConfig{}, err
	}
	database, err := requiredValue(lookup, "MYSQL_DATABASE")
	if err != nil {
		return MySQLConfig{}, err
	}
	user, err := requiredValue(lookup, "MYSQL_USER")
	if err != nil {
		return MySQLConfig{}, err
	}
	password, err := requiredValue(lookup, "MYSQL_PASSWORD")
	if err != nil {
		return MySQLConfig{}, err
	}
	replicaCount, err := replicaCountValue(lookup)
	if err != nil {
		return MySQLConfig{}, err
	}
	maxOpenConns, maxIdleConns, connMaxLifetime, err := mysqlPoolValues(lookup, replicaCount)
	if err != nil {
		return MySQLConfig{}, err
	}
	return MySQLConfig{
		Host:            host,
		Port:            port,
		Database:        database,
		User:            user,
		Password:        password,
		MaxOpenConns:    maxOpenConns,
		MaxIdleConns:    maxIdleConns,
		ConnMaxLifetime: connMaxLifetime,
	}, nil
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
	runtimeMode, err := loadRuntimeMode(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	mysqlHost := valueOrDefault(lookup, "MYSQL_HOST", defaultMySQLHost)
	if err := validateDependencyHost(runtimeMode, "MYSQL_HOST", mysqlHost); err != nil {
		return ReindexConfig{}, err
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
	replicaCount, err := replicaCountValue(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	maxOpenConns, maxIdleConns, connMaxLifetime, err := mysqlPoolValues(lookup, replicaCount)
	if err != nil {
		return ReindexConfig{}, err
	}
	elasticsearch, err := loadElasticsearchConfig(lookup, runtimeMode)
	if err != nil {
		return ReindexConfig{}, err
	}
	businessAsyncMaxOpenRequirements, err := businessAsyncMaxOpenRequirementsValue(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	retainedGroups, retainedBytes, err := businessAsyncRetentionValues(lookup)
	if err != nil {
		return ReindexConfig{}, err
	}
	return ReindexConfig{
		MySQL: MySQLConfig{
			Host:            mysqlHost,
			Port:            mysqlPort,
			Database:        database,
			User:            user,
			Password:        password,
			MaxOpenConns:    maxOpenConns,
			MaxIdleConns:    maxIdleConns,
			ConnMaxLifetime: connMaxLifetime,
		},
		Elasticsearch:                    elasticsearch,
		BusinessAsyncMaxOpenRequirements: businessAsyncMaxOpenRequirements,
		BusinessAsyncMaxRetainedGroups:   retainedGroups,
		BusinessAsyncMaxRetainedBytes:    retainedBytes,
	}, nil
}

func loadElasticsearchConfig(lookup LookupFunc, runtimeMode RuntimeMode) (ElasticsearchConfig, error) {
	rawURL := valueOrDefault(lookup, "ELASTICSEARCH_URL", defaultElasticsearchURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must be an HTTP(S) origin without userinfo")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return ElasticsearchConfig{}, errors.New("ELASTICSEARCH_URL must not include a query or fragment")
	}
	if err := validateOriginHost(runtimeMode, "ELASTICSEARCH_URL", parsed); err != nil {
		return ElasticsearchConfig{}, err
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
	return ElasticsearchConfig{URL: strings.TrimRight(rawURL, "/"), RequestTimeout: timeout, ReindexBatch: batch, Purpose: "search"}, nil
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
