package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
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
	stdoutLogger := logging.New("backend", os.Stdout)
	cfg, err := config.Load()
	if err != nil {
		logging.Module(stdoutLogger, "lifecycle").Error("backend stopped", slog.String("reason", "invalid_configuration"))
		os.Exit(1)
	}
	if err = run(cfg, stdoutLogger); err != nil {
		logging.Module(stdoutLogger, "lifecycle").Error("backend stopped", slog.String("reason", "process_failed"), slog.String("error", err.Error()))
		os.Exit(1)
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
	goredis.SetLogger(&redislogging.VoidLogger{})
	if err := backendhttp.ConfigureGinMode(cfg.AppEnv); err != nil {
		return fmt.Errorf("configure Gin mode: %w", err)
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	assembly, err := newRoleAssembly(cfg, logger, signalContext)
	if err != nil {
		return err
	}
	defer assembly.close(lifecycleLogger)

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

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
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
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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
