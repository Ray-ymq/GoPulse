package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/processlog"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/worker"
)

func main() {
	os.Exit(execute(os.Stdout, config.LoadSearchIndexer, run))
}

func execute(stdout io.Writer, load func() (config.SearchIndexerConfig, error), operation func(config.SearchIndexerConfig, *slog.Logger) error) int {
	stdoutLogger := logging.New("search-indexer", stdout)
	cfg, err := load()
	if err != nil {
		indexerInitializationFailure(logging.Module(stdoutLogger, "lifecycle"), "configuration", "invalid_configuration")
		return 1
	}
	logs, err := processlog.Open("search-indexer", stdout, cfg.LogShip)
	if err != nil {
		indexerInitializationFailure(logging.Module(stdoutLogger, "lifecycle"), "logship", "invalid_configuration")
		return 1
	}
	exitCode := 0
	if err := operation(cfg, logs.Logger); err != nil {
		logging.Module(logs.Logger, "lifecycle").Error("search indexer stopped", slog.String("reason", "process_failed"))
		exitCode = 1
	}
	if err := logs.Close(); err != nil {
		logging.Module(stdoutLogger, "logship").Warn("log shipper shutdown incomplete", slog.String("reason", "shutdown_timeout"))
	}
	return exitCode
}

func run(cfg config.SearchIndexerConfig, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("search-indexer")
	}
	lifecycleLogger := logging.Module(logger, "lifecycle")
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
