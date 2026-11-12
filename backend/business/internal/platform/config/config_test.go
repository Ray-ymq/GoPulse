package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := LoadFrom(mapLookup(requiredEnvironment()))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.AppEnv != "development" {
		t.Fatalf("AppEnv = %q, want development", cfg.AppEnv)
	}
	if cfg.BusinessAsyncMaxOpenRequirements != 10000 {
		t.Fatalf("BusinessAsyncMaxOpenRequirements = %d, want 10000", cfg.BusinessAsyncMaxOpenRequirements)
	}
	if cfg.InstanceID != "backend-local" || cfg.ReplicaCount != 1 || cfg.HTTPMaxConcurrency != 128 {
		t.Fatalf("instance/concurrency config = %#v/%d/%d", cfg.InstanceID, cfg.ReplicaCount, cfg.HTTPMaxConcurrency)
	}
	if cfg.HTTPHost != "127.0.0.1" || cfg.HTTPPort != 8080 {
		t.Fatalf("HTTP endpoint = %s:%d, want 127.0.0.1:8080", cfg.HTTPHost, cfg.HTTPPort)
	}
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 {
		t.Fatalf("MySQL endpoint = %s:%d, want 127.0.0.1:3306", cfg.MySQL.Host, cfg.MySQL.Port)
	}
	if cfg.MySQL.MaxOpenConns != 10 || cfg.MySQL.MaxIdleConns != 2 || cfg.MySQL.ConnMaxLifetime != 3*time.Minute {
		t.Fatalf("MySQL pool = %#v, want bounded defaults", cfg.MySQL)
	}
	if cfg.Redis.Host != "127.0.0.1" || cfg.Redis.Port != 6379 || cfg.Redis.DB != 0 {
		t.Fatalf("Redis config = %#v, want default endpoint and DB", cfg.Redis)
	}

	if cfg.Elasticsearch.Purpose != "search" {
		t.Fatalf("Elasticsearch purpose = %q, want search", cfg.Elasticsearch.Purpose)
	}

	if cfg.Auth.JWTTTL != 2*time.Hour || cfg.Auth.CookieName != "gopulse_session" || cfg.Auth.CookieSecure {
		t.Fatalf("Auth config = %#v, want local defaults", cfg.Auth)
	}
	if cfg.Redis.PostDetailTTL != 5*time.Minute || cfg.Redis.OperationTimeout != 200*time.Millisecond || cfg.Redis.PoolSize != 16 || cfg.Redis.PoolTimeout != 200*time.Millisecond {
		t.Fatalf("Redis durations = %#v, want defaults", cfg.Redis)
	}
	if !cfg.Protection.RateLimitEnabled || cfg.Protection.RateLimitOperationTimeout != 200*time.Millisecond || cfg.Protection.LocalMaxIdentities != 1024 || cfg.Protection.ResourceAuth != 16 || cfg.Protection.ResourceRead != 64 || cfg.Protection.ResourceWrite != 32 || cfg.Protection.ResourceDatabase != 32 || cfg.Protection.ResourceRebuild != 8 || cfg.Protection.ResourceWaiter != 256 || len(cfg.Protection.TrustedProxyCIDRs) != 0 {
		t.Fatalf("Protection config = %#v, want bounded enabled defaults", cfg.Protection)
	}
	if cfg.Outbox.PollInterval != time.Second || cfg.Outbox.ClaimBatch != 50 ||
		cfg.Outbox.LeaseDuration != 5*time.Minute || cfg.Outbox.PublishTimeout != 5*time.Second ||
		cfg.Outbox.RetryDelay != 30*time.Second || cfg.Outbox.CleanupInterval != time.Hour ||
		cfg.Outbox.Retention != 7*24*time.Hour || cfg.Outbox.CleanupBatch != 500 {
		t.Fatalf("Outbox config = %#v, want defaults", cfg.Outbox)
	}

}

