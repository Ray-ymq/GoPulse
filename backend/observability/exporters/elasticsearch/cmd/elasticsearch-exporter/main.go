package main

import (
	"context"
	"fmt"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"github.com/Ray-ymq/GoPulse/runtimekit/exportercontract"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/internal/collector"
	runtime "github.com/Ray-ymq/GoPulse/exporters/elasticsearch/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && managedCommand(os.Args[1]) {
		if len(os.Args) != 4 || os.Args[2] != "--config" {
			_ = exportercontract.Result(os.Stdout, false, "invalid_arguments")
			os.Exit(2)
		}
		code, err := runManaged(os.Args[1], os.Args[3])
		if err != nil {
			if code == "" {
				code = "runtime_failed"
			}
			_ = exportercontract.Result(os.Stdout, false, code)
			os.Exit(1)
		}
		return
	}
	if run() != nil {
		if len(os.Args) > 1 {
			fmt.Fprintln(os.Stdout, `{"reachable":false,"code":"target_unavailable"}`)
		} else {
			runtimekit.NewLogger("elasticsearch-exporter", os.Stdout).Error("exporter stopped", "reason", "invalid_configuration_or_runtime")
		}
		os.Exit(1)
	}
}
func run() error {
	cfg, err := runtime.Load()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 && (len(os.Args) != 2 || os.Args[1] != "--check") {
		return runtime.ErrConfig
	}
	return runWithConfig(cfg, len(os.Args) > 1)
}

func runWithConfig(cfg runtime.Config, check bool) error {
	db, err := runtime.Open(cfg)
	if err != nil {
		return err
	}
	defer db.CloseIdleConnections()
	if check {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ScrapeTimeout)
		defer cancel()
		if _, err := collector.Collect(ctx, db); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, `{"reachable":true,"code":"ok"}`)
		return nil
	}
	ctx, stop := runtimekit.SignalContext()
	defer stop()
	server := &http.Server{Addr: cfg.Listen, Handler: runtime.Handler(db, cfg.ScrapeTimeout), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: cfg.ScrapeTimeout + time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return runtime.ErrConfig
	}
	defer listener.Close()
	logger := runtimekit.NewLogger("elasticsearch-exporter", os.Stdout)
	return runtimekit.ServeRuntime(ctx, server, listener, 5*time.Second, logger)
}

func managedCommand(command string) bool {
	return command == "check-config" || command == "check-connection" || command == "serve"
}

func runManaged(command, path string) (string, error) {
	if command == "check-config" {
		if _, err := runtime.LoadFile(path); err != nil {
			return "invalid_configuration", err
		}
		return "", exportercontract.Result(os.Stdout, true, "ok")
	}
	cfg, err := runtime.LoadFile(path)
	if err != nil {
		return "invalid_configuration", err
	}
	if command == "check-connection" {
		if err := runWithConfig(cfg, true); err != nil {
			return "target_unavailable", err
		}
		return "", nil
	}
	return "runtime_failed", runWithConfig(cfg, false)
}
