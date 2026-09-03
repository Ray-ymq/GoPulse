package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func main() {
	logger := logging.New("business-worker", os.Stdout)
	if err := run(logger); err != nil {
		logging.Module(logger, "lifecycle").Error("business worker stopped", slog.String("reason", "process_failed"))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("business-worker")
	}
	lifecycleLogger := logging.Module(logger, "lifecycle")
	cfg, err := config.LoadWorker()
	if err != nil {
		return initializationFailure(lifecycleLogger, "configuration", "invalid_configuration")
	}
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
