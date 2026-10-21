package collector

import (
	"strings"
	"testing"
)

const nativeNodeFixture = `# TYPE node_cpu_seconds_total counter
node_cpu_seconds_total{cpu="0",mode="idle"} 42
# TYPE node_memory_MemTotal_bytes gauge
node_memory_MemTotal_bytes 8192
# TYPE node_load1 gauge
node_load1 0.5
# TYPE node_filesystem_avail_bytes gauge
node_filesystem_avail_bytes{device="/dev/sda1",mountpoint="/",fstype="ext4"} 2048
# TYPE node_network_receive_bytes_total counter
node_network_receive_bytes_total{device="eth0"} 128
# TYPE node_scrape_collector_success gauge
node_scrape_collector_success{collector="cpu"} 1
`

func TestNativeNodeMetricsPreserveNamesAndLabels(t *testing.T) {
	status, samples, err := parseSource("node", 200, []byte(nativeNodeFixture))
	if err != nil || status != "success" || len(samples) != 7 {
		t.Fatal(status, samples, err)
	}
	found := false
	for _, sample := range samples {
		if sample.Name == "node_cpu_seconds_total" {
			found = sample.Kind == "counter" && sample.Value == 42 && sample.Labels["cpu"] == "0" && sample.Labels["mode"] == "idle"
		}
	}
	if !found {
		t.Fatal("原生 CPU 指标丢失")
	}
	if _, _, err := parseSource("redis", 200, []byte(nativeNodeFixture)); err == nil {
		t.Fatal("原生指标进入其他数据源")
	}
	if err := ValidateSuccessfulSourceSnapshot("node", 200, []byte(nativeNodeFixture)); err != nil {
		t.Fatal(err)
	}
}

func TestNativeNodeFailedCollectorDropsBusinessMetrics(t *testing.T) {
	body := strings.Replace(nativeNodeFixture, `collector="cpu"} 1`, `collector="cpu"} 0`, 1)
	status, samples, err := parseSource("node", 200, []byte(body))
	if err != nil || status != "target_unavailable" || len(samples) != 1 || samples[0].Name != "gopulse_node_up" || samples[0].Value != 0 {
		t.Fatal(status, samples, err)
	}
	if err := ValidateSuccessfulSourceSnapshot("node", 200, []byte(body)); err == nil {
		t.Fatal("安装检查接受了失败采集")
	}
}

func TestNativeNodeRejectsInvalidSnapshots(t *testing.T) {
	for name, body := range map[string]string{
		"伪造状态":   nativeNodeFixture + "# TYPE gopulse_node_up gauge\ngopulse_node_up 1\n",
		"其他命名空间": nativeNodeFixture + "# TYPE go_goroutines gauge\ngo_goroutines 1\n",
		"重复序列":   nativeNodeFixture + "node_load1 2\n",
		"无穷值":    strings.Replace(nativeNodeFixture, "node_load1 0.5", "node_load1 +Inf", 1),
		"保留标签":   strings.Replace(nativeNodeFixture, `cpu="0"`, `source="node"`, 1),
		"缺少 CPU": strings.Replace(nativeNodeFixture, `node_cpu_seconds_total{cpu="0",mode="idle"} 42`, "", 1),
		"非法状态":   strings.Replace(nativeNodeFixture, `collector="cpu"} 1`, `collector="cpu"} 2`, 1),
		"空快照":    "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseSource("node", 200, []byte(body)); err == nil {
				t.Fatal("接受了非法快照")
			}
		})
	}
	if _, _, err := parseSource("node", 503, []byte(nativeNodeFixture)); err == nil {
		t.Fatal("接受了 HTTP 503")
	}
}
