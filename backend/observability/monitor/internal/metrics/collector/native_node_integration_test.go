package collector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

// TestNativeNodeLifecycle 使用显式提供的官方发布包，覆盖 Monitor 的真实进程管理流程。
func TestNativeNodeLifecycle(t *testing.T) {
	catalog := os.Getenv("GOPULSE_TEST_NODE_CATALOG")
	if catalog == "" {
		t.Skip("未提供官方 node_exporter 测试包")
	}
	cfg := plugin.ManagerConfig{Root: t.TempDir(), PackagesRoot: filepath.Dir(catalog), CatalogPath: catalog, ExporterEnv: map[string]string{"GOPULSE_RUNTIME_MODE": "host"}, StartupTimeout: 10 * time.Second, StopTimeout: 2 * time.Second, ValidateSourceSnapshot: ValidateSuccessfulSourceSnapshot}
	manager, err := plugin.NewManager(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := manager.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	request := []byte(`{"config":{},"secrets":{}}`)
	if err := manager.ConnectionTest(t.Context(), "node-exporter", request); err != nil {
		t.Fatal("原生采集检查失败", err)
	}
	if entries, err := os.ReadDir(cfg.Root); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if entry.Name() == "node-exporter" {
				t.Fatal("采集检查修改了插件状态")
			}
		}
	}
	status, err := manager.Configure(t.Context(), "node-exporter", request, true)
	if err != nil || status.ObservedState != plugin.ObservedRunning {
		t.Fatal("安装失败", status, err)
	}
	entries := manager.ExecutionCatalog()
	if len(entries) != 1 || entries[0].ID != "node-exporter" {
		t.Fatal(entries)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(entries[0].Port) + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	state, samples, err := parseSource("node", response.StatusCode, body)
	if err != nil || state != "success" {
		t.Fatalf("采集失败: %s %v; %s", state, err, body)
	}
	if !strings.Contains(string(body), "node_cpu_seconds_total") || strings.Contains(string(body), "gopulse_") {
		t.Fatal("官方二进制输出被改写")
	}
	message, err := envelopeForNativeNode(samples)
	if err != nil || !strings.Contains(string(message), "node_cpu_seconds_total") {
		t.Fatal(string(message), err)
	}
	if status, err = manager.Stop(t.Context(), "node-exporter"); err != nil || status.ObservedState != plugin.ObservedStopped {
		t.Fatal(status, err)
	}
	if status, err = manager.Start(t.Context(), "node-exporter"); err != nil || status.ObservedState != plugin.ObservedRunning {
		t.Fatal(status, err)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := plugin.NewManager(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := recovered.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	status, err = recovered.Get("node-exporter")
	if err != nil || status.ObservedState != plugin.ObservedRunning {
		t.Fatal("重启恢复失败", status, err)
	}
}

func envelopeForNativeNode(samples []envelope.Sample) ([]byte, error) {
	message, err := envelope.NewSource("node", samples, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return json.Marshal(message)
}
