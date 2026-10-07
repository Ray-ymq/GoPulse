package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"os"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logship"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/tracing"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	goredis "github.com/redis/go-redis/v9"
	redislogging "github.com/redis/go-redis/v9/logging"
)

const (
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 1 << 20
)

func main() {
	defer componentmetrics.ReleaseShutdown()
	stdoutLogger := logging.New("backend", os.Stdout)
	cfg, err := config.Load()
	if err != nil {
		logging.Module(stdoutLogger, "lifecycle").Error("backend stopped", slog.String("reason", "invalid_configuration"))
		os.Exit(1)
	}
	logger := stdoutLogger
	var shipper *logship.Shipper
	if cfg.LogShip.Enabled() {
		shipper, err = logship.New(logship.Config{
			Endpoint: cfg.LogShip.Endpoint, Token: cfg.LogShip.Token, RequestTimeout: cfg.LogShip.RequestTimeout,
			QueueCapacity: cfg.LogShip.QueueCapacity, RetryMin: cfg.LogShip.RetryMin, RetryMax: cfg.LogShip.RetryMax,
			ShutdownTimeout: cfg.LogShip.ShutdownTimeout,
		}, logging.Module(stdoutLogger, "logship"))
		if err != nil {
			logging.Module(stdoutLogger, "lifecycle").Error("backend stopped", slog.String("reason", "invalid_log_shipper"))
			os.Exit(1)
		}
		logger = logging.NewWithSink("backend", os.Stdout, shipper)
	}
	if err = run(cfg, logger); err != nil {
		logging.Module(logger, "lifecycle").Error("backend stopped", slog.String("reason", "process_failed"))
		if shipper != nil {
			ctx, cancel := componentmetrics.ShutdownContext(cfg.LogShip.ShutdownTimeout)
			_ = shipper.Close(ctx)
			cancel()
		}
		os.Exit(1)
	}
	if shipper != nil {
		ctx, cancel := componentmetrics.ShutdownContext(cfg.LogShip.ShutdownTimeout)
		if err := shipper.Close(ctx); err != nil {
			logging.Module(stdoutLogger, "logship").Warn("log shipper shutdown incomplete", slog.String("reason", "shutdown_timeout"))
			cancel()
			os.Exit(1)
		}
		cancel()
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	if _, err := profileForRole(cfg.ServiceRole); err != nil {
		return err
	}
	if logger == nil {
		logger = logging.Discard("backend")
	}
	lifecycleLogger := logging.Module(logger, "lifecycle")
	traceProvider, err := tracing.New(context.Background(), tracing.Config{
		Enabled: cfg.Trace.Enabled, Endpoint: cfg.Trace.Endpoint, ServiceName: cfg.Trace.ServiceName,
		SampleRatio: cfg.Trace.SampleRatio, QueueCapacity: cfg.Trace.QueueCapacity, BatchSize: cfg.Trace.BatchSize,
		BatchTimeout: cfg.Trace.BatchTimeout, ExportTimeout: cfg.Trace.ExportTimeout, ShutdownTimeout: cfg.Trace.ShutdownTimeout,
	})
	if err != nil {
		return errors.New("initialize tracing")
	}
	defer func() {
		traceShutdownTimeout := cfg.Trace.ShutdownTimeout
		if traceShutdownTimeout <= 0 {
			traceShutdownTimeout = tracing.DefaultShutdownTimeout
		}
		shutdownContext, cancel := context.WithTimeout(context.Background(), traceShutdownTimeout)
		defer cancel()
		if err := traceProvider.Shutdown(shutdownContext); err != nil {
			lifecycleLogger.Warn("trace exporter shutdown incomplete", slog.String("reason", "shutdown_timeout"))
		}
	}()

	metrics, err := componentmetrics.NewBackend(componentmetrics.BackendRoutes())
	if err != nil {
		return err
	}
	metrics.SetHTTPConcurrencyLimit(cfg.HTTPMaxConcurrency)
	componentmetrics.InstallBackend(metrics)
	defer componentmetrics.InstallBackend(nil)
	goredis.SetLogger(&redislogging.VoidLogger{})
	if err := backendhttp.ConfigureGinMode(cfg.AppEnv); err != nil {
		return fmt.Errorf("configure Gin mode: %w", err)
	}

	signalContext, stopSignals := componentmetrics.SignalContext()
	defer stopSignals()
	assembly, err := newRoleAssembly(cfg, logger, signalContext)
	if err != nil {
		return err
	}
	defer assembly.close(lifecycleLogger)
	assembly.initializeMetrics(metrics)

	componentmetrics.BindShutdown(signalContext, shutdownTimeout)
	internalMetrics, err := componentmetrics.StartConfiguredWithProbes(signalContext, "backend", metrics.Snapshot, assembly.probes)
	if err != nil {
		return err
	}

	alertContext, cancelAlerts := context.WithCancel(signalContext)
	alertDone := make(chan struct{})
	go func() {
		defer close(alertDone)
		if assembly.runAlertCheck != nil {
			assembly.runAlertCheck(alertContext)
		}
	}()
	defer func() {
		cancelAlerts()
		select {
		case <-alertDone:
		case <-time.After(3 * time.Second):
		}
	}()

	sampleContext, cancelSample := context.WithCancel(signalContext)
	sampleDone := make(chan struct{})
	go func() {
		defer close(sampleDone)
		if assembly.sampleOutbox != nil {
			assembly.sampleOutbox(sampleContext, metrics)
		}
	}()
	defer func() {
		cancelSample()
		shutdownContext, cancel := componentmetrics.ShutdownContext(shutdownTimeout)
		defer cancel()
		_ = internalMetrics.Shutdown(shutdownContext)
		select {
		case <-sampleDone:
		case <-shutdownContext.Done():
		}
	}()

	assembly.probes.Started()
	server := newHTTPServer(cfg.HTTPAddress(), assembly.router)
	return serveWithDispatcher(signalContext, server, assembly.dispatcher, lifecycleLogger)
}

func newHTTPServer(address string, handler stdhttp.Handler) *stdhttp.Server {
	return &stdhttp.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func serveWithDispatcher(ctx context.Context, server *stdhttp.Server, dispatcher *outbox.Dispatcher, logger *slog.Logger) error {
	if ctx == nil {
		return errors.New("serve backend: context is required")
	}
	if server == nil {
		return errors.New("serve backend: HTTP server is required")
	}
	if dispatcher == nil {
		return serveLogged(ctx, server, server.ListenAndServe, logger)
	}

	dispatcherContext, cancelDispatcher := context.WithCancel(ctx)
	dispatcherErrors := make(chan error, 1)
	go func() {
		dispatcherErrors <- dispatcher.Run(dispatcherContext)
	}()

	serverErr := serveLogged(ctx, server, server.ListenAndServe, logger)
	cancelDispatcher()

	shutdownContext, cancelShutdown := componentmetrics.ShutdownContext(shutdownTimeout)
	defer cancelShutdown()
	select {
	case dispatcherErr := <-dispatcherErrors:
		if dispatcherErr != nil && !errors.Is(dispatcherErr, context.Canceled) {
			if serverErr != nil {
				return fmt.Errorf("%v; outbox dispatcher shutdown failed: %w", serverErr, dispatcherErr)
			}
			return fmt.Errorf("outbox dispatcher shutdown failed: %w", dispatcherErr)
		}
	case <-shutdownContext.Done():
		if serverErr != nil {
			return fmt.Errorf("%v; outbox dispatcher shutdown timed out", serverErr)
		}
		return errors.New("outbox dispatcher shutdown timed out")
	}
	return serverErr
}

func serve(ctx context.Context, server *stdhttp.Server, startServer func() error) error {
	return serveLogged(ctx, server, startServer, logging.Module(logging.Discard("backend"), "lifecycle"))
}

func serveLogged(ctx context.Context, server *stdhttp.Server, startServer func() error, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Module(logging.Discard("backend"), "lifecycle")
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- startServer()
	}()

	logger.Info("backend listening", "listen", server.Addr)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			logger.Error("backend server failed", slog.String("reason", "listen_failed"))
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		logger.Info("backend stopped", slog.String("reason", "server_closed"))
		return nil
	case <-ctx.Done():
		logger.Info("backend shutdown started")
		shutdownContext, cancel := componentmetrics.ShutdownContext(shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			logger.Error("backend shutdown failed", slog.String("reason", "shutdown_failed"))
			return fmt.Errorf("HTTP server shutdown failed: %w", err)
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			logger.Error("backend shutdown failed", slog.String("reason", "server_failed"))
			return fmt.Errorf("HTTP server failed during shutdown: %w", err)
		}
		logger.Info("backend stopped", slog.String("reason", "shutdown_complete"))
		return nil
	}
}
