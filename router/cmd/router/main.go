package main

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Ray-ymq/GoPulse/router/internal/config"
	"github.com/Ray-ymq/GoPulse/router/internal/httpserver"
	kafkaclient "github.com/Ray-ymq/GoPulse/router/internal/kafka"
)

func main() {
	logger := componentmetrics.NewLogger("router", os.Stdout)
	slog.SetDefault(logger)
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "event", "startup_failed")
		os.Exit(1)
	}
	rootCtx, stop := componentmetrics.SignalContext()
	defer stop()
	releaseBudget := componentmetrics.BindShutdown(rootCtx, cfg.ShutdownTimeout)
	defer releaseBudget()
	metrics, err := componentmetrics.New("router")
	if err != nil {
		logger.Error("metrics initialization failed")
		return
	}
	componentmetrics.Install(metrics)
	producer, err := kafkaclient.New(kafkaclient.Config{
		Brokers: cfg.KafkaBrokers, ProduceTimeout: cfg.KafkaProduceTimeout,
		MaxBufferedRecords: cfg.KafkaMaxBufferedRecords, MaxBufferedBytes: cfg.KafkaMaxBufferedBytes,
	})
	if err != nil {
		logger.Error("Kafka client initialization failed", "event", "startup_failed")
		os.Exit(1)
	}
	probes, _ := componentmetrics.NewProbes(rootCtx, cfg.RequestTimeout, 250*time.Millisecond, func(ctx context.Context) error { return producer.Ready(ctx, config.Topic) })
	internalMetrics, err := componentmetrics.StartConfiguredWithProbes(rootCtx, "router", func() ([]byte, bool) { return producer.Snapshot(metrics) }, probes)
	if err != nil {
		logger.Error("metrics listener initialization failed", "event", "startup_failed")
		os.Exit(1)
	}
	server := httpserver.New(cfg, producer, logger)
	server.SetProbes(probes)
	probes.Started()
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("router listening", "event", "started", "listen", cfg.Address())
		serveErrors <- server.ListenAndServe()
	}()

	var exitCode int
	select {
	case <-rootCtx.Done():
		logger.Info("shutdown requested", "event", "stopping")
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "event", "server_failed")
			exitCode = 1
		}
	}

	probes.Stop()
	shutdownCtx, cancel := componentmetrics.ShutdownContext(cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown failed", "event", "shutdown_failed")
		exitCode = 1
	}
	if err := internalMetrics.Shutdown(shutdownCtx); err != nil {
		exitCode = 1
	}
	if err := producer.Close(shutdownCtx); err != nil {
		logger.Error("Kafka shutdown failed", "event", "shutdown_failed")
		exitCode = 1
	}
	logger.Info("router stopped", "event", "stopped")
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
