package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/config"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/consumer"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/elasticsearch"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/httpserver"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/logging"
	logtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/logs"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/metrics"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/victoriametrics"
)

type processorLogger struct{ logger *slog.Logger }

func (l processorLogger) Permanent(r consumer.Record, code string) {
	l.logger.Warn("record permanently rejected", "module", "transform", "event", "record_rejected", "reason_code", code, "topic", r.Topic, "partition", r.Partition, "offset", r.Offset)
}
func (l processorLogger) Transient(r consumer.Record) {
	l.logger.Warn("storage write will retry", "module", "storage", "event", "write_retry", "topic", r.Topic, "partition", r.Partition, "offset", r.Offset)
}
func (l processorLogger) Accepted(r consumer.Record) {
	l.logger.Info("record accepted and committed", "module", "consumer", "event", "record_committed", "topic", r.Topic, "partition", r.Partition, "offset", r.Offset)
}

type storageReadiness struct {
	vm, logs interface{ Ready(context.Context) error }
}

func (s storageReadiness) Ready(ctx context.Context) error {
	if s.vm.Ready(ctx) != nil {
		return errors.New("VictoriaMetrics unavailable")
	}
	return s.logs.Ready(ctx)
}

func main() {
	logger := logging.New("marshaller", os.Stdout)
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "module", "lifecycle", "event", "startup_failed")
		os.Exit(1)
	}
	ownership := consumer.NewOwnership()
	kafka, err := consumer.NewKafka(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaGroup, cfg.KafkaCommitTimeout, ownership)
	if err != nil {
		logger.Error("Kafka client initialization failed", "module", "consumer", "event", "startup_failed")
		os.Exit(1)
	}
	vm := victoriametrics.New(cfg.VMURL, cfg.VMUsername, cfg.VMPassword, cfg.VMTimeout)
	logStore, err := elasticsearch.New(cfg.ElasticsearchURL, cfg.ElasticsearchTimeout)
	if err != nil {
		logger.Error("Elasticsearch client initialization failed", "module", "storage", "event", "startup_failed")
		os.Exit(1)
	}
	processor := &consumer.Processor{
		Decoder: envelope.Decoder{MaxBytes: cfg.MaxRecordBytes, FutureSkew: cfg.FutureSkew},
		Targets: map[string]consumer.Target{
			"metrics/redis":        {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"logs/backend":         {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes}, Writer: logStore},
			"logs/business-worker": {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes}, Writer: logStore},
			"logs/search-indexer":  {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes}, Writer: logStore},
			"logs/search-reindex":  {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes}, Writer: logStore},
		},
		Committer: kafka, RetryMin: cfg.RetryMin, RetryMax: cfg.RetryMax, Logger: processorLogger{logger},
	}
	server := httpserver.New(cfg.HTTPHost, cfg.HTTPPort, cfg.APIToken, cfg.ReadinessTimeout, kafka, storageReadiness{vm: vm, logs: logStore}, logger)
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErrors := make(chan error, 1)
	consumerDone := make(chan error, 1)
	go func() {
		logger.Info("marshaller listening", "module", "http", "event", "started", "address", cfg.HTTPHost)
		serveErrors <- server.ListenAndServe()
	}()
	go func() {
		consumerDone <- kafka.Run(rootCtx, processor, func(message string, args ...any) { logger.Warn(message, args...) })
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	exitCode := 0
	running := true
	for running {
		select {
		case sig := <-signals:
			logger.Info("shutdown requested", "module", "lifecycle", "event", "stopping", "signal", sig.String())
			running = false
		case err := <-serveErrors:
			if !errors.Is(err, http.ErrServerClosed) {
				logger.Error("HTTP server failed", "module", "http", "event", "server_failed")
				exitCode = 1
			}
			running = false
		case err := <-consumerDone:
			if err != nil {
				logger.Error("consumer partition halted", "module", "consumer", "event", "consumer_halted")
				exitCode = 1
			}
			consumerDone = nil
		}
	}
	cancel()
	ownership.CancelAll()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown failed", "module", "http", "event", "shutdown_failed")
		exitCode = 1
	}
	if err := kafka.Close(shutdownCtx); err != nil {
		logger.Error("Kafka shutdown failed", "module", "consumer", "event", "shutdown_failed")
		exitCode = 1
	}
	logger.Info("marshaller stopped", "module", "lifecycle", "event", "stopped")
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
