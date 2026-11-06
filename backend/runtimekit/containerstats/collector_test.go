package containerstats

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) Collector {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"proc/42/cgroup":                   "0::/container\n",
		"cgroup/container/cpu.max":         "75000 100000\n",
		"cgroup/container/cpu.stat":        "usage_usec 1500000\nuser_usec 1000000\nsystem_usec 500000\nnr_periods 10\nnr_throttled 2\nthrottled_usec 100000\n",
		"cgroup/container/memory.current":  "1234\n",
		"cgroup/container/memory.peak":     "2048\n",
		"cgroup/container/memory.max":      "4096\n",
		"cgroup/container/memory.swap.max": "0\n",
		"cgroup/container/memory.events":   "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n",
		"cgroup/container/pids.current":    "7\n",
		"targets.json":                     `[{"name":"backend","id":"container-id","pid":42}]`,
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Collector{ProcFS: filepath.Join(root, "proc"), CgroupFS: filepath.Join(root, "cgroup"), TargetsFile: filepath.Join(root, "targets.json")}
}
func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotAndPrometheusShareCounters(t *testing.T) {
	c := fixture(t)
	s, err := c.Collect()
	if err != nil {
		t.Fatal(err)
	}
	value := s.Containers["backend"]
	if s.SchemaVersion != Schema || s.Epoch <= 0 || value.CPU["usage_usec"] != 1500000 || value.MemoryCurrent != 1234 || *value.MemoryMax != 4096 || *value.MemoryPeak != 2048 || value.PIDsCurrent != 7 {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
	handler := c.Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	for _, line := range []string{`gopulse_container_cpu_usage_seconds_total{container="backend",container_id="container-id"} 1.5`, `gopulse_container_cpu_quota_cores{container="backend",container_id="container-id"} 0.75`, `gopulse_container_oom_kills_total{container="backend",container_id="container-id"} 0`} {
		if !strings.Contains(response.Body.String(), line) {
			t.Fatal(response.Body.String())
		}
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/snapshot", nil))
	var decoded Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil || decoded.Containers["backend"].CPU["usage_usec"] != value.CPU["usage_usec"] {
		t.Fatal("HTTP snapshot differs", err)
	}
}
func TestUnsupportedOptionalAndUnlimitedValuesAreAbsent(t *testing.T) {
	c := fixture(t)
	if err := os.Remove(filepath.Join(c.CgroupFS, "container/memory.peak")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(c.CgroupFS, "container/memory.max"), "max")
	put(t, filepath.Join(c.CgroupFS, "container/memory.swap.max"), "max")
	put(t, filepath.Join(c.CgroupFS, "container/cpu.max"), "max 100000")
	snapshot, err := c.Collect()
	if err != nil {
		t.Fatal(err)
	}
	value := snapshot.Containers["backend"]
	if value.MemoryPeak != nil || value.MemoryMax != nil || value.MemorySwapMax != nil {
		t.Fatal("unavailable values fabricated")
	}
	response := httptest.NewRecorder()
	c.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	for _, name := range []string{"memory_peak_bytes{", "memory_limit_bytes{", "cpu_quota_cores{"} {
		if strings.Contains(response.Body.String(), name) {
			t.Fatal("unavailable value exposed")
		}
	}
}
func TestInvalidRequiredSourcesFailWithoutPartialMetrics(t *testing.T) {
	for _, test := range []struct{ name, file, body string }{
		{"missing counter", "cgroup/container/cpu.stat", "nr_periods 10\n"},
		{"duplicate counter", "cgroup/container/cpu.stat", "usage_usec 1\nusage_usec 2"},
		{"negative memory", "cgroup/container/memory.current", "-1"},
		{"invalid quota", "cgroup/container/cpu.max", "1 0"},
		{"missing oom counter", "cgroup/container/memory.events", "oom 0"},
		{"v1", "proc/42/cgroup", "2:cpu:/container"},
		{"traversal", "proc/42/cgroup", "0::/../container"},
		{"duplicate targets", "targets.json", `[{"name":"a","id":"a","pid":42},{"name":"a","id":"b","pid":42}]`},
		{"unknown field", "targets.json", `[{"name":"a","id":"a","pid":42,"secret":"x"}]`},
		{"oversized", "targets.json", `[{"name":"a","id":"a","pid":42}]` + strings.Repeat(" ", 65536)},
		{"invalid identity", "targets.json", `[{"name":"a","id":"\t","pid":42}]`},
		{"trailing data", "targets.json", `[{"name":"a","id":"a","pid":42}][]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixture(t)
			put(t, filepath.Join(filepath.Dir(c.ProcFS), test.file), test.body)
			for _, endpoint := range []string{"/metrics", "/snapshot"} {
				response := httptest.NewRecorder()
				c.Handler().ServeHTTP(response, httptest.NewRequest("GET", endpoint, nil))
				if response.Code != 503 || response.Body.String() != "# TYPE gopulse_container_up gauge\ngopulse_container_up 0\n" {
					t.Fatal("invalid source yielded partial metrics", response.Body.String())
				}
			}
		})
	}
}
func TestStoppedContainerIsNotZeroUsage(t *testing.T) {
	c := fixture(t)
	if err := os.Remove(filepath.Join(c.ProcFS, "42/cgroup")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collect(); err == nil {
		t.Fatal("missing container accepted")
	}
}
