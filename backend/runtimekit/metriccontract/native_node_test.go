package metriccontract

import "testing"

func TestNativeNodeNamespaceIsSourceBounded(t *testing.T) {
	for _, name := range []string{"node_cpu_seconds_total", "node_memory_MemTotal_bytes", "node_load1", "gopulse_node_up"} {
		if !ValidName("node", name) {
			t.Fatal(name)
		}
		if ValidName("redis", name) {
			t.Fatal("跨数据源名称被接受", name)
		}
	}
	for _, name := range []string{"node_", "node_cpu{mode=\"idle\"}", "go_goroutines", "process_cpu_seconds_total"} {
		if ValidName("node", name) {
			t.Fatal(name)
		}
	}
}
