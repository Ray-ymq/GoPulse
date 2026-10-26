package metricquery

import "testing"

func TestNativeNodeMetricDiscoveryIdentity(t *testing.T) {
	definition, ok := definitionFor("node_cpu_seconds_total")
	if !ok || definition.Source != "node" || definition.ProducerID != "node-exporter" || definition.TargetID != "node-exporter-local" {
		t.Fatal(definition, ok)
	}
	if _, ok := definitionFor("node_cpu_seconds_total{source=\"other\"}"); ok {
		t.Fatal("接受了查询表达式")
	}
}
