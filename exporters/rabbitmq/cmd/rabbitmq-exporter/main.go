package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/rabbitmq/internal/collector"
	runtime "github.com/Ray-ymq/GoPulse/exporters/rabbitmq/internal/runtime"
)

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stdout, `{"reachable":false,"code":"target_unavailable"}`)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := runtime.Load()
	if err != nil {
		return err
	}
	db, err := runtime.Open(cfg)
	if err != nil {
		return err
	}
	defer db.CloseIdleConnections()
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "--check" {
			return runtime.ErrConfig
		}
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ScrapeTimeout)
		defer cancel()
		if _, err := collector.Collect(ctx, db); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, `{"reachable":true,"code":"ok"}`)
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: cfg.Listen, Handler: runtime.Handler(db, cfg.ScrapeTimeout), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: cfg.ScrapeTimeout + time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("exporter started", "service", "rabbitmq-exporter")
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}
