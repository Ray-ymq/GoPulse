package platform

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"net"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	goredis "github.com/redis/go-redis/v9"
)

var ErrRedisKeyNotFound = errors.New("redis key not found")

type Redis struct {
	client *goredis.Client
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
	return &Redis{client: goredis.NewClient(&goredis.Options{
		Addr:         net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     poolSize,
		PoolTimeout:  poolTimeout,
		MaxRetries:   0,
		DialTimeout:  poolTimeout,
		ReadTimeout:  poolTimeout,
		WriteTimeout: poolTimeout,
	})}
}

func (client *Redis) Check(ctx context.Context) error {
	err := client.client.Ping(ctx).Err()
	componentmetrics.Dependency("redis", err)
	return err
}

func (client *Redis) Get(ctx context.Context, key string) (string, error) {
	value, err := client.client.Get(ctx, key).Result()
	observedErr := err
	if errors.Is(err, goredis.Nil) {
		observedErr = nil
	}
	componentmetrics.Dependency("redis", observedErr)
	if errors.Is(err, goredis.Nil) {
		return "", ErrRedisKeyNotFound
	}
	return value, err
}

func (client *Redis) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	err := client.client.Set(ctx, key, value, expiration).Err()
	componentmetrics.Dependency("redis", err)
	return err
}

func (client *Redis) Delete(ctx context.Context, key string) error {
	err := client.client.Del(ctx, key).Err()
	componentmetrics.Dependency("redis", err)
	return err
}

// Eval executes one bounded Lua script. Higher-level adapters own the script
// text and key contract; exposing only Eval keeps the platform client small
// while allowing shared limiters to use Redis TIME atomically.
func (client *Redis) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	value, err := client.client.Eval(ctx, script, keys, args...).Result()
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) EvalSha(ctx context.Context, sha string, keys []string, args ...any) (any, error) {
	value, err := client.client.EvalSha(ctx, sha, keys, args...).Result()
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) ScriptLoad(ctx context.Context, script string) (string, error) {
	value, err := client.client.ScriptLoad(ctx, script).Result()
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
	value, err := client.client.TTL(ctx, key).Result()
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) Close() error {
	return client.client.Close()
}
