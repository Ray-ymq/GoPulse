package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"github.com/Ray-ymq/GoPulse/runtimekit/exportercontract"
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
		if isManagedCommand(os.Args[1]) {
			if len(os.Args) != 4 || os.Args[2] != "--config" {
				_ = exportercontract.Result(os.Stdout, false, "invalid_arguments")
				os.Exit(2)
			}
			code, err := runManaged(os.Args[1], os.Args[3])
			if err != nil {
				if code == "" {
					code = "runtime_failed"
				}
				_ = exportercontract.Result(os.Stdout, false, code)
				os.Exit(1)
			}
			return
		}
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
	return runWithConfig(cfg, logger)
}

func runWithConfig(cfg config.Config, logger *slog.Logger) error {
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
	ctx, cancel := runtimekit.SignalContext()
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

func isManagedCommand(command string) bool {
	return command == "check-config" || command == "check-connection" || command == "serve"
}

func runManaged(command, path string) (string, error) {
	if command == "check-config" {
		if _, err := config.LoadFile(path); err != nil {
			return "invalid_configuration", err
		}
		return "", exportercontract.Result(os.Stdout, true, "ok")
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return "invalid_configuration", err
	}
	if command == "check-connection" {
		if err := checkConfig(cfg); err != nil {
			return "target_unavailable", err
		}
		return "", nil
	}
	return "runtime_failed", runWithConfig(cfg, logging.New("redis-exporter", os.Stdout))
}

func checkConfig(cfg config.Config) error {
	goredis.SetLogger(&redislogging.VoidLogger{})
	client := goredis.NewClient(&goredis.Options{
		Addr: cfg.RedisAddress(), Password: cfg.RedisPassword, DB: cfg.RedisDB,
		DialTimeout: cfg.ConnectTimeout, ReadTimeout: cfg.ScrapeTimeout, WriteTimeout: cfg.ScrapeTimeout,
		ContextTimeoutEnabled: true, MaxRetries: -1,
	})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ScrapeTimeout)
	defer cancel()
	if _, err := collector.New(client, cfg.RedisDB).Collect(ctx); err != nil {
		return err
	}
	return exportercontract.Result(os.Stdout, true, "ok")
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	return runtimekit.ServeRuntime(ctx, server, listener, shutdownTimeout, logger)
}
