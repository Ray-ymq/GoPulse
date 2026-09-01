//go:build integration

package integrationtest

import (
	"os"
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
	return cfg
}

func lookup(key string) string {
	return os.Getenv(key)
}
