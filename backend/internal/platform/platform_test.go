package platform

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestClientsCanBeConstructedWhenDependenciesAreUnavailable(t *testing.T) {
	mysqlClient, err := NewMySQL(config.MySQLConfig{
		Host:     "127.0.0.1",
		Port:     1,
		Database: "gopulse",
		User:     "gopulse",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewMySQL() error = %v", err)
	}
	defer mysqlClient.Close()

	redisClient := NewRedis(config.RedisConfig{
		Host:     "127.0.0.1",
		Port:     1,
		Password: "secret",
	})
	defer redisClient.Close()

	rabbitMQ, err := NewRabbitMQ("amqp://gopulse:secret@127.0.0.1:1/")
	if err != nil {
		t.Fatalf("NewRabbitMQ() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := mysqlClient.Check(ctx); err == nil {
		t.Fatal("MySQL Check() error = nil, want unavailable dependency")
	}
	if err := redisClient.Check(ctx); err == nil {
		t.Fatal("Redis Check() error = nil, want unavailable dependency")
	}
	if err := rabbitMQ.Check(ctx); err == nil {
		t.Fatal("RabbitMQ Check() error = nil, want unavailable dependency")
	}
}
