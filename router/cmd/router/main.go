package main

import (
	"context"
	"errors"
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
	producer, err := kafkaclient.New(kafkaclient.Config{
		Brokers: cfg.KafkaBrokers, ProduceTimeout: cfg.KafkaProduceTimeout,
		MaxBufferedRecords: cfg.KafkaMaxBufferedRecords, MaxBufferedBytes: cfg.KafkaMaxBufferedBytes,
	})
	if err != nil {
		logger.Error("Kafka client initialization failed", "event", "startup_failed")
		os.Exit(1)
	}
	server := httpserver.New(cfg, producer, logger)
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("router listening", "event", "started")
		serveErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	var exitCode int
	select {
	case sig := <-signals:
		logger.Info("shutdown requested", "event", "stopping", "signal", sig.String())
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "event", "server_failed")
			exitCode = 1
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown failed", "event", "shutdown_failed")
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
