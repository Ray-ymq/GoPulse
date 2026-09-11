package envelope

import (
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"testing"
	"time"
)

func componentMessage(t *testing.T) (map[string]any, []map[string]any) {
	t.Helper()
	spec, _ := componentmetrics.Catalog("monitor")
	samples := []map[string]any{}
	for _, f := range spec.Families {
		if !f.Required {
			continue
		}
		for _, tuple := range f.Tuples {
			v := 0.
			if f.Unit == "state" {
				v = -1
			}
			samples = append(samples, map[string]any{"name": f.Name, "kind": f.Kind, "labels": componentmetrics.Labels(f, tuple), "value": v})
		}
	}
	return map[string]any{"schema_version": 2, "message_id": "0123456789abcdef0123456789abcdef", "type": "metrics", "source": "monitor", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "payload": map[string]any{"producer_kind": "component", "producer_id": "monitor", "producer_version": "1.11.5", "target_id": "monitor-local", "scrape_status": "success", "samples": samples}}, samples
}
func TestComponentStorageIngressContract(t *testing.T) {
	decode := func(m map[string]any) error {
		body, _ := json.Marshal(m)
		_, err := (Decoder{MaxBytes: 1 << 20, FutureSkew: time.Minute}).Decode([]byte("0123456789abcdef0123456789abcdef"), body)
		return err
	}
	m, _ := componentMessage(t)
	if err := decode(m); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"family", "label_key", "label_value", "duplicate", "sample_budget", "nonfinite", "producer", "scraped"} {
		t.Run(name, func(t *testing.T) {
			m, samples := componentMessage(t)
			p := m["payload"].(map[string]any)
			switch name {
			case "family":
				samples = append(samples, map[string]any{"name": "private", "kind": "gauge", "labels": map[string]string{}, "value": 0})
			case "label_key":
				samples[0]["labels"].(map[string]string)["user_id"] = "42"
			case "label_value":
				samples[0]["labels"].(map[string]string)["scraped_target_id"] = "private-host"
			case "duplicate":
				samples = append(samples, samples[0])
			case "sample_budget":
				for len(samples) <= 112 {
					samples = append(samples, samples[0])
				}
			case "nonfinite":
				samples[0]["value"] = json.Number("1e9999")
			case "producer":
				p["producer_id"] = "backend"
			case "scraped":
				samples[0]["labels"] = map[string]string{"producer_kind": "component", "target_id": "monitor-local"}
			}
			p["samples"] = samples
			if err := decode(m); err == nil {
				t.Fatal("invalid component written")
			}
		})
	}
}
