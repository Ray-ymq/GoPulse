package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/collector"
	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/common"
	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/config"
	"github.com/Ray-ymq/GoPulse/exporters/elasticsearch/logger"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
func run(args []string, out io.Writer) int {
	command := "serve"
	var cfg config.Config
	var err error
	if len(args) > 0 && (args[0] == "serve" || args[0] == "check-config" || args[0] == "check-connection") {
		command = args[0]
		if len(args) != 3 || args[1] != "--config" {
			return result(out, false, "invalid_arguments", 2)
		}
		cfg, err = config.LoadFile(args[2])
	} else {
		if len(args) > 0 && (len(args) != 1 || args[0] != "--check") {
			return result(out, false, "invalid_arguments", 2)
		}
		if len(args) > 0 {
			command = "check-connection"
		}
		cfg, err = config.Load()
	}
	if err != nil {
		return result(out, false, "invalid_configuration", 1)
	}
	if command == "check-config" {
		return result(out, true, "ok", 0)
	}
	exporter, err := collector.New(&cfg)
	if err != nil {
		return result(out, false, "invalid_configuration", 1)
	}
	defer exporter.Stop()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command == "check-connection" {
		check, cancel := context.WithTimeout(ctx, cfg.ScrapeTimeout)
		defer cancel()
		if exporter.CheckConnection(check) != nil {
			return result(out, false, "target_unavailable", 1)
		}
		return result(out, true, "ok", 0)
	}
	logs := logger.New(common.PluginID, os.Stderr)
	if common.Serve(ctx, cfg.Listen, cfg.ScrapeTimeout, cfg.ShutdownTimeout, exporter.Handler(), logs) != nil {
		return result(out, false, "runtime_failed", 1)
	}
	return 0
}
func result(out io.Writer, valid bool, code string, exit int) int {
	value := struct {
		Valid     bool   `json:"valid"`
		Reachable *bool  `json:"reachable,omitempty"`
		Code      string `json:"code"`
	}{Valid: valid, Code: code}
	if valid || code == "target_unavailable" {
		value.Reachable = &valid
	}
	if json.NewEncoder(out).Encode(value) != nil {
		return 1
	}
	return exit
}
