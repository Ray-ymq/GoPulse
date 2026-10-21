package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validConfig = `{"config":{"host":"127.0.0.1","port":8080,"username":"demo","connect_timeout":"100ms","scrape_timeout":"1s"},"secrets":{"password":"private"},"runtime":{"mode":"host","listen_host":"127.0.0.1","listen_port":9601,"shutdown_timeout":"2s"}}`

func TestParseAndRedact(t *testing.T) {
	cfg, err := Parse([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnectTimeout != 100*time.Millisecond || cfg.ScrapeTimeout != time.Second || cfg.Listen != "127.0.0.1:9601" || cfg.ShutdownTimeout != 2*time.Second {
		t.Fatalf("配置解析结果错误: %+v", cfg.Redacted())
	}
	if cfg.Redacted().Password != "<redacted>" || cfg.Password != "private" {
		t.Fatal("脱敏修改了原配置")
	}
}

func TestRejectInvalidConfiguration(t *testing.T) {
	cases := map[string]string{
		"重复键":     strings.Replace(validConfig, `"port":8080`, `"port":8080,"port":8081`, 1),
		"未知字段":    strings.Replace(validConfig, `"username":"demo"`, `"username":"demo","token":"x"`, 1),
		"重复文档":    validConfig + validConfig,
		"null":    strings.Replace(validConfig, `"listen_port":9601`, `"listen_port":null`, 1),
		"数组":      `[]`,
		"缺少配置":    `{"secrets":{}}`,
		"外部地址":    strings.Replace(validConfig, `"host":"127.0.0.1"`, `"host":"example.com"`, 1),
		"监听公网":    strings.Replace(validConfig, `"listen_host":"127.0.0.1"`, `"listen_host":"0.0.0.0"`, 1),
		"越界端口":    strings.Replace(validConfig, `"port":8080`, `"port":65536`, 1),
		"浮点端口":    strings.Replace(validConfig, `"port":8080`, `"port":8080.5`, 1),
		"超时顺序":    strings.Replace(validConfig, `"connect_timeout":"100ms"`, `"connect_timeout":"2s"`, 1),
		"运行时超时越界": strings.Replace(validConfig, `"mode":"host"`, `"mode":"host","scrape_timeout":"20s"`, 1),
		"缺少密码":    strings.Replace(validConfig, `"password":"private"`, `"password":""`, 1),
		"非法模式":    strings.Replace(validConfig, `"mode":"host"`, `"mode":"external"`, 1),
		"超大配置":    strings.Repeat(" ", MaxConfigBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(data)); err == nil {
				t.Fatal("接受了无效配置")
			}
		})
	}
}

func TestContainerAndFile(t *testing.T) {
	data := strings.ReplaceAll(validConfig, `"mode":"host"`, `"mode":"container"`)
	data = strings.Replace(data, `"host":"127.0.0.1"`, `"host":"demo-service"`, 1)
	data = strings.Replace(data, `"listen_host":"127.0.0.1"`, `"listen_host":"0.0.0.0"`, 1)
	path := filepath.Join(t.TempDir(), "execution.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(t.TempDir()); err == nil {
		t.Fatal("接受了目录")
	}
}

func TestRuntimeCapsTimeouts(t *testing.T) {
	data := strings.Replace(validConfig, `"mode":"host"`, `"mode":"host","connect_timeout":"3s","scrape_timeout":"500ms"`, 1)
	cfg, err := Parse([]byte(data))
	if err != nil || cfg.ConnectTimeout != 100*time.Millisecond || cfg.ScrapeTimeout != 500*time.Millisecond {
		t.Fatal("运行时预算未限制插件超时")
	}
}
