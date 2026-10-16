package metricquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func descriptorRow(name, kind string) map[string]string {
	return map[string]string{"__name__": "gopulse_metric_metadata", "source": "rabbitmq", "target_id": "rabbitmq-exporter-local", "producer_kind": "exporter_plugin", "producer_id": "rabbitmq-exporter", "metric": name, "kind": kind, "unit": "none", "label_keys": "node,queue"}
}

func TestUnregisteredMetricDiscoveryAndQuery(t *testing.T) {
	const name = "gopulse_rabbitmq_queue_depth"
	row := descriptorRow(name, "gauge")
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != "POST" || !ok || user != "reader" || pass != "secret" {
			t.Error("invalid authenticated discovery request")
		}
		_ = r.ParseForm()
		calls = append(calls, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/series") {
			if r.Form.Get("limit") != "2049" || r.Form.Get("start") == "" || r.Form.Get("end") == "" {
				t.Error("discovery request has no bounds")
			}
			if selector := r.Form.Get("match[]"); selector != "gopulse_metric_metadata" && selector != `gopulse_metric_metadata{metric="`+name+`"}` {
				t.Error("unexpected selector", selector)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{row}})
			return
		}
		if r.Form.Get("query") != name+`{source="rabbitmq",target_id="rabbitmq-exporter-local",producer_kind="exporter_plugin",producer_id="rabbitmq-exporter"}` {
			t.Error("unscoped query", r.Form.Get("query"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": map[string]string{"__name__": name, "source": "rabbitmq", "target_id": "rabbitmq-exporter-local", "producer_kind": "exporter_plugin", "producer_id": "rabbitmq-exporter", "node": "node-2", "queue": "jobs"}, "values": [][]any{{1750000000, "7"}}}}}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "reader", "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(client)
	catalog, err := service.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range catalog {
		if d.Metric == name {
			found = d.Kind == "gauge" && reflect.DeepEqual(d.LabelKeys, []string{"node", "queue"})
		}
	}
	if !found {
		t.Fatal("unregistered metric absent from catalog")
	}
	options, err := ParseOptions(url.Values{"metric": {name}})
	if err != nil {
		t.Fatal("unregistered metric rejected", err)
	}
	result, err := service.Query(context.Background(), options)
	if err != nil || result.Kind != "gauge" || len(result.Series) != 1 || result.Series[0].Labels["queue"] != "jobs" || result.Series[0].Points[0].Value != 7 {
		t.Fatalf("dynamic query failed: %#v, %v", result, err)
	}
	if len(calls) != 3 {
		t.Fatal("unexpected upstream calls", calls)
	}
}

func TestUnlabelledMetricDiscoveryAndQuery(t *testing.T) {
	const name = "gopulse_rabbitmq_pending_messages"
	const labelledName = "gopulse_rabbitmq_queue_depth"
	for _, omitLabelKeys := range []bool{false, true} {
		testName := "empty label keys"
		if omitLabelKeys {
			testName = "storage omits empty label keys"
		}
		t.Run(testName, func(t *testing.T) {
			row := descriptorRow(name, "gauge")
			row["label_keys"] = ""
			if omitLabelKeys {
				delete(row, "label_keys")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				switch r.URL.Path {
				case "/prometheus/api/v1/series":
					rows := []map[string]string{row}
					switch r.Form.Get("match[]") {
					case "gopulse_metric_metadata":
						rows = append(rows, descriptorRow(labelledName, "gauge"))
					case `gopulse_metric_metadata{metric="` + name + `"}`:
					default:
						t.Error("unexpected selector", r.Form.Get("match[]"))
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": rows})
				case "/prometheus/api/v1/query_range":
					if r.Form.Get("query") != name+`{source="rabbitmq",target_id="rabbitmq-exporter-local",producer_kind="exporter_plugin",producer_id="rabbitmq-exporter"}` {
						t.Error("unscoped query", r.Form.Get("query"))
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": map[string]string{"__name__": name, "source": "rabbitmq", "target_id": "rabbitmq-exporter-local", "producer_kind": "exporter_plugin", "producer_id": "rabbitmq-exporter"}, "values": [][]any{{1750000000, "7"}}}}}})
				default:
					t.Error("unexpected upstream path", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "reader", "secret", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(client)
			catalog, err := service.Catalog(context.Background())
			if err != nil {
				t.Fatal("catalog failed for unlabelled metric", err)
			}
			definitions := make(map[string]Definition, len(catalog))
			for _, d := range catalog {
				definitions[d.Metric] = d
			}
			if d, ok := definitions[name]; !ok || d.Kind != "gauge" || len(d.LabelKeys) != 0 {
				t.Fatalf("unlabelled metric absent or invalid: %#v", d)
			}
			if d, ok := definitions[labelledName]; !ok || !reflect.DeepEqual(d.LabelKeys, []string{"node", "queue"}) {
				t.Fatalf("labelled metric absent or invalid: %#v", d)
			}
			options, err := ParseOptions(url.Values{"metric": {name}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Query(context.Background(), options)
			if err != nil || result.Kind != "gauge" || len(result.Series) != 1 {
				t.Fatalf("unlabelled metric query failed: %#v, %v", result, err)
			}
			if !reflect.DeepEqual(result.Series[0].Labels, Labels{}) || len(result.Series[0].Points) != 1 || result.Series[0].Points[0].Value != 7 {
				t.Fatalf("unexpected unlabelled metric series: %#v", result.Series[0])
			}
		})
	}
}

func TestDiscoveryRejectsForeignAndConflictingMetadata(t *testing.T) {
	for _, name := range []string{"source", "type", "reserved label", "unknown field", "unknown field without label keys", "missing required field", "conflict", "trailing"} {
		t.Run(name, func(t *testing.T) {
			row := descriptorRow("gopulse_rabbitmq_queue_depth", "gauge")
			rows := []map[string]string{row}
			switch name {
			case "source":
				row["producer_id"] = "redis-exporter"
			case "type":
				row["kind"] = "script"
			case "reserved label":
				row["label_keys"] = "producer_id"
			case "unknown field":
				row["extra"] = ""
			case "unknown field without label keys":
				delete(row, "label_keys")
				row["extra"] = ""
			case "missing required field":
				delete(row, "target_id")
			case "conflict":
				rows = append(rows, descriptorRow(row["metric"], "counter"))
			}
			body, _ := json.Marshal(map[string]any{"status": "success", "data": rows})
			if name == "trailing" {
				body = append(body, []byte(" {}")...)
			}
			if _, err := decodeDefinitions(body, ""); err == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}

func TestMetricNamesCannotInjectQueries(t *testing.T) {
	for _, name := range []string{`gopulse_redis_up{source="other"}`, "sum(gopulse_redis_up)", "gopulse_metric_metadata", "gopulse_other_up", "gopulse_redis_", "gopulse_redis_up\n"} {
		if _, err := ParseOptions(url.Values{"metric": {name}}); err == nil || QueryExpression(name) != "" {
			t.Fatal("invalid metric accepted", name)
		}
	}
}

func TestDiscoveryPreservesHistoricalDependencyStateUnit(t *testing.T) {
	row := map[string]string{"__name__": "gopulse_metric_metadata", "source": "monitor", "target_id": "monitor-local", "producer_kind": "component", "producer_id": "monitor", "metric": "gopulse_monitor_dependency_up", "kind": "gauge", "unit": "boolean", "label_keys": "dependency"}
	body, _ := json.Marshal(map[string]any{"status": "success", "data": []map[string]string{row}})
	definitions, err := decodeDefinitions(body, "")
	if err != nil || len(definitions) != 1 || definitions[0].Unit != "state" {
		t.Fatalf("historical dependency unit changed: %#v, %v", definitions, err)
	}
}
