package main

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/config"
	"github.com/Ray-ymq/GoPulse/monitor/internal/events"
	"github.com/Ray-ymq/GoPulse/monitor/internal/httpserver"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/collector"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "plugin-state" {
		// Offline transfer must never accidentally print credentials to a terminal
		// or a regular file. The lifecycle consumes a pipe and seals private state.
		stream := os.Stdout
		if len(os.Args) > 2 && os.Args[2] == "import" {
			stream = os.Stdin
		}
		info, err := stream.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			_, _ = os.Stderr.WriteString("plugin-state requires a private lifecycle pipe\n")
			os.Exit(2)
		}
		if err = plugin.RunPortableTransfer(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			_, _ = os.Stderr.WriteString("plugin-state transfer failed; stop Monitor and verify the empty target and trusted catalog\n")
			os.Exit(1)
		}
		return
	}

	logger := componentmetrics.NewLogger("monitor", os.Stdout)
	slog.SetDefault(logger)
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
	ctx, stop := componentmetrics.SignalContext()
	defer stop()
	releaseBudget := componentmetrics.BindShutdown(ctx, cfg.ShutdownTimeout)
	defer releaseBudget()
	state, err := componentmetrics.New("monitor")
	if err != nil {
		return err
	}
	componentmetrics.Install(state)
	defer componentmetrics.Install(nil)
	var manager *plugin.Manager
	probes, _ := componentmetrics.NewProbes(ctx, time.Second, 250*time.Millisecond, func(checkCtx context.Context) error {
		if manager == nil {
			return errors.New("local_state_unavailable")
		}
		return manager.RuntimeReady(checkCtx)
	})
	internalMetrics, err := componentmetrics.StartConfiguredWithProbes(ctx, "monitor", state.Snapshot, probes)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := componentmetrics.ShutdownContext(cfg.ShutdownTimeout)
		defer cancel()
		_ = internalMetrics.Shutdown(shutdown)
	}()
	var messagePublisher publisher.Transport = publisher.Discard{}
	if cfg.RouterURL != "" {
		messagePublisher, err = publisher.NewHTTP(cfg.RouterURL, cfg.RouterToken, cfg.PublishTimeout)
		if err != nil {
			return err
		}
	}
	eventMonitor, err := events.NewMonitor(events.Config{Capacity: cfg.EventQueueCapacity, Timeout: cfg.PublishTimeout, RetryMin: cfg.EventRetryMin, RetryMax: cfg.EventRetryMax, Sender: messagePublisher, Logger: logger})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := componentmetrics.ShutdownContext(cfg.EventShutdownTimeout)
		_ = eventMonitor.Close(closeCtx)
		cancel()
	}()
	manager, err = plugin.NewManager(ctx, plugin.ManagerConfig{Root: cfg.PluginRoot, ValidateSnapshot: collector.ValidateSuccessfulSnapshot, ValidateSourceSnapshot: collector.ValidateSuccessfulSourceSnapshot, ExporterEnv: cfg.ExporterEnv, HealthURL: cfg.ExporterHealthURL(), StartupTimeout: cfg.StartupTimeout, StopTimeout: cfg.StopTimeout, EventRecorder: eventMonitor})
	if err != nil {
		return err
	}
	for _, entry := range plugin.OfficialCatalog() {
		if !entry.Available {
			continue
		}
		metricsMonitor, err := collector.New(collector.Config{Source: entry.Source, Host: "127.0.0.1", Port: strconv.Itoa(entry.Port), Interval: cfg.ScrapeInterval, Timeout: cfg.ScrapeTimeout, PublishTimeout: cfg.PublishTimeout, Publisher: messagePublisher, Events: eventMonitor, Update: func(update collector.Update) {
			manager.RecordSourceMetrics(entry.ID, update.ScrapeAt, update.SuccessAt, update.ErrorCode, update.ErrorMessage)
		}})
		if err != nil {
			return err
		}
		manager.AttachSourceMetrics(entry.ID, metricsMonitor)
	}

	componentCollectors, err := collector.StartComponents(ctx, componentmetrics.Mode(), os.Getenv("GOPULSE_VERSION"), cfg.ScrapeInterval, cfg.ScrapeTimeout, cfg.PublishTimeout, messagePublisher, logger)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := componentmetrics.ShutdownContext(cfg.ShutdownTimeout)
		defer cancel()
		_ = componentCollectors.Shutdown(shutdown)
	}()
	if cfg.BootstrapPackage != "" {
		if _, err = manager.Bootstrap(ctx, cfg.BootstrapPackage); err != nil {
			logger.Warn("plugin bootstrap unavailable", "reason", "plugin_operation_failed")
		}
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count := 0
				for _, status := range manager.List() {
					if status.ObservedState == "running" {
						count++
					}
				}
				state.Set("plugins_running", float64(count))
			}
		}
	}()
	handler := httpserver.New(cfg.APIToken, cfg.PluginRoot, manager, logger, httpserver.LogOptions{Token: cfg.LogIngestToken, MaxBytes: cfg.LogMaxBytes, FutureSkew: cfg.LogFutureSkew, Publisher: messagePublisher})
	server := &http.Server{Addr: cfg.HTTPAddress(), Handler: probes.Wrap(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.RequestTimeout, WriteTimeout: cfg.RequestTimeout, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 1 << 20}
	probes.Started()
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	logger.Info("monitor listening", "listen", cfg.HTTPAddress())
	select {
	case err = <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		probes.Stop()
		shutdownCtx, cancel := componentmetrics.ShutdownContext(cfg.ShutdownTimeout)
		defer cancel()
		serverErr := server.Shutdown(shutdownCtx)
		if serverErr != nil {
			_ = server.Close()
		}
		managerErr := manager.Shutdown(shutdownCtx)
		eventCtx, eventCancel := componentmetrics.ShutdownContext(cfg.EventShutdownTimeout)
		eventErr := eventMonitor.Close(eventCtx)
		eventCancel()
		if serverErr != nil {
			return serverErr
		}
		if managerErr != nil {
			return managerErr
		}
		return eventErr
	}
}
