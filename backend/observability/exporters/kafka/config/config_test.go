package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/exporters/kafka/common"
)

func TestValidJMXURLIsBoundedByRuntimeMode(t *testing.T) {
	for _, tc := range []struct {
		mode, value string
		want        bool
	}{
		{"host", "http://127.0.0.1:9404/metrics", true},
		{"host", "http://jmx-bridge:9404/metrics", false},
		{"container", "http://jmx-bridge:9404/metrics", true},
		{"container", "http://jmx-bridge:9404/metrics?token=secret", false},
		{"container", "http://user:jmx-bridge:9404/metrics", false},
		{"container", "ftp://jmx-bridge:9404/metrics", false},
		{"container", "http://jmx-bridge:bad/metrics", false},
		{"container", "http://jmx-bridge:9404", false},
	} {
		if got := validJMXURL(tc.mode, tc.value); got != tc.want {
			t.Errorf("validJMXURL(%q, %q)=%v, want %v", tc.mode, tc.value, got, tc.want)
		}
	}
}

func TestLoadFileAcceptsOptionalJMXURL(t *testing.T) {
	t.Setenv("GOPULSE_RUNTIME_MODE", "container")
	dir := t.TempDir()
	path := filepath.Join(dir, "kafka-exporter.json")
	data, err := json.Marshal(map[string]any{
		"config":  json.RawMessage(`{"host":"kafka","port":19092,"topic":"` + common.Topic + `","consumer_group":"` + common.Group + `","connect_timeout":"1s","scrape_timeout":"2s","jmx_url":"http://jmx-bridge:9404/metrics"}`),
		"secrets": map[string]string{}, "runtime": Runtime{Mode: "container"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(path)
	if err != nil || c.JMXURL != "http://jmx-bridge:9404/metrics" {
		t.Fatalf("LoadFile()=%#v, err=%v", c, err)
	}
	if strings.Contains(c.JMXURL, "secret") {
		t.Fatal("JMX URL unexpectedly contains a secret")
	}
}
