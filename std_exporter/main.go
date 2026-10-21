package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"std_exporter/collector"
	"std_exporter/common"
	"std_exporter/config"
	"std_exporter/logger"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func run(args []string, out io.Writer) int {
	command := "serve"
	var cfg config.Config
	var err error
	managed := len(args) > 0 && (args[0] == "check-config" || args[0] == "check-connection" || args[0] == "serve")
	if managed {
		command = args[0]
		if len(args) != 3 || args[1] != "--config" || args[2] == "" {
			return result(out, false, "invalid_arguments", 2)
		}
		cfg, err = config.LoadFile(args[2])
	} else {
		cfg, err = legacyConfig(args)
	}
	if err != nil {
		return result(out, false, "invalid_configuration", 1)
	}
	if command == "check-config" {
		return result(out, true, "ok", 0)
	}
	exporter := collector.New(&cfg)
	defer exporter.Stop()
	if command == "check-connection" {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
		defer cancel()
		if exporter.CheckConnection(ctx) != nil {
			return result(out, false, "target_unavailable", 1)
		}
		return result(out, true, "ok", 0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if serve(ctx, &cfg, exporter.Handler()) != nil {
		return result(out, false, "runtime_failed", 1)
	}
	return 0
}

func result(out io.Writer, valid bool, code string, exit int) int {
	response := struct {
		Valid     bool   `json:"valid"`
		Reachable *bool  `json:"reachable,omitempty"`
		Code      string `json:"code"`
	}{Valid: valid, Code: code}
	if valid || code == "target_unavailable" {
		response.Reachable = &valid
	}
	if json.NewEncoder(out).Encode(response) != nil {
		return 1
	}
	return exit
}

func serve(ctx context.Context, cfg *config.Config, handler http.Handler) error {
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return errors.New("listen_failed")
	}
	defer func() { _ = listener.Close() }()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: cfg.ScrapeTimeout + time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.GetStdLogger().Info("插件已启动")
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("server_failed")
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			<-done
			return errors.New("shutdown_timeout")
		}
		<-done
		return nil
	}
}

func legacyConfig(args []string) (config.Config, error) {
	flags := flag.NewFlagSet(common.PluginID, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	host := flags.String("host", "", "目标主机")
	port := flags.Int("port", 0, "目标端口")
	user := flags.String("user", "", "认证用户名")
	password := flags.String("password", "", "认证密码")
	listen := flags.String("web.listen-address", "127.0.0.1:9601", "监听地址")
	metricPath := flags.String("web.telemetry-path", "/metrics", "指标路径")
	timeout := flags.Duration("timeout", 5*time.Second, "采集超时，例如 5s")
	level := flags.String("logLevel", "info", "日志级别")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *metricPath != "/metrics" {
		return config.Config{}, config.ErrConfig
	}
	*password = strings.Clone(*password)
	// 参数来自真实进程时才能安全改写 argv，单元测试传入的字符串不可改写。
	if len(os.Args) > 1 && len(args) == len(os.Args)-1 && len(args) > 0 && &args[0] == &os.Args[1] {
		common.ScrubSecretArgs(os.Args, "password")
	}
	logger.GetStdLogger().SetLevel(*level)
	listenHost, listenPort, err := net.SplitHostPort(*listen)
	if err != nil {
		return config.Config{}, config.ErrConfig
	}
	var portNumber int
	portNumber, err = strconv.Atoi(listenPort)
	if err != nil {
		return config.Config{}, config.ErrConfig
	}
	data, err := json.Marshal(map[string]any{
		"config":  map[string]any{"host": *host, "port": *port, "username": *user, "connect_timeout": min(time.Second, *timeout).String(), "scrape_timeout": timeout.String()},
		"secrets": map[string]string{"password": *password},
		"runtime": config.Runtime{Mode: "host", ListenHost: listenHost, ListenPort: portNumber},
	})
	if err != nil {
		return config.Config{}, config.ErrConfig
	}
	return config.Parse(data)
}
