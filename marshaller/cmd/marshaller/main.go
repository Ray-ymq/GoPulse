package main

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/marshaller/internal/config"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/consumer"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/elasticsearch"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	eventtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/events"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/httpserver"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/logging"
	logtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/logs"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/metrics"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/retention"
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
	vm, logs, events interface{ Ready(context.Context) error }
}

func (s storageReadiness) Ready(ctx context.Context) error {
	if s.vm.Ready(ctx) != nil {
		return errors.New("VictoriaMetrics unavailable")
	}
	if s.logs.Ready(ctx) != nil {
		return errors.New("Elasticsearch logs unavailable")
	}
	return s.events.Ready(ctx)
}

func main() {
	logger := logging.New("marshaller", os.Stdout)
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "module", "lifecycle", "event", "startup_failed")
		os.Exit(1)
	}
	ownership := consumer.NewOwnership()
	kafka, err := consumer.NewKafkaWithOptions(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaGroup, cfg.KafkaCommitTimeout, cfg.KafkaMinPartitions, cfg.MaxInFlight, ownership)
	if err != nil {
		logger.Error("Kafka client initialization failed", "module", "consumer", "event", "startup_failed")
		os.Exit(1)
	}
	vm := victoriametrics.New(cfg.VMURL, cfg.VMUsername, cfg.VMPassword, cfg.VMTimeout)
	var logStore *elasticsearch.Client
	if cfg.RuntimeMode == config.RuntimeModeContainer {
		logStore, err = elasticsearch.NewContainer(cfg.ElasticsearchURL, cfg.ElasticsearchTimeout)
	} else {
		logStore, err = elasticsearch.New(cfg.ElasticsearchURL, cfg.ElasticsearchTimeout)
	}
	if err != nil {
		logger.Error("Elasticsearch client initialization failed", "module", "storage", "event", "startup_failed")
		os.Exit(1)
	}
	var eventStore *elasticsearch.EventsClient
	if cfg.RuntimeMode == config.RuntimeModeContainer {
		eventStore, err = elasticsearch.NewEventsContainer(cfg.ElasticsearchURL, cfg.ElasticsearchTimeout)
	} else {
		eventStore, err = elasticsearch.NewEvents(cfg.ElasticsearchURL, cfg.ElasticsearchTimeout)
	}
	if err != nil {
		logger.Error("Elasticsearch events client initialization failed", "module", "storage", "event", "startup_failed")
		os.Exit(1)
	}
	if err := logStore.SetRetentionPolicy(cfg.Retention.Logs); err != nil {
		logger.Error("log retention configuration invalid", "module", "lifecycle", "event", "startup_failed")
		os.Exit(1)
	}
	if err := eventStore.SetRetentionPolicy(cfg.Retention.Events); err != nil {
		logger.Error("event retention configuration invalid", "module", "lifecycle", "event", "startup_failed")
		os.Exit(1)
	}
	retentionStore, err := retention.NewElasticsearch(cfg.ElasticsearchURL, cfg.ElasticsearchTimeout, cfg.RuntimeMode == config.RuntimeModeContainer)
	if err != nil {
		logger.Error("retention Elasticsearch client initialization failed", "module", "lifecycle", "event", "startup_failed")
		os.Exit(1)
	}
	retentionRunner, err := retention.NewRunner(retentionStore, cfg.Retention)
	if err != nil {
		logger.Error("retention lifecycle initialization failed", "module", "lifecycle", "event", "startup_failed")
		os.Exit(1)
	}
	retentionRunner.SetReportObserver(func(report retention.Report, roundErr error) {
		if roundErr != nil {
			logger.Warn("retention cleanup round failed", "module", "lifecycle", "event", "retention_cleanup_failed", "reason", "dependency_or_budget")
			return
		}
		for _, item := range report.Items {
			if item.Result == "deleted" || item.Result == "not_found" || item.Result == "not_expired" {
				continue
			}
			logger.Warn("retention cleanup item was not removed", "module", "lifecycle", "event", "retention_cleanup_blocked", "reason", item.Result)
		}
	})
	processor := &consumer.Processor{
		Decoder: envelope.Decoder{MaxBytes: cfg.MaxRecordBytes, FutureSkew: cfg.FutureSkew},
		Targets: map[string]consumer.Target{
			"metrics/mysql":           {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"metrics/rabbitmq":        {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"metrics/kafka":           {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"metrics/elasticsearch":   {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"metrics/victoriametrics": {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"metrics/redis":           {Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm},
			"logs/backend":            {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes, Retention: &cfg.Retention.Logs}, Writer: logStore},
			"logs/business-worker":    {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes, Retention: &cfg.Retention.Logs}, Writer: logStore},
			"logs/search-indexer":     {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes, Retention: &cfg.Retention.Logs}, Writer: logStore},
			"logs/search-reindex":     {Transformer: logtransform.Transformer{MaxBytes: cfg.MaxRecordBytes, Retention: &cfg.Retention.Logs}, Writer: logStore},
			"events/monitor":          {Transformer: eventtransform.Transformer{MaxBytes: 16 * 1024, Retention: &cfg.Retention.Events}, Writer: eventStore},
		},
		Committer: kafka, RetryMin: cfg.RetryMin, RetryMax: cfg.RetryMax, MaxRetrying: cfg.MaxRetrying, Logger: processorLogger{logger},
	}
	for _, id := range componentmetrics.Components {
		processor.Targets["metrics/"+id] = consumer.Target{Transformer: metrics.Transformer{MaxBytes: cfg.MaxOutputBytes}, Writer: vm}
	}
	state, err := componentmetrics.New("marshaller")
	if err != nil {
		logger.Error("metrics initialization failed")
		os.Exit(1)
	}
	componentmetrics.Install(state)
	server := httpserver.New(cfg.HTTPHost, cfg.HTTPPort, cfg.APIToken, cfg.ReadinessTimeout, kafka, storageReadiness{vm: vm, logs: logStore, events: eventStore}, logger)
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	releaseBudget := componentmetrics.BindShutdown(rootCtx, cfg.ShutdownTimeout)
	defer releaseBudget()
	probes, _ := componentmetrics.NewProbes(rootCtx, cfg.ReadinessTimeout, 250*time.Millisecond, server.Check)
	server.SetProbes(probes)
	internalMetrics, err := componentmetrics.StartConfiguredWithProbes(rootCtx, "marshaller", state.Snapshot, probes)
	if err != nil {
		logger.Error("metrics listener initialization failed", "event", "startup_failed")
		os.Exit(1)
	}
	probes.Started()
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		retentionRunner.Run(rootCtx)
	}()
	serveErrors := make(chan error, 1)
	consumerDone := make(chan error, 1)
	go func() {
		logger.Info("marshaller listening", "module", "http", "event", "started", "address", cfg.HTTPHost, "instance_id", cfg.InstanceID, "replica_count", cfg.ReplicaCount)
		serveErrors <- server.ListenAndServe()
	}()
	go func() {
		consumerDone <- kafka.Run(rootCtx, processor, func(message string, args ...any) { logger.Warn(message, args...) })
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
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
			running = false
			consumerDone = nil
		}
	}
	signal.Stop(signals)
	probes.Stop()
	cancel()
	ownership.CancelAll()
	shutdownCtx, shutdownCancel := componentmetrics.ShutdownContext(cfg.ShutdownTimeout)
	defer shutdownCancel()
	select {
	case <-retentionDone:
	case <-shutdownCtx.Done():
		exitCode = 1
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown failed", "module", "http", "event", "shutdown_failed")
		exitCode = 1
	}
	if err := internalMetrics.Shutdown(shutdownCtx); err != nil {
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
