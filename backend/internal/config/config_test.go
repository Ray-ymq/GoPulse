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
	if cfg.HTTPHost != "127.0.0.1" || cfg.HTTPPort != 8080 {
		t.Fatalf("HTTP endpoint = %s:%d, want 127.0.0.1:8080", cfg.HTTPHost, cfg.HTTPPort)
	}
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 {
		t.Fatalf("MySQL endpoint = %s:%d, want 127.0.0.1:3306", cfg.MySQL.Host, cfg.MySQL.Port)
	}
	if cfg.Redis.Host != "127.0.0.1" || cfg.Redis.Port != 6379 || cfg.Redis.DB != 0 {
		t.Fatalf("Redis config = %#v, want default endpoint and DB", cfg.Redis)
	}
	if cfg.Auth.JWTTTL != 2*time.Hour || cfg.Auth.CookieName != "gopulse_session" || cfg.Auth.CookieSecure {
		t.Fatalf("Auth config = %#v, want local defaults", cfg.Auth)
	}
	if cfg.Redis.PostDetailTTL != 5*time.Minute || cfg.Redis.OperationTimeout != 200*time.Millisecond {
		t.Fatalf("Redis durations = %#v, want defaults", cfg.Redis)
	}
	if cfg.Outbox.PollInterval != time.Second || cfg.Outbox.ClaimBatch != 10 ||
		cfg.Outbox.LeaseDuration != 30*time.Second || cfg.Outbox.PublishTimeout != 5*time.Second ||
		cfg.Outbox.RetryDelay != 30*time.Second {
		t.Fatalf("Outbox config = %#v, want defaults", cfg.Outbox)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	env := requiredEnvironment()
	env["APP_ENV"] = "test"
	env["HTTP_HOST"] = "127.0.0.1"
	env["HTTP_PORT"] = "18080"
	env["MYSQL_HOST"] = "mysql.internal"
	env["MYSQL_PORT"] = "13306"
	env["REDIS_HOST"] = "redis.internal"
	env["REDIS_PORT"] = "16379"
	env["REDIS_DB"] = "3"
	env["AUTH_JWT_TTL"] = "30m"
	env["AUTH_COOKIE_NAME"] = "custom_session"
	env["AUTH_COOKIE_SECURE"] = "true"
	env["REDIS_POST_DETAIL_TTL"] = "10m"
	env["REDIS_OPERATION_TIMEOUT"] = "350ms"
	env["OUTBOX_POLL_INTERVAL"] = "250ms"
	env["OUTBOX_CLAIM_BATCH"] = "25"
	env["OUTBOX_LEASE_DURATION"] = "45s"
	env["OUTBOX_PUBLISH_TIMEOUT"] = "4s"
	env["OUTBOX_RETRY_DELAY"] = "2m"

	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.AppEnv != "test" || cfg.HTTPAddress() != "127.0.0.1:18080" {
		t.Fatalf("unexpected application config: %#v", cfg)
	}
	if cfg.MySQL.Host != "mysql.internal" || cfg.MySQL.Port != 13306 {
		t.Fatalf("unexpected MySQL config: %#v", cfg.MySQL)
	}
	if cfg.Redis.Host != "redis.internal" || cfg.Redis.Port != 16379 || cfg.Redis.DB != 3 {
		t.Fatalf("unexpected Redis config: %#v", cfg.Redis)
	}
	if cfg.Auth.JWTTTL != 30*time.Minute || cfg.Auth.CookieName != "custom_session" || !cfg.Auth.CookieSecure {
		t.Fatalf("unexpected auth config: %#v", cfg.Auth)
	}
	if cfg.Redis.PostDetailTTL != 10*time.Minute || cfg.Redis.OperationTimeout != 350*time.Millisecond {
		t.Fatalf("unexpected Redis duration config: %#v", cfg.Redis)
	}
	if cfg.Outbox.PollInterval != 250*time.Millisecond || cfg.Outbox.ClaimBatch != 25 ||
		cfg.Outbox.LeaseDuration != 45*time.Second || cfg.Outbox.PublishTimeout != 4*time.Second ||
		cfg.Outbox.RetryDelay != 2*time.Minute {
		t.Fatalf("unexpected Outbox config: %#v", cfg.Outbox)
	}
}

func TestLoadFromMissingRequiredValue(t *testing.T) {
	for _, key := range []string{"MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "REDIS_PASSWORD", "RABBITMQ_URL", "AUTH_JWT_SECRET"} {
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

func TestLoadFromRejectsOutboxPublishTimeoutThatCanOutliveLease(t *testing.T) {
	env := requiredEnvironment()
	env["OUTBOX_LEASE_DURATION"] = "5s"
	env["OUTBOX_PUBLISH_TIMEOUT"] = "5s"
	_, err := LoadFrom(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "OUTBOX_PUBLISH_TIMEOUT") {
		t.Fatalf("LoadFrom() error = %v, want publish/lease relationship error", err)
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
