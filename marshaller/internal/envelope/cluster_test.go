package envelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClusterV2Identity(t *testing.T) {
	for _, source := range []string{"mysql", "rabbitmq"} {
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
