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

func TestBackendDistributionSecondValidation(t *testing.T) {
	spec, ok := componentmetrics.Catalog("backend")
	if !ok {
		t.Fatal("backend catalog missing")
	}
	samples := []map[string]any{}
	for _, family := range spec.Families {
		if !family.Required {
			continue
		}
		for _, tuple := range family.Tuples {
			samples = append(samples, map[string]any{"name": family.Name, "kind": family.Kind, "labels": componentmetrics.Labels(family, tuple), "value": 0})
		}
	}
	var bucket, count, sum componentmetrics.Family
	for _, family := range spec.Families {
		if family.Distribution == nil {
			continue
		}
		switch family.Distribution.Role {
		case "bucket":
			bucket = family
		case "count":
			count = family
		case "sum":
			sum = family
		}
	}
	base := count.Tuples[0]
	for i, le := range bucket.Distribution.Buckets {
		value := 0.
		if i == len(bucket.Distribution.Buckets)-1 {
			value = 1
		}
		samples = append(samples, map[string]any{"name": bucket.Name, "kind": bucket.Kind, "labels": componentmetrics.Labels(bucket, append(append([]string(nil), base...), le)), "value": value})
	}
	samples = append(samples,
		map[string]any{"name": count.Name, "kind": count.Kind, "labels": componentmetrics.Labels(count, base), "value": 1.},
		map[string]any{"name": sum.Name, "kind": sum.Kind, "labels": componentmetrics.Labels(sum, base), "value": .25},
	)
	message := map[string]any{
		"schema_version": 2, "message_id": testID, "type": "metrics", "source": "backend",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"payload":   map[string]any{"producer_kind": "component", "producer_id": "backend", "producer_version": "2.1.1", "target_id": "backend-local", "scrape_status": "success", "samples": samples},
	}
	decode := func(value map[string]any) error {
		body, _ := json.Marshal(value)
		_, err := (Decoder{MaxBytes: 2 << 20, FutureSkew: time.Minute}).Decode([]byte(testID), body)
		return err
	}
	if err := decode(message); err != nil {
		t.Fatalf("valid backend distribution rejected: %v", err)
	}
	for _, name := range []string{"missing bucket", "foreign bucket"} {
		copyMessage, _ := json.Marshal(message)
		var mutated map[string]any
		_ = json.Unmarshal(copyMessage, &mutated)
		mutatedPayload := mutated["payload"].(map[string]any)
		mutatedSamples := mutatedPayload["samples"].([]any)
		for i := range mutatedSamples {
			item := mutatedSamples[i].(map[string]any)
			if item["name"] != bucket.Name {
				continue
			}
			labels := item["labels"].(map[string]any)
			if name == "missing bucket" && labels["le"] == "0.25" {
				mutatedSamples = append(mutatedSamples[:i], mutatedSamples[i+1:]...)
				break
			} else if name == "foreign bucket" && labels["le"] == "0.25" {
				labels["le"] = "private"
				break
			}
		}
		mutatedPayload["samples"] = mutatedSamples
		if err := decode(mutated); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