func TestLoadFromOverrides(t *testing.T) {
	env := requiredEnvironment()
	env["GOPULSE_RUNTIME_MODE"] = "container"
	env["GOPULSE_INSTANCE_ID"] = "backend-2"
	env["GOPULSE_REPLICA_COUNT"] = "2"
	env["BACKEND_HTTP_MAX_CONCURRENCY"] = "64"
	env["MYSQL_MAX_OPEN_CONNS"] = "8"
	env["MYSQL_MAX_IDLE_CONNS"] = "4"
	env["MYSQL_CONN_MAX_LIFETIME"] = "11m"
	env["MYSQL_TOTAL_MAX_OPEN_CONNS"] = "26"
	env["APP_ENV"] = "test"
	env["HTTP_HOST"] = "0.0.0.0"
	env["HTTP_PORT"] = "18080"
	env["MYSQL_HOST"] = "mysql"
	env["MYSQL_PORT"] = "13306"
	env["REDIS_HOST"] = "redis"
	env["REDIS_PORT"] = "16379"
	env["REDIS_DB"] = "3"
	env["AUTH_JWT_TTL"] = "30m"
	env["AUTH_COOKIE_NAME"] = "custom_session"
	env["AUTH_COOKIE_SECURE"] = "true"
	env["REDIS_POST_DETAIL_TTL"] = "10m"
	env["REDIS_OPERATION_TIMEOUT"] = "350ms"
	env["REDIS_POOL_SIZE"] = "24"
	env["REDIS_POOL_TIMEOUT"] = "450ms"
	env["BACKEND_RATE_LIMIT_ENABLED"] = "true"
	env["BACKEND_RATE_LIMIT_OPERATION_TIMEOUT"] = "500ms"
	env["BACKEND_RATE_LIMIT_LOCAL_MAX_IDENTITIES"] = "2048"
	env["BACKEND_RESOURCE_AUTH"] = "20"
	env["BACKEND_RESOURCE_READ"] = "80"
	env["BACKEND_RESOURCE_WRITE"] = "40"
	env["BACKEND_RESOURCE_DATABASE"] = "48"
	env["BACKEND_RESOURCE_REBUILD"] = "12"
	env["BACKEND_RESOURCE_WAITER"] = "300"
	env["BACKEND_TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32, 10.0.0.0/8"
	env["OUTBOX_POLL_INTERVAL"] = "250ms"
	env["OUTBOX_CLAIM_BATCH"] = "25"
	env["OUTBOX_LEASE_DURATION"] = "2m"
	env["OUTBOX_PUBLISH_TIMEOUT"] = "4s"
	env["OUTBOX_RETRY_DELAY"] = "2m"
	env["OUTBOX_CLEANUP_INTERVAL"] = "30m"
	env["OUTBOX_PUBLISHED_RETENTION"] = "720h"
	env["OUTBOX_CLEANUP_BATCH"] = "450"
	env["RABBITMQ_URL"] = "amqp://gopulse:rabbit-secret@rabbitmq:5672/"
	env["ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
	env["BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS"] = "8"

	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.RuntimeMode != RuntimeModeContainer || cfg.AppEnv != "test" || cfg.HTTPAddress() != "0.0.0.0:18080" || cfg.InstanceID != "backend-2" || cfg.ReplicaCount != 2 || cfg.HTTPMaxConcurrency != 64 {
		t.Fatalf("unexpected application config: %#v", cfg)
	}
	if cfg.BusinessAsyncMaxOpenRequirements != 8 {
		t.Fatalf("BusinessAsyncMaxOpenRequirements = %d, want 8", cfg.BusinessAsyncMaxOpenRequirements)
	}
	if cfg.MySQL.Host != "mysql" || cfg.MySQL.Port != 13306 {
		t.Fatalf("unexpected MySQL config: %#v", cfg.MySQL)
	}
	if cfg.Elasticsearch.URL != "http://elasticsearch:9200" || cfg.Elasticsearch.Purpose != "search" {
		t.Fatalf("unexpected Elasticsearch client: %#v", cfg.Elasticsearch)
	}
	if cfg.MySQL.MaxOpenConns != 8 || cfg.MySQL.MaxIdleConns != 4 || cfg.MySQL.ConnMaxLifetime != 11*time.Minute {
		t.Fatalf("unexpected MySQL pool config: %#v", cfg.MySQL)
	}
	if cfg.Redis.Host != "redis" || cfg.Redis.Port != 16379 || cfg.Redis.DB != 3 {
		t.Fatalf("unexpected Redis config: %#v", cfg.Redis)
	}

	if cfg.Auth.JWTTTL != 30*time.Minute || cfg.Auth.CookieName != "custom_session" || !cfg.Auth.CookieSecure {
		t.Fatalf("unexpected auth config: %#v", cfg.Auth)
	}
	if cfg.Redis.PostDetailTTL != 10*time.Minute || cfg.Redis.OperationTimeout != 350*time.Millisecond || cfg.Redis.PoolSize != 24 || cfg.Redis.PoolTimeout != 450*time.Millisecond {
		t.Fatalf("unexpected Redis duration config: %#v", cfg.Redis)
	}
	if !cfg.Protection.RateLimitEnabled || cfg.Protection.RateLimitOperationTimeout != 500*time.Millisecond || cfg.Protection.LocalMaxIdentities != 2048 || cfg.Protection.ResourceAuth != 20 || cfg.Protection.ResourceRead != 80 || cfg.Protection.ResourceWrite != 40 || cfg.Protection.ResourceDatabase != 48 || cfg.Protection.ResourceRebuild != 12 || cfg.Protection.ResourceWaiter != 300 || strings.Join(cfg.Protection.TrustedProxyCIDRs, ",") != "127.0.0.1/32,10.0.0.0/8" {
		t.Fatalf("unexpected protection config: %#v", cfg.Protection)
	}
	if cfg.Outbox.PollInterval != 250*time.Millisecond || cfg.Outbox.ClaimBatch != 25 ||
		cfg.Outbox.LeaseDuration != 2*time.Minute || cfg.Outbox.PublishTimeout != 4*time.Second ||
		cfg.Outbox.RetryDelay != 2*time.Minute || cfg.Outbox.CleanupInterval != 30*time.Minute ||
		cfg.Outbox.Retention != 720*time.Hour || cfg.Outbox.CleanupBatch != 450 {
		t.Fatalf("unexpected Outbox config: %#v", cfg.Outbox)
	}
}

func TestLoadFromRejectsInvalidBusinessAsyncRequirementLimit(t *testing.T) {
	for _, value := range []string{"not-an-int", "0", "10001"} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnvironment()
			env["BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS"] = value
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), "BUSINESS_ASYNC_MAX_OPEN_REQUIREMENTS") {
				t.Fatalf("LoadFrom() error = %v, want business async limit error", err)
			}
		})
	}
}

