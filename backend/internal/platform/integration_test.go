//go:build integration

package platform

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
)

func TestIntegrationDependenciesAreAvailable(t *testing.T) {
	cfg := integrationtest.Environment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mysqlClient, err := NewMySQL(cfg.MySQL)
	if err != nil {
		t.Fatalf("NewMySQL() error = %v", err)
	}
	defer mysqlClient.Close()
	if err := mysqlClient.Check(ctx); err != nil {
		t.Fatalf("MySQL dependency is unavailable: %v", err)
	}

	redisClient := NewRedis(cfg.Redis)
	defer redisClient.Close()
	if err := redisClient.Check(ctx); err != nil {
		t.Fatalf("Redis dependency is unavailable: %v", err)
	}
}

// A slow server response reproduces the cold-start migration connection failure
// without relying on host load to make a particular ALTER TABLE take >1s.
func TestIntegrationMigrationAllowsSlowStatement(t *testing.T) {
	cfg := integrationtest.Environment(t)
	database, err := OpenMySQLMigrationDatabase(cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result int
	if err := database.QueryRowContext(ctx, "SELECT SLEEP(1.2)").Scan(&result); err != nil {
		t.Fatalf("migration connection must survive a response beyond the application 1s budget: %v", err)
	}
	if result != 0 {
		t.Fatalf("slow statement interrupted: %d", result)
	}
}
