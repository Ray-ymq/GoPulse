package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
		DialTimeout: cfg.ScrapeTimeout, ReadTimeout: cfg.ScrapeTimeout, WriteTimeout: cfg.ScrapeTimeout,
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
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()
	logger.Info("redis exporter listening")
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("redis exporter server failed", slog.String("reason", "server_failed"))
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		logger.Info("redis exporter stopped", slog.String("reason", "server_closed"))
		return nil
	case <-ctx.Done():
		logger.Info("redis exporter shutdown started")
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("redis exporter shutdown failed", slog.String("reason", "shutdown_failed"))
			return errors.New("shutdown failed")
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("redis exporter shutdown failed", slog.String("reason", "server_failed"))
			return errors.New("server failed during shutdown")
		}
		logger.Info("redis exporter stopped", slog.String("reason", "shutdown_complete"))
		return nil
	}
}
