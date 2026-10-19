package envelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClusterV2Identity(t *testing.T) {
	for _, source := range []string{"mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"} {
		payload := map[string]any{"producer_kind": "exporter_plugin", "producer_id": source + "-exporter", "producer_version": "1.11.2", "target_id": source + "-exporter-local", "scrape_status": "target_unavailable", "samples": []map[string]any{{"name": "gopulse_" + source + "_up", "kind": "gauge", "labels": map[string]string{}, "value": 0}}}
		message := map[string]any{"schema_version": 2, "message_id": testID, "type": "metrics", "source": source, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "payload": payload}
		body, _ := json.Marshal(message)
		if _, err := (Decoder{FutureSkew: time.Minute}).Decode([]byte(testID), body); err != nil {
			t.Fatal(err)
		}
		payload["producer_id"] = "redis-exporter"
		body, _ = json.Marshal(message)
		if _, err := (Decoder{FutureSkew: time.Minute}).Decode([]byte(testID), body); Code(err) != "invalid_producer" {
			t.Fatal("foreign producer accepted", err)
		}
	}
}

func TestExporterPayloadAcceptsNewFamiliesAndDimensions(t *testing.T) {
	for _, source := range []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"} {
		p := Payload{PluginID: source + "-exporter", PluginVersion: "1.11.3", TargetID: source + "-exporter-local", ScrapeStatus: "success", Samples: []Sample{
			{Name: "gopulse_" + source + "_up", Kind: "gauge", Labels: map[string]string{}, Value: json.Number("1")},
			{Name: "gopulse_" + source + "_new_depth", Kind: "gauge", Labels: map[string]string{"node": "node-2", "partition": "3"}, Value: json.Number("7")},
		}}
		if err := validatePayload(&p); err != nil {
			t.Fatal(source, err)
		}
		p.Samples[1].Labels["source"] = "foreign"
		if err := validatePayload(&p); err == nil {
			t.Fatal("reserved label accepted")
		}
	}
}
