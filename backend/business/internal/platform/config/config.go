package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServiceRole        ServiceRole
	RuntimeMode        RuntimeMode
	AppEnv             string
	InstanceID         string
	ReplicaCount       int
	HTTPHost           string
	HTTPPort           int
	HTTPMaxConcurrency int
	MySQL              MySQLConfig
	Redis              RedisConfig
	RedisDisabled      bool
	RabbitMQURL        string
	Outbox             OutboxConfig
	Auth               AuthConfig
	Elasticsearch      ElasticsearchConfig

	BusinessAsyncMaxOpenRequirements uint64
	BusinessAsyncMaxRetainedGroups   uint64
	BusinessAsyncMaxRetainedBytes    uint64
	Protection                       ProtectionConfig
}

func Load() (Config, error) {
	if _, err := LoadServiceRoleFrom(os.LookupEnv); err != nil {
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
	if err := ValidateInstanceID(instanceID); err != nil {
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
		RabbitMQURL:   rabbitMQURL,
		Outbox:        outboxConfig,
		Elasticsearch: elasticsearch,

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

func (cfg Config) HTTPAddress() string {
	return net.JoinHostPort(cfg.HTTPHost, strconv.Itoa(cfg.HTTPPort))
}
