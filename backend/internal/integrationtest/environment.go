//go:build integration

package integrationtest

import (
	"os"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

const (
	allowedHost         = "127.0.0.1"
	allowedDatabase     = "gopulse_integration"
	allowedDatabaseUser = "gopulse_integration"
	allowedRedisDB      = 15
	allowedRabbitMQURL  = "amqp://gopulse_integration:integration-rabbitmq@127.0.0.1:25672/"
	allowedSearchURL    = "http://127.0.0.1:29200"
	allowedCIRabbitURL  = "amqp://integration:integration@127.0.0.1:5672/"
	allowedCISearchURL  = "http://127.0.0.1:9200"
	allowedObserveURL   = "http://127.0.0.1:29201"
	allowedMetricsURL   = "http://127.0.0.1:18428"
	allowedMonitorURL   = "http://127.0.0.1:19090"
)

// Environment loads the ordinary application configuration but requires an
// explicit, narrowly named integration target before any test can mutate data.
// Missing dependencies and unsafe targets fail instead of being skipped.
func Environment(t *testing.T) config.Config {
	t.Helper()
	if testingFlag := lookup("INTEGRATION_TESTS"); testingFlag != "1" {
		t.Fatal("INTEGRATION_TESTS=1 is required for integration tests")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load integration configuration: %v", err)
	}
	if cfg.AppEnv != "test" {
		t.Fatalf("APP_ENV = %q, want test for integration tests", cfg.AppEnv)
	}
	if cfg.MySQL.Host != allowedHost || cfg.Redis.Host != allowedHost {
		t.Fatalf("integration dependencies must use loopback host %s", allowedHost)
	}
	if cfg.MySQL.Database != allowedDatabase {
		t.Fatalf("MYSQL_DATABASE = %q, want whitelisted %q", cfg.MySQL.Database, allowedDatabase)
	}
	if cfg.MySQL.User != allowedDatabaseUser {
		t.Fatalf("MYSQL_USER = %q, want whitelisted %q", cfg.MySQL.User, allowedDatabaseUser)
	}
	if cfg.Redis.DB != allowedRedisDB {
		t.Fatalf("REDIS_DB = %d, want whitelisted %d", cfg.Redis.DB, allowedRedisDB)
	}
	if !oneOf(cfg.RabbitMQURL, allowedRabbitMQURL, allowedCIRabbitURL) {
		t.Fatalf("RABBITMQ_URL = %q, want a whitelisted local or CI integration endpoint", cfg.RabbitMQURL)
	}
	if !oneOf(cfg.Elasticsearch.URL, allowedSearchURL, allowedCISearchURL) {
		t.Fatalf("ELASTICSEARCH_URL = %q, want a whitelisted local or CI integration endpoint", cfg.Elasticsearch.URL)
	}
	return cfg
}

// ObservabilityEnvironment extends the ordinary integration whitelist to every
// test-only observability endpoint. It is intentionally separate so ordinary
// business tests cannot accidentally depend on Kafka, VM, Monitor, or the
// observability Elasticsearch volume.
func ObservabilityEnvironment(t *testing.T) config.Config {
	t.Helper()
	if lookup("OBSERVABILITY_INTEGRATION") != "1" {
		t.Fatal("OBSERVABILITY_INTEGRATION=1 is required for observability integration tests")
	}
	cfg := Environment(t)
	if cfg.RabbitMQURL != allowedRabbitMQURL || cfg.Elasticsearch.URL != allowedSearchURL || cfg.ObservabilityElasticsearch.URL != allowedObserveURL || cfg.VictoriaMetrics.URL != allowedMetricsURL || cfg.Monitor.URL != allowedMonitorURL {
		t.Fatalf("observability integration endpoints are outside the test whitelist: rabbit=%q search=%q observe=%q metrics=%q monitor=%q", cfg.RabbitMQURL, cfg.Elasticsearch.URL, cfg.ObservabilityElasticsearch.URL, cfg.VictoriaMetrics.URL, cfg.Monitor.URL)
	}
	if cfg.HTTPPort != 18080 || !strings.HasSuffix(cfg.Auth.CookieName, "_test_session") {
		t.Fatalf("observability integration application identity is outside the test whitelist: http=%d cookie=%q", cfg.HTTPPort, cfg.Auth.CookieName)
	}
	return cfg
}

func lookup(key string) string {
	return os.Getenv(key)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
