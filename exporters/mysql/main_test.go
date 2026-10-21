package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedConfigChecksAndSafeErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"config":{"host":"127.0.0.1","port":8080,"connect_timeout":"100ms","scrape_timeout":"1s","username":"demo","database":"gopulse"},"secrets":{"password":"private"},"runtime":{"mode":"host","listen_host":"127.0.0.1","listen_port":19121}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run([]string{"check-config", "--config", path}, &out); code != 0 || !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("配置校验失败: %d %s", code, out.String())
	}
	for _, args := range [][]string{{"serve"}, {"check-config", "--config"}, {"serve", "--config", path, "extra"}} {
		out.Reset()
		if code := run(args, &out); code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatalf("参数错误响应异常: %d %s", code, out.String())
		}
	}
	if err := os.WriteFile(path, []byte(`{"config":{"password":"private"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"check-config", "--config", path}, &out); code != 1 || strings.Contains(out.String(), "private") {
		t.Fatalf("配置错误响应泄漏数据: %d %s", code, out.String())
	}
}
