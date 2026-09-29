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

func TestBackendLatencyBucketQueryPreservesFixedBucketLabel(t *testing.T) {
	d, ok := definitions["gopulse_backend_http_request_duration_seconds_bucket"]
	if !ok {
		t.Fatal("backend latency bucket missing from query catalog")
	}
	metric := map[string]string{
		"__name__": "gopulse_backend_http_request_duration_seconds_bucket", "source": "backend",
		"target_id": "backend-local", "producer_kind": "component", "producer_id": "backend",
		"method": "GET", "route": "/health", "status_class": "2xx", "le": "0.25",
	}
	body, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": metric, "values": []any{[]any{1750000000, "3"}}}}}})
	series, err := decodeResponse(body, d)
	if err != nil || len(series) != 1 || series[0].Labels.LE != "0.25" {
		t.Fatalf("bucket label was not preserved: %#v, %v", series, err)
	}
	metric["le"] = "0.3"
	body, _ = json.Marshal(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": metric, "values": []any{[]any{1750000000, "3"}}}}}})
	if _, err := decodeResponse(body, d); err == nil {
		t.Fatal("unknown latency bucket accepted")
	}
}

func TestBackendAlertQueryPreservesFixedAlertSourceLabel(t *testing.T) {
	d, ok := definitions["gopulse_backend_alert_evaluation_known"]
	if !ok {
		t.Fatal("backend alert catalog missing")
	}
	metric := map[string]string{
		"__name__": "gopulse_backend_alert_evaluation_known", "source": "backend",
		"target_id": "backend-local", "producer_kind": "component", "producer_id": "backend",
		"alert_source": "metrics",
	}
	body, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{
		"resultType": "matrix", "result": []any{map[string]any{
			"metric": metric, "values": []any{[]any{1750000000, "1"}},
		}},
	}})
	series, err := decodeResponse(body, d)
	if err != nil || len(series) != 1 || series[0].Labels.AlertSource != "metrics" {
		t.Fatalf("alert source label was not preserved: %#v, %v", series, err)
	}
}

func TestMarshallerPartitionQueryPreservesFixedPartitionLabel(t *testing.T) {
	d, ok := definitions["gopulse_marshaller_partition_ownership"]
	if !ok {
		t.Fatal("marshaller partition catalog missing")
	}
	metric := map[string]string{
		"__name__": "gopulse_marshaller_partition_ownership", "source": "marshaller",
		"target_id": "marshaller-local", "producer_kind": "component", "producer_id": "marshaller",
		"partition": "3",
	}
	body, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{
		"resultType": "matrix", "result": []any{map[string]any{
			"metric": metric, "values": []any{[]any{1750000000, "1"}},
		}},
	}})
	series, err := decodeResponse(body, d)
	if err != nil || len(series) != 1 || series[0].Labels.Partition != "3" {
		t.Fatalf("partition label was not preserved: %#v, %v", series, err)
	}
}
