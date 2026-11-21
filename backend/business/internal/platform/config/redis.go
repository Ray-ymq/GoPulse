package config

import (
	"time"
)

const (
	defaultRedisHost             = "127.0.0.1"
	defaultRedisPort             = 6379
	defaultRedisDB               = 0
	defaultRedisPostDetailTTL    = 5 * time.Minute
	defaultRedisOperationTimeout = 200 * time.Millisecond
	defaultRedisPoolSize         = 16
	minimumRedisPostDetailTTL    = time.Second
	maximumRedisPostDetailTTL    = 24 * time.Hour
	minimumRedisOperationTimeout = 10 * time.Millisecond
	maximumRedisOperationTimeout = 5 * time.Second
	minimumRedisPoolSize         = 1
	maximumRedisPoolSize         = 256
)

type RedisConfig struct {
	PostCountsCache    bool
	DiscoveryCache     bool
	CommentPageCache   bool
	PublicProfileCache bool
	Host               string
	Port               int
	Password           string
	DB                 int
	PoolSize           int
	PoolTimeout        time.Duration
	PostDetailTTL      time.Duration
	OperationTimeout   time.Duration
}
