package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/kafka/internal/collector"
	"github.com/Ray-ymq/GoPulse/runtimekit/exportercontract"
)

func TestReadOnlyClientOptionsAreCompatible(t *testing.T) {
	// No consume topics or network requests: catch constructor option conflicts
	// before the safe --check path hides an upstream configuration error.
	c, err := Open(Config{Host: "127.0.0.1", Port: "9092", ConnectTimeout: time.Second, ScrapeTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c.CloseIdleConnections()
}

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
	data, err := exportercontract.Encode(
		[]byte(`{"host":"kafka","port":19092,"topic":"`+collector.Topic+`","consumer_group":"`+collector.Group+`","connect_timeout":"1s","scrape_timeout":"2s","jmx_url":"http://jmx-bridge:9404/metrics"}`),
		[]byte(`{}`),
		exportercontract.Runtime{Mode: "container"},
	)
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
