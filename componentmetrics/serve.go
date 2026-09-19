package componentmetrics

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ServeRuntime adds process probes to an existing listener and spends one
// shutdown budget. Exporters have no source dependency in their readiness:
// source failure is represented by their existing metrics, not process health.
func ServeRuntime(ctx context.Context, server *http.Server, listener net.Listener, budget time.Duration, logs ...*slog.Logger) (result error) {
	if ctx == nil || server == nil || listener == nil || budget <= 0 {
		return errors.New("invalid runtime configuration")
	}
	var logger *slog.Logger
	if len(logs) > 0 {
		logger = logs[0]
	}
	if logger != nil {
		logger.Info("exporter started", "listen", server.Addr)
		defer func() {
			if result != nil {
				logger.Error("exporter stopped", "reason", "runtime_failed")
			} else {
				logger.Info("exporter stopped", "reason", "shutdown_complete")
			}
		}()
	}
	probes, err := NewProbes(ctx, time.Second, 250*time.Millisecond, nil)
	if err != nil {
		return err
	}
	server.Handler = probes.Wrap(HTTP(server.Handler, logger))
	release := BindShutdown(ctx, budget)
	defer release()
	probes.Started()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		probes.Stop()
	case err := <-done:
		probes.Stop()
		_ = server.Close()
		if err == nil {
			return errors.New("server_stopped")
		}
		return errors.New("server_failed")
	}
	shutdown, cancel := ShutdownContext(budget)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		<-done
		return errors.New("shutdown_timeout")
	}
	if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("server_failed")
	}
	return nil
}
