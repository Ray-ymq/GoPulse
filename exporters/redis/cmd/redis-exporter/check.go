package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/collector"
	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/config"
	goredis "github.com/redis/go-redis/v9"
	redislogging "github.com/redis/go-redis/v9/logging"
)

// check never starts a listener or creates runtime state. All protocol diagnostics
// are discarded; the only output is a closed, safe result consumed by Monitor.
func check(output io.Writer) error {
	goredis.SetLogger(&redislogging.VoidLogger{})
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(output, `{"reachable":false,"code":"invalid_configuration"}`)
		return errors.New("invalid configuration")
	}
	client := goredis.NewClient(&goredis.Options{
		Addr: cfg.RedisAddress(), Password: cfg.RedisPassword, DB: cfg.RedisDB,
		DialTimeout: cfg.ConnectTimeout, ReadTimeout: cfg.ScrapeTimeout, WriteTimeout: cfg.ScrapeTimeout,
		ContextTimeoutEnabled: true, MaxRetries: -1,
	})
	defer client.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return checkSource(ctx, collector.New(client, cfg.RedisDB), cfg.ScrapeTimeout, output)
}

func checkSource(ctx context.Context, source collector.Collector, timeout time.Duration, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := source.Collect(ctx); err != nil {
		// No raw error is returned or printed, even for an unexpected collector error.
		fmt.Fprintln(output, `{"reachable":false,"code":"target_unavailable"}`)
		return errors.New("target unavailable")
	}
	_, err := fmt.Fprintln(output, `{"reachable":true}`)
	return err
}
