package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
)

type options struct {
	DSNFile    string
	MetricsURL string
	Once       bool
	Fast       time.Duration
	Summary    time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.DSNFile, "dsn-file", "", "私有只读诊断账号的 DSN 文件")
	flag.StringVar(&o.MetricsURL, "metrics-url", "", "独立 MySQL exporter 的回环 metrics 地址")
	flag.BoolVar(&o.Once, "once", false, "采集一次汇总和现场后退出")
	flag.DurationVar(&o.Fast, "interval", 200*time.Millisecond, "现场采样间隔，至少 100ms")
	flag.DurationVar(&o.Summary, "summary-interval", 5*time.Second, "汇总采样间隔，至少 1s")
	flag.Parse()
	if o.Fast < 100*time.Millisecond || o.Summary < time.Second || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "诊断参数无效")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, o, os.Stdin, os.Stdout); err != nil {
		// 驱动错误可能包含 DSN 或 SQL。只输出固定错误，详细状态使用查询名称。
		fmt.Fprintln(os.Stderr, "数据库诊断未完成")
		os.Exit(1)
	}
}

func run(ctx context.Context, o options, input io.Reader, output io.Writer) error {
	raw, err := os.ReadFile(o.DSNFile)
	if err != nil {
		return err
	}
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil || cfg == nil || cfg.Net != "tcp" || cfg.User == "" || cfg.DBName == "" {
		return fmt.Errorf("invalid_dsn")
	}
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = time.Second, time.Second, time.Second
	cfg.Logger = silentLogger{}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(connector)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	endpoint, err := url.Parse(o.MetricsURL)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/metrics" || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() {
		return fmt.Errorf("invalid_metrics_url")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	var mu sync.Mutex
	encoder := json.NewEncoder(output)
	emit := func(row map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		return encoder.Encode(row)
	}
	summary := func(trigger string) error {
		start := time.Now()
		status, body, err := readMetrics(ctx, client, o.MetricsURL)
		if err != nil {
			return err
		}
		return emit(map[string]any{"type": "metrics", "timestamp": start.UTC(), "epoch": float64(start.UnixNano()) / 1e9,
			"finished_at": time.Now().UTC(), "elapsed_seconds": time.Since(start).Seconds(), "trigger": trigger, "status": status, "body": body})
	}
	var sceneMu sync.Mutex
	cursor := &historyCursor{}
	scene := func(trigger string, history bool) error {
		sceneMu.Lock()
		defer sceneMu.Unlock()
		return emit(snapshot(ctx, db, cfg.DBName, cfg.User, trigger, history, cursor))
	}
	if err := summary("baseline"); err != nil {
		return err
	}
	if err := scene("baseline", true); err != nil || o.Once {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	failures := make(chan error, 3)
	var workers sync.WaitGroup
	loop := func(interval time.Duration, collect func() error) {
		defer workers.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := collect(); err != nil {
					failures <- err
					return
				}
			}
		}
	}
	workers.Add(2)
	go loop(o.Summary, func() error { return summary("periodic") })
	lastHistory := time.Now()
	go loop(o.Fast, func() error {
		history := time.Since(lastHistory) >= time.Second
		if history {
			lastHistory = time.Now()
		}
		return scene("periodic", history)
	})
	go func() {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 1024), 1024)
		for scanner.Scan() {
			trigger := scanner.Text()
			if trigger != "first_failure" && trigger != "stage_end" && trigger != "recovery_end" && trigger != "manual" {
				continue
			}
			if err := scene(trigger, true); err != nil {
				failures <- err
				return
			}
			if err := summary(trigger); err != nil {
				failures <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
	case err = <-failures:
	}
	cancel()
	workers.Wait()
	return err
}

type silentLogger struct{}

func (silentLogger) Print(...any) {}
