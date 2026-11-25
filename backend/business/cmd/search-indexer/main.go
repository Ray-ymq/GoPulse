package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/backend/internal/async/worker"
	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/modules/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform/config"
)

func main() {
	os.Exit(execute(os.Stdout, config.LoadSearchIndexer, run))
}

func execute(stdout io.Writer, load func() (config.SearchIndexerConfig, error), operation func(config.SearchIndexerConfig, *slog.Logger) error) int {
	stdoutLogger := logging.New("search-indexer", stdout)
	cfg, err := load()
	if err != nil {
		_ = indexerInitializationFailure(logging.Module(stdoutLogger, "lifecycle"), "configuration", "invalid_configuration")
		return 1
	}

	exitCode := 0
	if err := operation(cfg, stdoutLogger); err != nil {
		logging.Module(stdoutLogger, "lifecycle").Error("search indexer stopped", slog.String("reason", "process_failed"))
		exitCode = 1
	}

	return exitCode
}

func run(cfg config.SearchIndexerConfig, logger *slog.Logger) (runErr error) {
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
	state, err := businessstate.NewRepository(mysqlClient.DB(), businessstate.WithOpenRequirementsLimit(cfg.BusinessAsyncMaxOpenRequirements), businessstate.WithRetentionLimits(cfg.BusinessAsyncMaxRetainedGroups, cfg.BusinessAsyncMaxRetainedBytes))
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "business_state", "invalid_dependency")
	}
	if _, err := state.Ensure(context.Background()); err != nil {
		return indexerInitializationFailure(lifecycleLogger, "business_state", "not_ready")
	}
	elasticsearchClient, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "elasticsearch", "connection_failed")
	}
	processor, err := search.NewProcessor(search.NewMySQLDocumentStore(mysqlClient.DB()), search.NewElasticsearchRepository(elasticsearchClient))
	if err != nil {
		return indexerInitializationFailure(lifecycleLogger, "search_processor", "invalid_dependency")
	}
	processor.WithLogger(logger).WithBusinessState(state)
	runtime, err := worker.NewRuntime(cfg.RabbitMQURL, processor, worker.RuntimeOptions{
		Profile:   worker.SearchProfile,
		Admission: state,
		Prefetch:  cfg.Worker.Prefetch, MaxRetries: cfg.Worker.MaxRetries,
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
	probes := platform.NewHealth(ctx, func(checkCtx context.Context) error {
		if err := runtime.Ready(checkCtx); err != nil {
			return err
		}
		if err := mysqlClient.Check(checkCtx); err != nil {
			return err
		}
		if err := elasticsearchClient.Check(checkCtx); err != nil {
			return err
		}
		return nil
	})
	probeListener, err := platform.StartHealth(healthAddress("SEARCH_INDEXER_HEALTH_ADDRESS", "127.0.0.1:19103"), probes)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Worker.ShutdownTimeout)
		defer cancel()
		if err := probeListener.Shutdown(shutdownCtx); err != nil {
			_ = probeListener.Close()
			runErr = errors.New("shutdown_timeout")
		}
	}()
	probes.Started()
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

func healthAddress(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
