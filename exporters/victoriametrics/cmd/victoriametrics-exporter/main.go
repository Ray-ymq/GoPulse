package main

import (
	"context"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/victoriametrics/internal/collector"
	runtime "github.com/Ray-ymq/GoPulse/exporters/victoriametrics/internal/runtime"
)

func main() {
	if run() != nil {
		if len(os.Args) > 1 {
			fmt.Fprintln(os.Stdout, `{"reachable":false,"code":"target_unavailable"}`)
		} else {
			componentmetrics.NewLogger("victoriametrics-exporter", os.Stdout).Error("exporter stopped", "reason", "invalid_configuration_or_runtime")
		}
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
	ctx, stop := componentmetrics.SignalContext()
	defer stop()
	server := &http.Server{Addr: cfg.Listen, Handler: runtime.Handler(db, cfg.ScrapeTimeout), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: cfg.ScrapeTimeout + time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return runtime.ErrConfig
	}
	defer listener.Close()
	logger := componentmetrics.NewLogger("victoriametrics-exporter", os.Stdout)
	return componentmetrics.ServeRuntime(ctx, server, listener, 5*time.Second, logger)
}
