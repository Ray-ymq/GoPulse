package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const executionFixture = `{"config":{"host":"127.0.0.1","port":8080,"connect_timeout":"100ms","scrape_timeout":"1s","username":"demo"},"secrets":{"password":"private"},"runtime":{"mode":"host","listen_host":"127.0.0.1","listen_port":19121}}`

func TestFileConfigurationIsStrictAndDoesNotMutateEnvironment(t *testing.T) {
	t.Setenv("ELASTICSEARCH_HOST", "ambient-canary")
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
	if os.Getenv("ELASTICSEARCH_HOST") != "ambient-canary" {
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
