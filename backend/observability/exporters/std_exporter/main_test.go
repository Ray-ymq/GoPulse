package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/std_exporter/config"
)

func executionFile(t *testing.T, host string, port int) string {
	t.Helper()
	value := map[string]any{"config": map[string]any{"host": host, "port": port, "username": "demo", "connect_timeout": "100ms", "scrape_timeout": "1s"}, "secrets": map[string]string{"password": "private"}}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommands(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	number, _ := strconv.Atoi(port)
	path := executionFile(t, host, number)
	var out bytes.Buffer
	if code := run([]string{"check-config", "--config", path}, &out); code != 0 || calls != 0 || !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("配置检查失败: %d %s", code, out.String())
	}
	out.Reset()
	if code := run([]string{"check-connection", "--config", path}, &out); code != 0 || !strings.Contains(out.String(), `"reachable":true`) {
		t.Fatalf("连接检查失败: %d %s", code, out.String())
	}
	// 关闭目标后，连接检查必须返回稳定错误且不暴露凭据。
	server.Close()
	out.Reset()
	if code := run([]string{"check-connection", "--config", path}, &out); code != 1 || !strings.Contains(out.String(), `"reachable":false`) || strings.Contains(out.String(), "private") {
		t.Fatalf("连接失败响应错误: %d %s", code, out.String())
	}
	for _, args := range [][]string{{"serve"}, {"check-config", "--config"}, {"check-config", "--config", path, "extra"}} {
		out.Reset()
		if code := run(args, &out); code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatalf("参数检查错误: %d %s", code, out.String())
		}
	}
}

func TestShutdownReleasesListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case ready <- struct{}{}:
		default:
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, &config.Config{Listen: address, ScrapeTimeout: time.Second, ShutdownTimeout: time.Second}, handler)
	}()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := client.Get("http://" + address + "/health")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("服务未启动")
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-ready
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务未退出")
	}
	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal("退出后端口未释放")
	}
	_ = listener.Close()
}

func TestLegacyFlags(t *testing.T) {
	cfg, err := legacyConfig([]string{"-host=127.0.0.1", "-port=8080", "-user=demo", "-password=private", "-timeout=2s"})
	if err != nil || cfg.ScrapeTimeout != 2*time.Second || cfg.Password != "private" {
		t.Fatal("手动启动参数解析失败")
	}
}
