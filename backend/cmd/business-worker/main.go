package main

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/processlog"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func main() {
	os.Exit(execute(os.Stdout, config.LoadWorker, run))
}

func execute(stdout io.Writer, load func() (config.WorkerConfig, error), operation func(config.WorkerConfig, *slog.Logger) error) int {
	defer componentmetrics.ReleaseShutdown()
	stdoutLogger := logging.New("business-worker", stdout)
	cfg, err := load()
	if err != nil {
		initializationFailure(logging.Module(stdoutLogger, "lifecycle"), "configuration", "invalid_configuration")
		return 1
	}
	logs, err := processlog.Open("business-worker", stdout, cfg.LogShip)
	if err != nil {
		initializationFailure(logging.Module(stdoutLogger, "lifecycle"), "logship", "invalid_configuration")
		return 1
	}
	exitCode := 0
	if err := operation(cfg, logs.Logger); err != nil {
		logging.Module(logs.Logger, "lifecycle").Error("business worker stopped", slog.String("reason", "process_failed"))
		exitCode = 1
	}
	if err := logs.Close(); err != nil {
		logging.Module(stdoutLogger, "logship").Warn("log shipper shutdown incomplete", slog.String("reason", "shutdown_timeout"))
	}
	return exitCode
}

func run(cfg config.WorkerConfig, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("business-worker")
	}
	lifecycleLogger := logging.Module(logger, "lifecycle")
	metrics, err := componentmetrics.New("business-worker")
	if err != nil {
		return err
	}
	componentmetrics.Install(metrics)
	defer componentmetrics.Install(nil)
	metrics.Set("prefetch_limit", float64(cfg.Worker.Prefetch))
	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return initializationFailure(lifecycleLogger, "mysql", "connection_failed")
	}
	defer func() {
		if err := mysqlClient.Close(); err != nil {
			lifecycleLogger.Warn("resource close failed", slog.String("resource", "mysql"), slog.String("reason", "close_failed"))
		}
	}()
	repository, err := notification.NewRepository(mysqlClient.DB())
	if err != nil {
		return initializationFailure(lifecycleLogger, "notification_repository", "invalid_dependency")
	}
	processor, err := notification.NewProcessor(repository)
	if err != nil {
		return initializationFailure(lifecycleLogger, "notification_processor", "invalid_dependency")
	}
	runtime, err := worker.NewRuntime(cfg.RabbitMQURL, processor, worker.RuntimeOptions{
		Profile:  worker.BusinessProfile,
		Prefetch: cfg.Worker.Prefetch, MaxRetries: cfg.Worker.MaxRetries,
		RetryDelay: cfg.Worker.RetryDelay, PublishTimeout: cfg.Worker.PublishTimeout,
		ShutdownTimeout:  cfg.Worker.ShutdownTimeout,
		ReconnectMinimum: cfg.Worker.ReconnectMinimum, ReconnectMaximum: cfg.Worker.ReconnectMaximum,
		Logger: logger,
	})
	if err != nil {
		return initializationFailure(lifecycleLogger, "runtime", "invalid_configuration")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	componentmetrics.BindShutdown(ctx, cfg.Worker.ShutdownTimeout)
	internalMetrics, err := componentmetrics.StartConfigured(ctx, "business-worker", metrics.Snapshot)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := componentmetrics.ShutdownContext(cfg.Worker.ShutdownTimeout)
		defer cancel()
		_ = internalMetrics.Shutdown(shutdownCtx)
	}()
	lifecycleLogger.Info("business worker started")
	if err := runtime.Run(ctx); err != nil {
		return errors.New("business worker runtime failed")
	}
	lifecycleLogger.Info("business worker stopped", slog.String("reason", "shutdown_complete"))
	return nil
}

func initializationFailure(logger *slog.Logger, stage, reason string) error {
	logger.Error("business worker initialization failed", slog.String("stage", stage), slog.String("reason", reason))
	return errors.New("business worker initialization failed")
}
