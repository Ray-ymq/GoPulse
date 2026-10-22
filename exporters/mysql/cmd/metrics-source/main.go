// metrics-source 将已有 MySQL collector 作为独立 HTTP 数据源运行。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/mysql/collector"
	"github.com/Ray-ymq/GoPulse/exporters/mysql/config"
	"github.com/go-sql-driver/mysql"
)

func main() {
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "MySQL 指标源运行失败")
		os.Exit(1)
	}
}
func serve() error {
	dsn := flag.String("dsn-file", "", "只读账号的私有 DSN 文件")
	flag.Parse()
	if *dsn == "" || flag.NArg() != 0 {
		return fmt.Errorf("invalid_arguments")
	}
	raw, err := os.ReadFile(*dsn)
	if err != nil {
		return err
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil || cfg == nil || cfg.Net != "tcp" || cfg.User == "" || cfg.DBName == "" {
		return fmt.Errorf("invalid_dsn")
	}
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() && !ip.IsPrivate() {
		return fmt.Errorf("invalid_target")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid_port")
	}
	exporter, err := collector.New(&config.Config{Host: host, Port: portNumber, Username: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, ConnectTimeout: time.Second, ScrapeTimeout: 2 * time.Second})
	if err != nil {
		return err
	}
	defer exporter.Stop()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	server := &http.Server{Handler: exporter.Handler(), ReadHeaderTimeout: time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "http://" + listener.Addr().String(), "schema_version": "gopulse.mysql.metrics.v1"}); err != nil {
		return err
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
