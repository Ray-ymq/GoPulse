package collector

import (
	"github.com/Ray-ymq/GoPulse/exporters/redis/config"
	goredis "github.com/redis/go-redis/v9"
	redislogging "github.com/redis/go-redis/v9/logging"
)

type Client struct {
	Source Collector
	redis  *goredis.Client
}

func NewClient(c *config.Config) (*Client, error) {
	goredis.SetLogger(&redislogging.VoidLogger{})
	client := goredis.NewClient(&goredis.Options{Addr: c.RedisAddress(), Password: c.RedisPassword, DB: c.RedisDB, DialTimeout: c.ConnectTimeout, ReadTimeout: c.ScrapeTimeout, WriteTimeout: c.ScrapeTimeout, ContextTimeoutEnabled: true, MaxRetries: -1})
	return &Client{Source: NewSource(client, c.RedisDB), redis: client}, nil
}
func (c *Client) CloseIdleConnections() {
	if c.redis != nil {
		_ = c.redis.Close()
	}
}
