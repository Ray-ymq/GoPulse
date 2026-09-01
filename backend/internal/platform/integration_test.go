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
