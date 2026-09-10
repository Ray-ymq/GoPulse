package envelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClusterV2Identity(t *testing.T) {
	for _, source := range []string{"mysql", "rabbitmq", "kafka", "elasticsearch"} {
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

func TestElasticsearchOneHotHealth(t *testing.T) {
	p := Payload{PluginID: "elasticsearch-exporter", PluginVersion: "1.11.3", TargetID: "elasticsearch-exporter-local", ScrapeStatus: "success"}
	for name, rule := range rulesFor("elasticsearch") {
		if rule.count == 1 {
			p.Samples = append(p.Samples, Sample{Name: name, Kind: "gauge", Labels: map[string]string{}, Value: json.Number("1")})
			continue
		}
		for _, status := range []string{"green", "yellow", "red"} {
			value := json.Number("0")
			if status == "yellow" {
				value = "1"
			}
			p.Samples = append(p.Samples, Sample{Name: name, Kind: "gauge", Labels: map[string]string{"status": status}, Value: value})
		}
	}
	if err := validatePayload(&p); err != nil {
		t.Fatal(err)
	}
	for i := range p.Samples {
		if p.Samples[i].Labels["status"] == "green" {
			p.Samples[i].Value = "1"
		}
	}
	if err := validatePayload(&p); err == nil {
		t.Fatal("invalid health accepted")
	}
}
