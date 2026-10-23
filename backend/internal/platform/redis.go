package platform

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/backend/internal/instrumentation"
	"net"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	goredis "github.com/redis/go-redis/v9"
)

var ErrRedisKeyNotFound = errors.New("redis key not found")

type Redis struct {
	client   *goredis.Client
	recovery *redisRecoveryGate
}

func NewRedis(cfg config.RedisConfig) *Redis {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 16
	}
	poolTimeout := cfg.PoolTimeout
	if poolTimeout <= 0 {
		poolTimeout = cfg.OperationTimeout
	}
	if poolTimeout <= 0 {
		poolTimeout = time.Second
	}
	redisClient := goredis.NewClient(&goredis.Options{
		Addr:         net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     poolSize,
		PoolTimeout:  poolTimeout,
		MaxRetries:   0,
		DialTimeout:  poolTimeout,
		ReadTimeout:  poolTimeout,
		WriteTimeout: poolTimeout,
	})
	return &Redis{
		client:   redisClient,
		recovery: newRedisRecoveryGate(defaultRedisRecoveryCooldown),
	}
}

func (client *Redis) Check(ctx context.Context) error {
	err := client.call(ctx, func() error { return client.client.Ping(ctx).Err() })
	instrumentation.Dependency("redis", err)
	return err
}

func (client *Redis) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.Get(ctx, key).Result()
		return err
	})
	observedErr := err
	if errors.Is(err, goredis.Nil) {
		observedErr = nil
	}
	instrumentation.Dependency("redis", observedErr)
	if errors.Is(err, goredis.Nil) {
		return "", ErrRedisKeyNotFound
	}
	return value, err
}

func (client *Redis) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	err := client.call(ctx, func() error { return client.client.Set(ctx, key, value, expiration).Err() })
	instrumentation.Dependency("redis", err)
	return err
}

func (client *Redis) Delete(ctx context.Context, key string) error {
	err := client.call(ctx, func() error { return client.client.Del(ctx, key).Err() })
	instrumentation.Dependency("redis", err)
	return err
}

// Eval executes one bounded Lua script. Higher-level adapters own the script
// text and key contract; exposing only Eval keeps the platform client small
// while allowing shared limiters to use Redis TIME atomically.
func (client *Redis) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	var value any
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.Eval(ctx, script, keys, args...).Result()
		return err
	})
	instrumentation.Dependency("redis", err)
	return value, err
}

func (client *Redis) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error) {
	var value any
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.EvalSha(ctx, sha, keys, args...).Result()
		return err
	})
	instrumentation.Dependency("redis", err)
	return value, err
}

func (client *Redis) ScriptLoad(ctx context.Context, script string) (string, error) {
	var value string
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.ScriptLoad(ctx, script).Result()
		return err
	})
	instrumentation.Dependency("redis", err)
	return value, err
}

func (client *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
	var value time.Duration
	err := client.call(ctx, func() error {
		var err error
		value, err = client.client.TTL(ctx, key).Result()
		return err
	})
	instrumentation.Dependency("redis", err)
	return value, err
}

func (client *Redis) call(ctx context.Context, operation func() error) error {
	if client.recovery == nil {
		return operation()
	}
	return client.recovery.call(ctx, operation)
}

func (client *Redis) Close() error {
	return client.client.Close()
}
