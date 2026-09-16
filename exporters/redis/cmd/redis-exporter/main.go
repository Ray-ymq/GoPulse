package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/collector"
	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/config"
	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/httpapi"
	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/logging"
	goredis "github.com/redis/go-redis/v9"
	redislogging "github.com/redis/go-redis/v9/logging"
)

const (
	readHeaderTimeout = 2 * time.Second
	readTimeout       = 5 * time.Second
	idleTimeout       = 30 * time.Second
	maxHeaderBytes    = 1 << 20
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "--check" {
			fmt.Fprintln(os.Stdout, `{"reachable":false,"code":"invalid_arguments"}`)
			os.Exit(1)
		}
		if check(os.Stdout) != nil {
			os.Exit(1)
		}
		return
	}
	logger := logging.New("redis-exporter", os.Stdout)
	if err := run(logger); err != nil {
		logging.Module(logger, "runtime").Error("redis exporter stopped", slog.String("reason", "process_failed"))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("redis-exporter")
	}
	goredis.SetLogger(&redislogging.VoidLogger{})
	cfg, err := config.Load()
	if err != nil {
		logging.Module(logger, "config").Error("configuration invalid", slog.String("field", config.Field(err)), slog.String("reason", "invalid_configuration"))
		return errors.New("invalid configuration")
	}
	client := goredis.NewClient(&goredis.Options{
		Addr: cfg.RedisAddress(), Password: cfg.RedisPassword, DB: cfg.RedisDB,
		DialTimeout: cfg.ConnectTimeout, ReadTimeout: cfg.ScrapeTimeout, WriteTimeout: cfg.ScrapeTimeout,
		MaxRetries: -1,
	})
	collectorSource := collector.New(client, cfg.RedisDB)
	server := &http.Server{
		Addr:              cfg.HTTPAddress(),
		Handler:           httpapi.New(collectorSource, cfg.ScrapeTimeout, logging.Module(logger, "collector")),
		ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout,
		WriteTimeout: cfg.ScrapeTimeout + 2*time.Second, IdleTimeout: idleTimeout, MaxHeaderBytes: maxHeaderBytes,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		_ = client.Close()
		logging.Module(logger, "http").Error("redis exporter listen failed", slog.String("reason", "listen_failed"))
		return errors.New("listen failed")
	}
	ctx, cancel := componentmetrics.SignalContext()
	defer cancel()
	runErr := serve(ctx, server, listener, cfg.ShutdownTimeout, logging.Module(logger, "runtime"))
	if err := client.Close(); err != nil {
		logging.Module(logger, "runtime").Warn("resource close failed", slog.String("resource", "redis"), slog.String("reason", "close_failed"))
		if runErr == nil {
			runErr = errors.New("redis close failed")
		}
	}
	return runErr
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	return componentmetrics.ServeRuntime(ctx, server, listener, shutdownTimeout, logger)
}
