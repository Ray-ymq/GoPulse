package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const executionFixture = `{"config":{"host":"127.0.0.1","port":8080,"connect_timeout":"100ms","scrape_timeout":"1s","username":"demo","database":"gopulse"},"secrets":{"password":"private"},"runtime":{"mode":"host","listen_host":"127.0.0.1","listen_port":19121}}`

func TestFileConfigurationIsStrictAndDoesNotMutateEnvironment(t *testing.T) {
	t.Setenv("MYSQL_HOST", "ambient-canary")
	path := filepath.Join(t.TempDir(), "execution.json")
	if err := os.WriteFile(path, []byte(executionFixture), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := LoadFile(path); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if os.Getenv("MYSQL_HOST") != "ambient-canary" {
		t.Fatal("文件加载修改了进程环境")
	}
	for name, data := range map[string]string{
		"重复键":  strings.Replace(executionFixture, `"host":"127.0.0.1"`, `"host":"127.0.0.1","host":"localhost"`, 1),
		"未知字段": strings.Replace(executionFixture, `"config":{`, `"config":{"unexpected":1,`, 1),
		"null": strings.Replace(executionFixture, `"host":"127.0.0.1"`, `"host":null`, 1),
		"重复文档": executionFixture + executionFixture,
		"超大文件": strings.Repeat(" ", MaxConfigBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFile(path); err == nil {
				t.Fatal("接受了非法配置")
			}
		})
	}
}
func TestRedactedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.json")
	if err := os.WriteFile(path, []byte(executionFixture), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(cfg.Redacted())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private") {
		t.Fatal("脱敏配置包含密码")
	}
}

func TestCollectorSelection(t *testing.T) {
	for _, selection := range []string{"", "none", "transactions,statement_digest,statement_events", CollectorNames, "unknown", "transactions,transactions", "transactions,", " transactions"} {
		t.Run(selection, func(t *testing.T) {
			data := strings.Replace(executionFixture, `"config":{`, `"config":{"collectors":`+strconv.Quote(selection)+`,`, 1)
			path := filepath.Join(t.TempDir(), "execution.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := LoadFile(path)
			valid := selection == "" || selection == "none" || selection == "transactions,statement_digest,statement_events" || selection == CollectorNames
			if valid {
				if err != nil || c.Collectors != selection {
					t.Fatal(c.Collectors, err)
				}
			} else if err == nil {
				t.Fatal("接受了非法采集器列表")
			}
		})
	}
}

func TestHostPrivateTargetsKeepLoopbackListener(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "10.20.0.2", "172.19.0.2", "192.168.1.2", "fd00::2", "8.8.8.8", "169.254.1.2", "0.0.0.0", "mysql.example.com"} {
		t.Run(host, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			body := strings.Replace(executionFixture, `"host":"127.0.0.1"`, `"host":`+strconv.Quote(host), 1)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadFile(path)
			valid := host == "127.0.0.1" || host == "localhost" || host == "10.20.0.2" || host == "172.19.0.2" || host == "192.168.1.2" || host == "fd00::2"
			if valid && (err != nil || cfg.Host != host || cfg.Listen != "127.0.0.1:19121") {
				t.Fatalf("合法目标或回环监听发生变化: %v", err)
			}
			if !valid && err == nil {
				t.Fatal("接受了不支持的宿主机目标")
			}
		})
	}
	for _, host := range []string{"0.0.0.0", "172.19.0.2", "192.168.1.2"} {
		path := filepath.Join(t.TempDir(), "config.json")
		body := strings.Replace(executionFixture, `"listen_host":"127.0.0.1"`, `"listen_host":`+strconv.Quote(host), 1)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFile(path); err == nil {
			t.Fatal("私网目标支持不应放宽指标服务监听范围")
		}
	}
}