func TestLoadFromMissingRequiredValue(t *testing.T) {
	for _, key := range []string{"MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "RABBITMQ_URL", "AUTH_JWT_SECRET"} {
		t.Run(key, func(t *testing.T) {
			env := requiredEnvironment()
			delete(env, key)

			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadFrom() error = %v, want field name %s", err, key)
			}
		})
	}
}

func TestLoadFromRejectsInvalidPorts(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "HTTP_PORT", value: "invalid"},
		{key: "HTTP_PORT", value: "0"},
		{key: "MYSQL_PORT", value: "65536"},
		{key: "REDIS_PORT", value: "-1"},
	} {
		t.Run(test.key+"_"+test.value, func(t *testing.T) {
			env := requiredEnvironment()
			env[test.key] = test.value

			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want field name %s", err, test.key)
			}
		})
	}
}

func TestLoadFromRejectsInvalidReplicaAndPoolBudgets(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{key: "GOPULSE_INSTANCE_ID", value: "Backend-1"},
		{key: "GOPULSE_REPLICA_COUNT", value: "0"},
		{key: "GOPULSE_REPLICA_COUNT", value: "9"},
		{key: "BACKEND_HTTP_MAX_CONCURRENCY", value: "0"},
		{key: "BACKEND_HTTP_MAX_CONCURRENCY", value: "1025"},
		{key: "MYSQL_MAX_OPEN_CONNS", value: "0"},
		{key: "MYSQL_MAX_IDLE_CONNS", value: "11"},
		{key: "MYSQL_CONN_MAX_LIFETIME", value: "30s"},
		{key: "MYSQL_TOTAL_MAX_OPEN_CONNS", value: "1"},
	} {
		t.Run(test.key+"_"+test.value, func(t *testing.T) {
			env := requiredEnvironment()
			env[test.key] = test.value
			if _, err := LoadFrom(mapLookup(env)); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want %s error", err, test.key)
			}
		})
	}
	env := requiredEnvironment()
	env["GOPULSE_REPLICA_COUNT"] = "2"
	env["MYSQL_MAX_OPEN_CONNS"] = "10"
	env["MYSQL_TOTAL_MAX_OPEN_CONNS"] = "19"
	if _, err := LoadFrom(mapLookup(env)); err == nil || !strings.Contains(err.Error(), "MYSQL_TOTAL_MAX_OPEN_CONNS") {
		t.Fatalf("LoadFrom() error = %v, want total connection budget error", err)
	}
}

