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
	return &Redis{client: goredis.NewClient(&goredis.Options{
		Addr:         net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
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

func (client *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
	value, err := client.client.TTL(ctx, key).Result()
	componentmetrics.Dependency("redis", err)
	return value, err
}

func (client *Redis) Close() error {
	return client.client.Close()
}
