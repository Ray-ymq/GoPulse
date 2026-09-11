package metricquery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestComponentQueryProvenanceAndUnknownDependency(t *testing.T) {
	d := definitions["gopulse_monitor_dependency_up"]
	if d.ProducerKind != "component" || !strings.Contains(QueryExpression(d.Metric), `producer_kind="component"`) {
		t.Fatal("component catalog/provenance missing")
	}
	metric := map[string]string{"__name__": d.Metric, "source": "monitor", "target_id": "monitor-local", "producer_kind": "component", "producer_id": "monitor", "dependency": "router"}
	encode := func(value string) []byte {
		body, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": metric, "values": []any{[]any{1750000000, value}}}}}})
		return body
	}
	if _, err := decodeResponse(encode("-1"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeResponse(encode("-2"), d); err == nil {
		t.Fatal("invalid dependency state accepted")
	}
	metric["producer_id"] = "backend"
	if _, err := decodeResponse(encode("1"), d); err == nil {
		t.Fatal("cross-component provenance accepted")
	}
}
