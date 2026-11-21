package redisclient

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func (client *Redis) MGet(ctx context.Context, keys []string) ([]any, error) {
	var values []any
	err := client.call(ctx, func() error {
		var err error
		values, err = client.client.MGet(ctx, keys...).Result()
		return err
	})
	return values, err
}

func (client *Redis) SetMany(ctx context.Context, values map[string]string, expires time.Time) error {
	return client.call(ctx, func() error {
		ttl := time.Until(expires).Truncate(time.Millisecond)
		if ttl <= 0 {
			return nil
		}
		_, err := client.client.Pipelined(ctx, func(pipe goredis.Pipeliner) error {
			for key, value := range values {
				pipe.Set(ctx, key, value, ttl)
			}
			return nil
		})
		return err
	})
}

func (client *Redis) DeleteMany(ctx context.Context, keys []string) error {
	return client.call(ctx, func() error {
		return client.client.Del(ctx, keys...).Err()
	})
}
