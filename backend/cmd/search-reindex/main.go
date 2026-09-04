package main

import (
	"context"
	"errors"
	"flag"
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
)

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, config.LoadReindex, run))
}

func execute(arguments []string, stdout io.Writer, load func() (config.ReindexConfig, error), operation func(config.ReindexConfig, bool, *slog.Logger) error) int {
	stdoutLogger := logging.New("search-reindex", stdout)
	ifMissing, err := parseArguments(arguments, stdoutLogger)
	if err != nil {
		return 1
	}
	cfg, err := load()
	if err != nil {
		logging.Module(stdoutLogger, "search").Error("search reindex initialization failed", slog.String("stage", "configuration"), slog.String("reason", "invalid_configuration"))
		return 1
	}
	logs, err := processlog.Open("search-reindex", stdout, cfg.LogShip)
	if err != nil {
		logging.Module(stdoutLogger, "search").Error("search reindex initialization failed", slog.String("stage", "logship"), slog.String("reason", "invalid_configuration"))
		return 1
	}
	exitCode := 0
	if err := operation(cfg, ifMissing, logs.Logger); err != nil {
		logging.Module(logs.Logger, "search").Error("search reindex failed", slog.String("reason", "operation_failed"))
		exitCode = 1
	}
	if err := logs.Close(); err != nil {
		logging.Module(stdoutLogger, "logship").Warn("log shipper shutdown incomplete", slog.String("reason", "shutdown_timeout"))
	}
	return exitCode
}

func parseArguments(arguments []string, logger *slog.Logger) (bool, error) {
	flags := flag.NewFlagSet("search-reindex", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ifMissing := flags.Bool("if-missing", false, "initialize the search alias only when it does not exist")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		logging.Module(logger, "search").Error("search reindex arguments invalid", slog.String("reason", "invalid_arguments"))
		return false, errors.New("search reindex arguments invalid")
	}
	return *ifMissing, nil
}

func run(cfg config.ReindexConfig, ifMissing bool, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("search-reindex")
	}
	searchLogger := logging.Module(logger, "search")
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		searchLogger.Error("search reindex initialization failed", slog.String("stage", "mysql"), slog.String("reason", "connection_failed"))
		return errors.New("open MySQL for search reindex")
	}
	defer func() {
		if err := database.Close(); err != nil {
			searchLogger.Warn("resource close failed", slog.String("resource", "mysql"), slog.String("reason", "close_failed"))
		}
	}()
	elasticsearch, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		searchLogger.Error("search reindex initialization failed", slog.String("stage", "elasticsearch"), slog.String("reason", "connection_failed"))
		return errors.New("create Elasticsearch client for search reindex")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	searchLogger.Info("search reindex started", slog.Int("batch_size", cfg.Elasticsearch.ReindexBatch))
	reindexer := search.NewReindexer(
		search.NewReindexStore(database),
		search.NewElasticsearchRepository(elasticsearch),
		cfg.Elasticsearch.ReindexBatch,
	)
	result, err := reindexer.Run(ctx, ifMissing)
	if err != nil {
		return errors.New("search reindex operation failed")
	}
	if !result.Changed {
		searchLogger.Info("search reindex skipped", slog.String("result", "unchanged"), slog.Int("batch_size", cfg.Elasticsearch.ReindexBatch))
		return nil
	}
	searchLogger.Info("search reindex completed",
		slog.String("result", "completed"),
		slog.Uint64("document_count", result.DocumentCount),
		slog.Int("batch_size", cfg.Elasticsearch.ReindexBatch),
	)
	return nil
}