func TestLoadFromRejectsInvalidRedisDB(t *testing.T) {
	for _, value := range []string{"invalid", "-1"} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnvironment()
			env["REDIS_DB"] = value

			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), "REDIS_DB") {
				t.Fatalf("LoadFrom() error = %v, want REDIS_DB error", err)
			}
		})
	}
}

func TestLoadFromRejectsInvalidRabbitMQURLWithoutLeakingIt(t *testing.T) {
	secret := "do-not-leak"
	for _, value := range []string{
		"http://user:" + secret + "@localhost:5672/",
		"amqp://user:" + secret + "@/",
		"amqp://user:" + secret + "@%zz/",
	} {
		t.Run(value[:4], func(t *testing.T) {
			env := requiredEnvironment()
			env["RABBITMQ_URL"] = value

			_, err := LoadFrom(mapLookup(env))
			if err == nil {
				t.Fatal("LoadFrom() error = nil, want invalid RabbitMQ URL error")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), value) {
				t.Fatalf("error leaked sensitive URL: %v", err)
			}
		})
	}
}

func TestLoadFromRejectsInvalidAuthenticationConfigurationWithoutLeakingSecret(t *testing.T) {
	secret := "too-short"
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "short secret", key: "AUTH_JWT_SECRET", value: secret},
		{name: "short ttl", key: "AUTH_JWT_TTL", value: "1m"},
		{name: "long ttl", key: "AUTH_JWT_TTL", value: "25h"},
		{name: "invalid cookie name", key: "AUTH_COOKIE_NAME", value: "bad cookie"},
		{name: "invalid secure flag", key: "AUTH_COOKIE_SECURE", value: "sometimes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := requiredEnvironment()
			env[test.key] = test.value
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want %s error", err, test.key)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("configuration error leaked JWT secret: %v", err)
			}
		})
	}
}

func TestLoadFromRequiresSecureCookieOutsideLocalEnvironments(t *testing.T) {
	env := requiredEnvironment()
	env["APP_ENV"] = "production"
	env["AUTH_COOKIE_SECURE"] = "false"

	_, err := LoadFrom(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "AUTH_COOKIE_SECURE") {
		t.Fatalf("LoadFrom() error = %v, want secure cookie requirement", err)
	}

	env["AUTH_COOKIE_SECURE"] = "true"
	if _, err := LoadFrom(mapLookup(env)); err != nil {
		t.Fatalf("LoadFrom() with secure production cookie error = %v", err)
	}
}

func TestLoadFromRejectsInvalidRedisDurations(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "REDIS_POST_DETAIL_TTL", value: "0s"},
		{key: "REDIS_POST_DETAIL_TTL", value: "25h"},
		{key: "REDIS_OPERATION_TIMEOUT", value: "5ms"},
		{key: "REDIS_OPERATION_TIMEOUT", value: "6s"},
	} {
		t.Run(test.key+"_"+test.value, func(t *testing.T) {
			env := requiredEnvironment()
			env[test.key] = test.value
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want %s error", err, test.key)
			}
		})
	}
}

func TestLoadFromRejectsInvalidOutboxConfiguration(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{key: "OUTBOX_POLL_INTERVAL", value: "1ms"},
		{key: "OUTBOX_POLL_INTERVAL", value: "61m"},
		{key: "OUTBOX_CLAIM_BATCH", value: "0"},
		{key: "OUTBOX_CLAIM_BATCH", value: "101"},
		{key: "OUTBOX_CLAIM_BATCH", value: "not-an-int"},
		{key: "OUTBOX_LEASE_DURATION", value: "500ms"},
		{key: "OUTBOX_LEASE_DURATION", value: "11m"},
		{key: "OUTBOX_PUBLISH_TIMEOUT", value: "5ms"},
		{key: "OUTBOX_PUBLISH_TIMEOUT", value: "31s"},
		{key: "OUTBOX_RETRY_DELAY", value: "500ms"},
		{key: "OUTBOX_RETRY_DELAY", value: "25h"},
		{key: "OUTBOX_CLEANUP_INTERVAL", value: "30s"},
		{key: "OUTBOX_CLEANUP_INTERVAL", value: "25h"},
		{key: "OUTBOX_PUBLISHED_RETENTION", value: "30m"},
		{key: "OUTBOX_PUBLISHED_RETENTION", value: "8761h"},
		{key: "OUTBOX_CLEANUP_BATCH", value: "0"},
		{key: "OUTBOX_CLEANUP_BATCH", value: "1001"},
	}
	for _, test := range tests {
		t.Run(test.key+"_"+test.value, func(t *testing.T) {
			env := requiredEnvironment()
			env[test.key] = test.value
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want %s error", err, test.key)
			}
		})
	}
}

