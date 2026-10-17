//go:build integration

package ratelimit

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/integrationtest"
	goredis "github.com/redis/go-redis/v9"
)

type integrationRedisClient struct {
	client *goredis.Client
}

func (client integrationRedisClient) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	return client.client.Eval(ctx, script, keys, args...).Result()
}

func integrationRedis(t *testing.T, cfg config.Config) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:         net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		PoolTimeout:  cfg.Redis.PoolTimeout,
		DialTimeout:  cfg.Redis.OperationTimeout,
		ReadTimeout:  cfg.Redis.OperationTimeout,
		WriteTimeout: cfg.Redis.OperationTimeout,
		MaxRetries:   0,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("connect integration Redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestIntegrationSharedLimiterEnforcesCreationBudgetAndTTL(t *testing.T) {
	cfg := integrationtest.Environment(t)
	client := integrationRedis(t, cfg)
	policy := Policy{
		Name: "integration_creation", KeyPrefix: "gopulse:rl:integration:creation:",
		Capacity: 10, RefillPerSecond: 1, Cost: 1, IdleTTL: time.Second,
		CreationCapacity: 2, CreationRate: 0.001, CreationTTL: 100 * time.Millisecond,
	}
	limiter := NewSharedLimiter(integrationRedisClient{client: client}, 100*time.Millisecond)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	identities := []string{"user:" + suffix + ":1", "user:" + suffix + ":2", "user:" + suffix + ":3"}
	keys := []string{policy.KeyPrefix + identities[0], policy.KeyPrefix + identities[1], policy.KeyPrefix + identities[2], "gopulse:rl:v1:creation:" + policy.Name}
	t.Cleanup(func() { _ = client.Del(context.Background(), keys...).Err() })

	for _, identity := range identities[:2] {
		decision, err := limiter.Allow(context.Background(), policy, identity)
		if err != nil || !decision.Allowed {
			t.Fatalf("initial creation decision=%#v error=%v", decision, err)
		}
	}
	decision, err := limiter.Allow(context.Background(), policy, identities[2])
	if err != nil || decision.Allowed || decision.Reason != "key_creation_limited" {
		t.Fatalf("creation budget decision=%#v error=%v", decision, err)
	}
	ttl, err := client.PTTL(context.Background(), keys[3]).Result()
	if err != nil || ttl <= 0 {
		t.Fatalf("creation budget TTL=%s error=%v", ttl, err)
	}

	time.Sleep(150 * time.Millisecond)
	decision, err = limiter.Allow(context.Background(), policy, identities[2])
	if err != nil || !decision.Allowed {
		t.Fatalf("expired creation budget decision=%#v error=%v", decision, err)
	}
}

func TestIntegrationSharedLimiterFailsClosedOnRedisOOM(t *testing.T) {
	cfg := integrationtest.Environment(t)
	client := integrationRedis(t, cfg)
	ctx := context.Background()
	maxMemory, err := client.ConfigGet(ctx, "maxmemory").Result()
	if err != nil {
		t.Fatalf("read Redis maxmemory: %v", err)
	}
	maxMemoryPolicy, err := client.ConfigGet(ctx, "maxmemory-policy").Result()
	if err != nil {
		t.Fatalf("read Redis maxmemory-policy: %v", err)
	}
	t.Cleanup(func() {
		if err := client.ConfigSet(ctx, "maxmemory", maxMemory["maxmemory"]).Err(); err != nil {
			t.Errorf("restore Redis maxmemory: %v", err)
		}
		if err := client.ConfigSet(ctx, "maxmemory-policy", maxMemoryPolicy["maxmemory-policy"]).Err(); err != nil {
			t.Errorf("restore Redis maxmemory-policy: %v", err)
		}
	})
	if err := client.ConfigSet(ctx, "maxmemory-policy", "noeviction").Err(); err != nil {
		t.Fatalf("set Redis noeviction: %v", err)
	}
	if err := client.ConfigSet(ctx, "maxmemory", "1").Err(); err != nil {
		t.Fatalf("set Redis OOM budget: %v", err)
	}

	policy := Policy{
		Name: "integration_oom", KeyPrefix: "gopulse:rl:integration:oom:",
		Capacity: 2, RefillPerSecond: 1, Cost: 1, IdleTTL: time.Minute,
		CreationCapacity: 2, CreationRate: 1, CreationTTL: time.Minute,
	}
	limiter := NewSharedLimiter(integrationRedisClient{client: client}, 100*time.Millisecond)
	if _, err := limiter.Allow(ctx, policy, "user:oom"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("OOM limiter error=%v, want ErrUnavailable", err)
	}
}

func TestIntegrationSharedLimiterTimesOutHungRedis(t *testing.T) {
	integrationtest.Environment(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen hung Redis fixture: %v", err)
	}
	stop := make(chan struct{})
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		<-stop
	}()
	t.Cleanup(func() {
		close(stop)
		_ = listener.Close()
	})

	client := goredis.NewClient(&goredis.Options{
		Addr:         listener.Addr().String(),
		PoolSize:     1,
		PoolTimeout:  25 * time.Millisecond,
		DialTimeout:  25 * time.Millisecond,
		ReadTimeout:  25 * time.Millisecond,
		WriteTimeout: 25 * time.Millisecond,
		MaxRetries:   0,
	})
	defer client.Close()
	limiter := NewSharedLimiter(integrationRedisClient{client: client}, 25*time.Millisecond)
	started := time.Now()
	_, err = limiter.Allow(context.Background(), Policy{
		Name: "integration_hung", KeyPrefix: "gopulse:rl:integration:hung:",
		Capacity: 2, RefillPerSecond: 1, Cost: 1, IdleTTL: time.Minute,
		CreationCapacity: 2, CreationRate: 1, CreationTTL: time.Minute,
	}, "user:hung")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("hung Redis error=%v, want ErrUnavailable", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("hung Redis call took %s, want bounded timeout", elapsed)
	}
}
