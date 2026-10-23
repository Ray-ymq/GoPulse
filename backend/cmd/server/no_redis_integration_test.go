//go:build integration

package main

import (
	"context"
	"os"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
)

func TestIntegrationNoRedisAssembly(t *testing.T) {
	if os.Getenv("PT3_INTEGRATION") != "1" {
		t.Skip("仅在测试三独立环境中执行")
	}
	cfg, err := config.LoadFrom(os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RedisDisabled = true
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 1
	cfg.Redis.Password = ""
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := logging.Discard("backend")
	assembly, err := newRoleAssembly(cfg, logger, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer assembly.close(logger)
	if assembly.router == nil || assembly.dispatcher == nil || len(assembly.pools) != 3 {
		t.Fatal("无 Redis 模式必须保留 HTTP、outbox 和独立 MySQL 池")
	}
	for _, resource := range assembly.resources {
		if resource.name == "redis" || resource.name == "post_body_rebuild" {
			t.Fatalf("无 Redis 模式装配了 %s", resource.name)
		}
	}
}
