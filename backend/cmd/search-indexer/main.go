package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func main() {
	logger := logging.New("search-indexer", os.Stdout)
	if err := run(logger); err != nil {
		logging.Module(logger, "lifecycle").Error("search indexer stopped", slog.String("reason", "process_failed"))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("search-indexer")
	}
	lifecycleLogger := logging.Module(logger, "lifecycle")
	cfg, err := config.LoadSearchIndexer()
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "configuration", "invalid_configuration")
	}
	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "mysql", "connection_failed")
	}
	defer func() {
		if err := mysqlClient.Close(); err != nil {
			lifecycleLogger.Warn("resource close failed", slog.String("resource", "mysql"), slog.String("reason", "close_failed"))
		}
	}()
	elasticsearchClient, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "elasticsearch", "connection_failed")
	}
	processor, err := search.NewProcessor(search.NewMySQLDocumentStore(mysqlClient.DB()), search.NewElasticsearchRepository(elasticsearchClient))
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "search_processor", "invalid_dependency")
	}
	runtime, err := worker.NewRuntime(cfg.RabbitMQURL, processor, worker.RuntimeOptions{
		Profile:  worker.SearchProfile,
		Prefetch: cfg.Worker.Prefetch, MaxRetries: cfg.Worker.MaxRetries,
		RetryDelay: cfg.Worker.RetryDelay, PublishTimeout: cfg.Worker.PublishTimeout,
		ShutdownTimeout:  cfg.Worker.ShutdownTimeout,
		ReconnectMinimum: cfg.Worker.ReconnectMinimum, ReconnectMaximum: cfg.Worker.ReconnectMaximum,
		Logger: logger,
	})
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "runtime", "invalid_configuration")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lifecycleLogger.Info("search indexer started")
	if err := runtime.Run(ctx); err != nil {
		return errors.New("search indexer runtime failed")
	}
	lifecycleLogger.Info("search indexer stopped", slog.String("reason", "shutdown_complete"))
	return nil
}

func indexerInitializationFailure(logger *slog.Logger, stage, reason string) error {
	logger.Error("search indexer initialization failed", slog.String("stage", stage), slog.String("reason", reason))
	return errors.New("search indexer initialization failed")
}
