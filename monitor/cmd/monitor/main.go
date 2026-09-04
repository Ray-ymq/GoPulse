package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/config"
	"github.com/Ray-ymq/GoPulse/monitor/internal/httpserver"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/collector"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "monitor")
	if err := run(logger); err != nil {
		logger.Error("monitor stopped", "error_code", "monitor_runtime_failed")
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var messagePublisher publisher.Transport = publisher.Discard{}
	if cfg.RouterURL != "" {
		messagePublisher, err = publisher.NewHTTP(cfg.RouterURL, cfg.RouterToken, cfg.PublishTimeout)
		if err != nil {
			return err
		}
	}
	manager, err := plugin.NewManager(ctx, plugin.ManagerConfig{Root: cfg.PluginRoot, ExporterEnv: cfg.ExporterEnv, HealthURL: cfg.ExporterHealthURL(), StartupTimeout: cfg.StartupTimeout, StopTimeout: cfg.StopTimeout})
	if err != nil {
		return err
	}
	metricsMonitor, err := collector.New(collector.Config{
		Host: cfg.ExporterEnv["REDIS_EXPORTER_HTTP_HOST"], Port: cfg.ExporterEnv["REDIS_EXPORTER_HTTP_PORT"],
		Interval: cfg.ScrapeInterval, Timeout: cfg.ScrapeTimeout, PublishTimeout: cfg.PublishTimeout,
		Publisher: messagePublisher,
		Update: func(update collector.Update) {
			manager.RecordMetrics(update.ScrapeAt, update.SuccessAt, update.ErrorCode, update.ErrorMessage)
		},
	})
	if err != nil {
		return err
	}
	manager.AttachMetrics(metricsMonitor)
	handler := httpserver.New(cfg.APIToken, cfg.PluginRoot, manager, logger, httpserver.LogOptions{Token: cfg.LogIngestToken, MaxBytes: cfg.LogMaxBytes, FutureSkew: cfg.LogFutureSkew, Publisher: messagePublisher})
	server := &http.Server{Addr: cfg.HTTPAddress(), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.RequestTimeout, WriteTimeout: cfg.RequestTimeout, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 1 << 20}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logger.Info("monitor listening")
	select {
	case err = <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		serverErr := server.Shutdown(shutdownCtx)
		managerErr := manager.Shutdown(shutdownCtx)
		if serverErr != nil {
			return serverErr
		}
		return managerErr
	}
}
