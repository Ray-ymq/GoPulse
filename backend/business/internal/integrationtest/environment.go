//go:build integration

package integrationtest

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

const (
	allowedHost         = "127.0.0.1"
	allowedDatabase     = "gopulse_integration"
	allowedDatabaseUser = "gopulse_integration"
	allowedRedisDB      = 15
)

// Environment loads the ordinary application configuration but requires an
// explicit, narrowly named integration target before any test can mutate data.
// Missing dependencies and unsafe targets fail instead of being skipped.
func Environment(t *testing.T) config.Config {
	t.Helper()
	if testingFlag := lookup("INTEGRATION_TESTS"); testingFlag != "1" {
		t.Fatal("INTEGRATION_TESTS=1 is required for integration tests")
	}
	if lookup("INTEGRATION_DYNAMIC_PORTS") != "1" || lookup("INTEGRATION_RUN_ID") == "" {
		t.Fatal("integration tests must run with a Testcontainers-owned dynamic environment")
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
	if !rabbitMQTargetIsOwned(cfg.RabbitMQURL) {
		t.Fatalf("RABBITMQ_URL = %q, want a dynamic loopback Testcontainers endpoint", cfg.RabbitMQURL)
	}
	if !httpTargetIsOwned(cfg.Elasticsearch.URL) {
		t.Fatalf("ELASTICSEARCH_URL = %q, want a dynamic loopback Testcontainers endpoint", cfg.Elasticsearch.URL)
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
	if !rabbitMQTargetIsOwned(cfg.RabbitMQURL) || !httpTargetIsOwned(cfg.Elasticsearch.URL) || !httpTargetIsOwned(cfg.ObservabilityElasticsearch.URL) || !httpTargetIsOwned(cfg.VictoriaMetrics.URL) || !httpTargetIsOwned(cfg.Monitor.URL) {
		t.Fatalf("observability integration endpoints are outside the Testcontainers target set: rabbit=%q search=%q observe=%q metrics=%q monitor=%q", cfg.RabbitMQURL, cfg.Elasticsearch.URL, cfg.ObservabilityElasticsearch.URL, cfg.VictoriaMetrics.URL, cfg.Monitor.URL)
	}
	if cfg.HTTPPort < 1 || !strings.HasSuffix(cfg.Auth.CookieName, "_test_session") {
		t.Fatalf("observability integration application identity is outside the dynamic test contract: http=%d cookie=%q", cfg.HTTPPort, cfg.Auth.CookieName)
	}
	return cfg
}

func lookup(key string) string {
	return os.Getenv(key)
}

func rabbitMQTargetIsOwned(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "amqp" || parsed.User == nil || parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	password, hasPassword := parsed.User.Password()
	return parsed.User.Username() == "gopulse_integration" && hasPassword && password == "integration-rabbitmq" && localEndpoint(parsed.Host)
}

func httpTargetIsOwned(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "http" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && localEndpoint(parsed.Host)
}

func localEndpoint(raw string) bool {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || (host != allowedHost && host != "localhost") {
		return false
	}
	parsed, err := strconv.Atoi(port)
	return err == nil && parsed > 0 && parsed < 65536
}
