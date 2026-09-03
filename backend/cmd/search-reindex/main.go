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
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
)

func main() {
	logger := logging.New("search-reindex", os.Stdout)
	if err := run(os.Args[1:], logger); err != nil {
		logging.Module(logger, "search").Error("search reindex failed", slog.String("reason", "operation_failed"))
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("search-reindex")
	}
	searchLogger := logging.Module(logger, "search")
	flags := flag.NewFlagSet("search-reindex", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ifMissing := flags.Bool("if-missing", false, "initialize the search alias only when it does not exist")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		searchLogger.Error("search reindex arguments invalid", slog.String("reason", "invalid_arguments"))
		return errors.New("search reindex arguments invalid")
	}

	cfg, err := config.LoadReindex()
	if err != nil {
		searchLogger.Error("search reindex initialization failed", slog.String("stage", "configuration"), slog.String("reason", "invalid_configuration"))
		return errors.New("load search reindex configuration")
	}
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
	result, err := reindexer.Run(ctx, *ifMissing)
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