func TestLoadFromRejectsOutboxLeaseThatCannotCoverClaimBatch(t *testing.T) {
	env := requiredEnvironment()
	env["OUTBOX_CLAIM_BATCH"] = "10"
	env["OUTBOX_LEASE_DURATION"] = "30s"
	env["OUTBOX_PUBLISH_TIMEOUT"] = "5s"
	_, err := LoadFrom(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "OUTBOX_LEASE_DURATION") || !strings.Contains(err.Error(), "OUTBOX_CLAIM_BATCH") {
		t.Fatalf("LoadFrom() error = %v, want claim batch lease budget error", err)
	}
}

func requiredEnvironment() map[string]string {
	return map[string]string{
		"MYSQL_DATABASE":  "gopulse",
		"MYSQL_USER":      "gopulse",
		"MYSQL_PASSWORD":  "mysql-secret",
		"REDIS_PASSWORD":  "redis-secret",
		"RABBITMQ_URL":    "amqp://gopulse:rabbit-secret@127.0.0.1:5672/",
		"AUTH_JWT_SECRET": "local-development-jwt-secret-32-bytes-minimum",
	}
}

func mapLookup(values map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestLoadFromMapsSupportedApplicationEnvironments(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "development", want: "development"},
		{input: "TEST", want: "test"},
		{input: " production ", want: "production"},
	} {
		t.Run(test.input, func(t *testing.T) {
			env := requiredEnvironment()
			env["APP_ENV"] = test.input
			env["AUTH_COOKIE_SECURE"] = "true"
			cfg, err := LoadFrom(mapLookup(env))
			if err != nil {
				t.Fatalf("LoadFrom() error = %v", err)
			}
			if cfg.AppEnv != test.want {
				t.Fatalf("AppEnv = %q, want %q", cfg.AppEnv, test.want)
			}
		})
	}
}

func TestLoadFromRejectsUnsupportedApplicationEnvironment(t *testing.T) {
	for _, value := range []string{"local", "staging", "invalid"} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnvironment()
			env["APP_ENV"] = value
			env["AUTH_COOKIE_SECURE"] = "true"
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
				t.Fatalf("LoadFrom() error = %v, want APP_ENV error", err)
			}
		})
	}
}

func TestOutboxCapacitySettingsAndBatchBudget(t *testing.T) {
	for key, values := range map[string][]string{
		"OUTBOX_DATABASE_TIMEOUT":     {"0s", "31s"},
		"OUTBOX_MYSQL_MAX_OPEN_CONNS": {"0", "33"},
		"OUTBOX_PUBLISH_CONCURRENCY":  {"0", "33"},
		"OUTBOX_CLAIM_BATCH":          {"51"},
		"OUTBOX_CLEANUP_BATCH":        {"501"},
	} {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				env := requiredEnvironment()
				env[key] = value
				if _, err := LoadFrom(mapLookup(env)); err == nil {
					t.Fatal("invalid capacity value accepted")
				}
			})
		}
	}
}

func TestDedicatedPoolsCountTowardMySQLBudget(t *testing.T) {
	env := requiredEnvironment()
	env["MYSQL_MAX_OPEN_CONNS"] = "8"
	env["MYSQL_TOTAL_MAX_OPEN_CONNS"] = "12"
	if _, err := LoadFrom(mapLookup(env)); err == nil {
		t.Fatal("dedicated pools exceeded total budget")
	}
	env["MYSQL_TOTAL_MAX_OPEN_CONNS"] = "13"
	if _, err := LoadFrom(mapLookup(env)); err != nil {
		t.Fatal(err)
	}
}

func TestRedisDisabledDoesNotRequireOrParseRedisConnection(t *testing.T) {
	env := requiredEnvironment()
	env["BACKEND_REDIS_DISABLED"] = "true"
	delete(env, "REDIS_PASSWORD")
	env["REDIS_PORT"] = "invalid-unused-port"
	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RedisDisabled || cfg.Redis.Host != "" || !cfg.Protection.RateLimitEnabled || cfg.Protection.ResourceDatabase != 32 {
		t.Fatal("无 Redis 配置必须跳过 Redis 连接并保留应用保护")
	}
	env["BACKEND_REDIS_DISABLED"] = "invalid"
	if _, err := LoadFrom(mapLookup(env)); err == nil {
		t.Fatal("无效布尔值未被拒绝")
	}
}
