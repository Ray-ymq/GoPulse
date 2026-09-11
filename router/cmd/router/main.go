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

	"github.com/Ray-ymq/GoPulse/router/internal/config"
	"github.com/Ray-ymq/GoPulse/router/internal/httpserver"
	kafkaclient "github.com/Ray-ymq/GoPulse/router/internal/kafka"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "router")
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "event", "startup_failed")
		os.Exit(1)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
	internalMetrics, err := componentmetrics.StartConfigured(rootCtx, "router", func() ([]byte, bool) { return producer.Snapshot(metrics) })
	if err != nil {
		logger.Error("metrics listener initialization failed", "event", "startup_failed")
		os.Exit(1)
	}
	server := httpserver.New(cfg, producer, logger)
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("router listening", "event", "started")
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
