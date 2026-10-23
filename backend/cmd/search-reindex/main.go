package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/bus"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/processlog"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
)

func main() {
	os.Exit(executeWithOptions(os.Args[1:], os.Stdout, config.LoadReindex, runWithOptions))
}

func execute(arguments []string, stdout io.Writer, load func() (config.ReindexConfig, error), operation func(config.ReindexConfig, bool, *slog.Logger) error) int {
	return executeWithOptions(arguments, stdout, load, func(cfg config.ReindexConfig, options reindexOptions, logger *slog.Logger) error {
		return operation(cfg, options.ifMissing, logger)
	})
}

type reindexOptions struct {
	ifMissing        bool
	restoreOperation string
	expectedOldEpoch string
	newEpoch         string
}

func executeWithOptions(arguments []string, stdout io.Writer, load func() (config.ReindexConfig, error), operation func(config.ReindexConfig, reindexOptions, *slog.Logger) error) int {
	stdoutLogger := logging.New("search-reindex", stdout)
	options, err := parseReindexArguments(arguments, stdoutLogger)
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
	if err := operation(cfg, options, logs.Logger); err != nil {
		logging.Module(logs.Logger, "search").Error("search reindex failed", slog.String("reason", "operation_failed"))
		exitCode = 1
	}
	if err := logs.Close(); err != nil {
		logging.Module(stdoutLogger, "logship").Warn("log shipper shutdown incomplete", slog.String("reason", "shutdown_timeout"))
	}
	return exitCode
}

func parseReindexArguments(arguments []string, logger *slog.Logger) (reindexOptions, error) {
	flags := flag.NewFlagSet("search-reindex", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ifMissing := flags.Bool("if-missing", false, "initialize the search alias only when it does not exist")
	restoreOperation := flags.String("restore-operation", "", "64-character lifecycle restore operation ID")
	expectedOldEpoch := flags.String("expected-old-epoch", "", "canonical UUID of the restored source epoch")
	newEpoch := flags.String("new-epoch", "", "canonical UUID of the new data epoch")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		logging.Module(logger, "search").Error("search reindex arguments invalid", slog.String("reason", "invalid_arguments"))
		return reindexOptions{}, errors.New("search reindex arguments invalid")
	}
	options := reindexOptions{
		ifMissing:        *ifMissing,
		restoreOperation: *restoreOperation,
		expectedOldEpoch: *expectedOldEpoch,
		newEpoch:         *newEpoch,
	}
	if err := validateReindexOptions(options); err != nil {
		logging.Module(logger, "search").Error("search reindex arguments invalid", slog.String("reason", "invalid_arguments"))
		return reindexOptions{}, err
	}
	return options, nil
}

func runWithOptions(cfg config.ReindexConfig, options reindexOptions, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("search-reindex")
	}
	searchLogger := logging.Module(logger, "search")
	if err := validateReindexElasticsearch(cfg); err != nil {
		searchLogger.Error("search reindex initialization failed", slog.String("stage", "elasticsearch"), slog.String("reason", "wrong_client_purpose"))
		return err
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
	state, err := businessstate.NewRepository(database, businessstate.WithOpenRequirementsLimit(cfg.BusinessAsyncMaxOpenRequirements), businessstate.WithRetentionLimits(cfg.BusinessAsyncMaxRetainedGroups, cfg.BusinessAsyncMaxRetainedBytes))
	if err != nil {
		return errors.New("create business state repository")
	}
	environment, err := state.Ensure(ctx)
	if err != nil {
		return errors.New("initialize business state")
	}
	searchLogger.Info("search reindex started", slog.Int("batch_size", cfg.Elasticsearch.ReindexBatch))
	client := search.NewElasticsearchRepository(elasticsearch)
	reindexer := search.NewReindexer(
		search.NewReindexStore(database),
		client,
		cfg.Elasticsearch.ReindexBatch,
	)
	if options.restoreOperation != "" {
		if err := state.BeginRestore(ctx, options.restoreOperation, options.expectedOldEpoch, options.newEpoch); err != nil {
			return errors.New("begin business epoch restore")
		}
		if err := verifyOldEpochRejected(ctx, state, options.expectedOldEpoch); err != nil {
			return err
		}
		result, err := reindexer.RunForEpoch(ctx, false, options.newEpoch)
		if err != nil {
			return fmt.Errorf("search reindex operation failed: %w", err)
		}
		if result.Generation == "" {
			return errors.New("search reindex did not produce a generation")
		}
		actual, err := client.ResolveGenerationForEpoch(ctx, options.newEpoch)
		if err != nil || actual != result.Generation {
			return errors.New("search restore generation verification failed")
		}
		if err := state.MarkReady(ctx, options.restoreOperation, options.newEpoch); err != nil {
			return errors.New("mark business epoch ready")
		}
		if err := verifyNewEpochAccepted(ctx, state, options.newEpoch); err != nil {
			return err
		}
		searchLogger.Info("search reindex restore completed",
			slog.String("result", "ready"),
			slog.String("generation", result.Generation),
			slog.String("data_epoch", options.newEpoch),
			slog.Uint64("document_count", result.DocumentCount),
		)
		return nil
	}
	result, err := reindexer.RunForEpoch(ctx, options.ifMissing, environment.DataEpoch)
	if err != nil {
		return fmt.Errorf("search reindex operation failed: %w", err)
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

func verifyOldEpochRejected(ctx context.Context, state *businessstate.Repository, oldEpoch string) error {
	event, err := bus.NewV2PostDeleted(time.Now().UTC(), oldEpoch, 1, 1)
	if err != nil {
		return errors.New("construct old epoch verification event")
	}
	decision, err := state.CheckEnvelope(ctx, event)
	if err != nil || decision != bus.AdmissionPark {
		return errors.New("old business epoch was not rejected")
	}
	return nil
}

func verifyNewEpochAccepted(ctx context.Context, state *businessstate.Repository, epoch string) error {
	event, err := bus.NewV2PostLiked(time.Now().UTC(), epoch, 1, 2, 1)
	if err != nil {
		return errors.New("construct new epoch verification event")
	}
	decision, err := state.CheckEnvelope(ctx, event)
	if err != nil || decision != bus.AdmissionAccept {
		return errors.New("new business epoch was not accepted")
	}
	return nil
}

func validateReindexOptions(options reindexOptions) error {
	provided := options.restoreOperation != "" || options.expectedOldEpoch != "" || options.newEpoch != ""
	if !provided {
		return nil
	}
	if options.ifMissing {
		return errors.New("--if-missing cannot be combined with restore preparation")
	}
	if err := businessstate.ValidateOperationID(options.restoreOperation); err != nil {
		return err
	}
	if err := businessstate.ValidateEpoch(options.expectedOldEpoch); err != nil {
		return err
	}
	if err := businessstate.ValidateEpoch(options.newEpoch); err != nil {
		return err
	}
	if options.expectedOldEpoch == options.newEpoch {
		return errors.New("restore epochs must differ")
	}
	return nil
}

func validateReindexElasticsearch(cfg config.ReindexConfig) error {
	if cfg.Elasticsearch.Purpose != "search" {
		return errors.New("search reindex requires the business search Elasticsearch client")
	}
	return nil
}
