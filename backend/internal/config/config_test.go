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
	if cfg.Monitor.RequestTimeout != 30*time.Second {
		t.Fatalf("Monitor timeout = %s, want 30s", cfg.Monitor.RequestTimeout)
	}
	if cfg.Elasticsearch.Purpose != "search" {
		t.Fatalf("Elasticsearch purpose = %q, want search", cfg.Elasticsearch.Purpose)
	}
	if cfg.ObservabilityElasticsearch.URL != "http://127.0.0.1:9201" || cfg.ObservabilityElasticsearch.Purpose != "observability" {
		t.Fatalf("Observability Elasticsearch = %#v, want loopback observation client", cfg.ObservabilityElasticsearch)
	}
	if cfg.Auth.JWTTTL != 2*time.Hour || cfg.Auth.CookieName != "gopulse_session" || cfg.Auth.CookieSecure {
		t.Fatalf("Auth config = %#v, want local defaults", cfg.Auth)
	}
	if cfg.Redis.PostDetailTTL != 5*time.Minute || cfg.Redis.OperationTimeout != 200*time.Millisecond {
		t.Fatalf("Redis durations = %#v, want defaults", cfg.Redis)
	}
	if cfg.Outbox.PollInterval != time.Second || cfg.Outbox.ClaimBatch != 10 ||
		cfg.Outbox.LeaseDuration != time.Minute || cfg.Outbox.PublishTimeout != 5*time.Second ||
		cfg.Outbox.RetryDelay != 30*time.Second || cfg.Outbox.CleanupInterval != time.Hour ||
		cfg.Outbox.Retention != 7*24*time.Hour || cfg.Outbox.CleanupBatch != 500 {
		t.Fatalf("Outbox config = %#v, want defaults", cfg.Outbox)
	}
	if cfg.Trace.Enabled || cfg.Trace.ServiceName != "backend" || cfg.Trace.SampleRatio != 0.10 || cfg.Trace.QueueCapacity != 2048 || cfg.Trace.BatchSize != 256 || cfg.Trace.BatchTimeout != time.Second || cfg.Trace.ExportTimeout != 2*time.Second || cfg.Trace.ShutdownTimeout != 5*time.Second {
		t.Fatalf("Trace config = %#v, want bounded disabled defaults", cfg.Trace)
	}
}

func TestLoadFromTraceConfigurationIsBounded(t *testing.T) {
	env := requiredEnvironment()
	env["GOPULSE_RUNTIME_MODE"] = "container"
	env["HTTP_HOST"] = "0.0.0.0"
	env["MYSQL_HOST"] = "mysql"
	env["REDIS_HOST"] = "redis"
	env["RABBITMQ_URL"] = "amqp://gopulse:rabbit-secret@rabbitmq:5672/"
	env["ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
	env["OBSERVABILITY_ELASTICSEARCH_URL"] = "http://observability-elasticsearch:9200"
	env["MONITOR_URL"] = "http://monitor:9090"
	env["LOG_MONITOR_URL"] = "http://monitor:9090"
	env["BACKEND_VICTORIAMETRICS_URL"] = "http://victoriametrics:8428"
	env["LOG_MONITOR_INGEST_TOKEN"] = "local-log-ingest-token-at-least-32-bytes"
	env["GOPULSE_TRACE_ENABLED"] = "true"
	env["GOPULSE_TRACE_ENDPOINT"] = "otel-collector:4317"
	env["GOPULSE_TRACE_SAMPLE_RATIO"] = "1"
	env["GOPULSE_TRACE_QUEUE_CAPACITY"] = "1024"
	env["GOPULSE_TRACE_BATCH_SIZE"] = "128"
	env["GOPULSE_TRACE_BATCH_TIMEOUT"] = "2s"
	env["GOPULSE_TRACE_EXPORT_TIMEOUT"] = "3s"
	env["GOPULSE_TRACE_SHUTDOWN_TIMEOUT"] = "6s"
	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if !cfg.Trace.Enabled || cfg.Trace.Endpoint != "otel-collector:4317" || cfg.Trace.SampleRatio != 1 || cfg.Trace.QueueCapacity != 1024 || cfg.Trace.BatchSize != 128 || cfg.Trace.BatchTimeout != 2*time.Second || cfg.Trace.ExportTimeout != 3*time.Second || cfg.Trace.ShutdownTimeout != 6*time.Second {
		t.Fatalf("Trace config = %#v", cfg.Trace)
	}
	for _, test := range []struct {
		key, value string
		endpoint   bool
	}{
		{key: "GOPULSE_TRACE_SAMPLE_RATIO", value: "1.1", endpoint: true},
		{key: "GOPULSE_TRACE_ENABLED", value: "true", endpoint: false},
	} {
		candidate := requiredEnvironment()
		candidate["GOPULSE_RUNTIME_MODE"] = "container"
		candidate["HTTP_HOST"] = "0.0.0.0"
		candidate["MYSQL_HOST"] = "mysql"
		candidate["REDIS_HOST"] = "redis"
		candidate["RABBITMQ_URL"] = "amqp://gopulse:rabbit-secret@rabbitmq:5672/"
		candidate["ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
		candidate["OBSERVABILITY_ELASTICSEARCH_URL"] = "http://observability-elasticsearch:9200"
		candidate["MONITOR_URL"] = "http://monitor:9090"
		candidate["LOG_MONITOR_URL"] = "http://monitor:9090"
		candidate["BACKEND_VICTORIAMETRICS_URL"] = "http://victoriametrics:8428"
		candidate["LOG_MONITOR_INGEST_TOKEN"] = "local-log-ingest-token-at-least-32-bytes"
		candidate[test.key] = test.value
		if test.endpoint {
			candidate["GOPULSE_TRACE_ENABLED"] = "true"
			candidate["GOPULSE_TRACE_ENDPOINT"] = "otel-collector:4317"
		}
		if _, err := LoadFrom(mapLookup(candidate)); err == nil || !strings.Contains(err.Error(), "GOPULSE_TRACE") {
			t.Fatalf("invalid trace value %s=%s error = %v", test.key, test.value, err)
		}
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
	env["MYSQL_TOTAL_MAX_OPEN_CONNS"] = "16"
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
	env["OUTBOX_POLL_INTERVAL"] = "250ms"
	env["OUTBOX_CLAIM_BATCH"] = "25"
	env["OUTBOX_LEASE_DURATION"] = "2m"
	env["OUTBOX_PUBLISH_TIMEOUT"] = "4s"
	env["OUTBOX_RETRY_DELAY"] = "2m"
	env["OUTBOX_CLEANUP_INTERVAL"] = "30m"
	env["OUTBOX_PUBLISHED_RETENTION"] = "720h"
	env["OUTBOX_CLEANUP_BATCH"] = "750"
	env["RABBITMQ_URL"] = "amqp://gopulse:rabbit-secret@rabbitmq:5672/"
	env["ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
	env["OBSERVABILITY_ELASTICSEARCH_URL"] = "http://observability-elasticsearch:9200"
	env["MONITOR_URL"] = "http://monitor:9090"
	env["BACKEND_VICTORIAMETRICS_URL"] = "https://victoriametrics:8428"
	env["BACKEND_VICTORIAMETRICS_USERNAME"] = "metrics-reader"
	env["BACKEND_VICTORIAMETRICS_PASSWORD"] = "metrics-reader-password-at-least-32-bytes"
	env["BACKEND_VICTORIAMETRICS_QUERY_TIMEOUT"] = "4s"

	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.RuntimeMode != RuntimeModeContainer || cfg.AppEnv != "test" || cfg.HTTPAddress() != "0.0.0.0:18080" || cfg.InstanceID != "backend-2" || cfg.ReplicaCount != 2 || cfg.HTTPMaxConcurrency != 64 {
		t.Fatalf("unexpected application config: %#v", cfg)
	}
	if cfg.MySQL.Host != "mysql" || cfg.MySQL.Port != 13306 {
		t.Fatalf("unexpected MySQL config: %#v", cfg.MySQL)
	}
	if cfg.Elasticsearch.URL != "http://elasticsearch:9200" || cfg.Elasticsearch.Purpose != "search" || cfg.ObservabilityElasticsearch.URL != "http://observability-elasticsearch:9200" || cfg.ObservabilityElasticsearch.Purpose != "observability" {
		t.Fatalf("unexpected Elasticsearch clients: search=%#v observability=%#v", cfg.Elasticsearch, cfg.ObservabilityElasticsearch)
	}
	if cfg.MySQL.MaxOpenConns != 8 || cfg.MySQL.MaxIdleConns != 4 || cfg.MySQL.ConnMaxLifetime != 11*time.Minute {
		t.Fatalf("unexpected MySQL pool config: %#v", cfg.MySQL)
	}
	if cfg.Redis.Host != "redis" || cfg.Redis.Port != 16379 || cfg.Redis.DB != 3 {
		t.Fatalf("unexpected Redis config: %#v", cfg.Redis)
	}
	if cfg.VictoriaMetrics.URL != "https://victoriametrics:8428" || cfg.VictoriaMetrics.Username != "metrics-reader" || cfg.VictoriaMetrics.RequestTimeout != 4*time.Second {
		t.Fatalf("unexpected VictoriaMetrics config: %#v", cfg.VictoriaMetrics)
	}
	if cfg.Auth.JWTTTL != 30*time.Minute || cfg.Auth.CookieName != "custom_session" || !cfg.Auth.CookieSecure {
		t.Fatalf("unexpected auth config: %#v", cfg.Auth)
	}
	if cfg.Redis.PostDetailTTL != 10*time.Minute || cfg.Redis.OperationTimeout != 350*time.Millisecond {
		t.Fatalf("unexpected Redis duration config: %#v", cfg.Redis)
	}
	if cfg.Outbox.PollInterval != 250*time.Millisecond || cfg.Outbox.ClaimBatch != 25 ||
		cfg.Outbox.LeaseDuration != 2*time.Minute || cfg.Outbox.PublishTimeout != 4*time.Second ||
		cfg.Outbox.RetryDelay != 2*time.Minute || cfg.Outbox.CleanupInterval != 30*time.Minute ||
		cfg.Outbox.Retention != 720*time.Hour || cfg.Outbox.CleanupBatch != 750 {
		t.Fatalf("unexpected Outbox config: %#v", cfg.Outbox)
	}
}

func TestLoadFromMissingRequiredValue(t *testing.T) {
	for _, key := range []string{"MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "REDIS_PASSWORD", "RABBITMQ_URL", "AUTH_JWT_SECRET", "MONITOR_API_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD"} {
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

func TestLoadFromRejectsUnsafeVictoriaMetricsConfiguration(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{key: "BACKEND_VICTORIAMETRICS_URL", value: "http://user:pass@127.0.0.1:8428?query=secret"},
		{key: "BACKEND_VICTORIAMETRICS_PASSWORD", value: "short"},
		{key: "BACKEND_METRIC_QUERY_MAX_RANGE", value: "48h"},
	} {
		env := requiredEnvironment()
		env[test.key] = test.value
		if _, err := LoadFrom(mapLookup(env)); err == nil {
			t.Fatalf("%s=%q was accepted", test.key, test.value)
		}
	}
}

func requiredEnvironment() map[string]string {
	return map[string]string{
		"MYSQL_DATABASE":                   "gopulse",
		"MYSQL_USER":                       "gopulse",
		"MYSQL_PASSWORD":                   "mysql-secret",
		"REDIS_PASSWORD":                   "redis-secret",
		"RABBITMQ_URL":                     "amqp://gopulse:rabbit-secret@127.0.0.1:5672/",
		"AUTH_JWT_SECRET":                  "local-development-jwt-secret-32-bytes-minimum",
		"MONITOR_API_TOKEN":                "local-monitor-api-token-at-least-32-bytes",
		"BACKEND_VICTORIAMETRICS_PASSWORD": "local-victoriametrics-password-at-least-32-bytes",
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

func TestLoadFromEnforcesMonitorRequestTimeoutContract(t *testing.T) {
	env := requiredEnvironment()
	for _, value := range []string{"1s", "60s"} {
		env["MONITOR_REQUEST_TIMEOUT"] = value
		if _, err := LoadFrom(mapLookup(env)); err != nil {
			t.Fatalf("valid MONITOR_REQUEST_TIMEOUT %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{"999ms", "61s"} {
		env["MONITOR_REQUEST_TIMEOUT"] = value
		if _, err := LoadFrom(mapLookup(env)); err == nil {
			t.Fatalf("invalid MONITOR_REQUEST_TIMEOUT %s accepted", value)
		}
	}
}
