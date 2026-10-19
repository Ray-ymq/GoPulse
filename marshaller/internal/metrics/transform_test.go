package metrics

import (
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	"strings"
	"testing"
	"time"
)

func TestUnregisteredMetricDecodesAndImportsItsDescriptor(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	body := []byte(`{"schema_version":2,"message_id":"` + id + `","type":"metrics","source":"rabbitmq","timestamp":"2026-10-14T08:00:00Z","payload":{"producer_kind":"exporter_plugin","producer_id":"rabbitmq-exporter","producer_version":"2.6.5","target_id":"rabbitmq-exporter-local","scrape_status":"success","samples":[{"name":"gopulse_rabbitmq_up","kind":"gauge","labels":{},"value":1},{"name":"gopulse_rabbitmq_queue_depth","kind":"gauge","labels":{"queue":"jobs","node":"node-2"},"value":7},{"name":"gopulse_rabbitmq_pending_messages","kind":"gauge","labels":{},"value":9}]}}`)
	message, err := (envelope.Decoder{Now: func() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC) }}).Decode([]byte(id), body)
	if err != nil {
		t.Fatal(err)
	}
	output, err := (Transformer{}).Transform(message)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `gopulse_rabbitmq_queue_depth{source="rabbitmq",target_id="rabbitmq-exporter-local",producer_kind="exporter_plugin",producer_id="rabbitmq-exporter",node="node-2",queue="jobs"} 7 `) {
		t.Fatal("new dimensions were not imported", string(output))
	}
	if !strings.Contains(string(output), `metric="gopulse_rabbitmq_queue_depth",kind="gauge",unit="none",label_keys="node,queue"`) {
		t.Fatal("type metadata was not imported", string(output))
	}
	if !strings.Contains(string(output), `gopulse_rabbitmq_pending_messages{source="rabbitmq",target_id="rabbitmq-exporter-local",producer_kind="exporter_plugin",producer_id="rabbitmq-exporter"} 9 `) {
		t.Fatal("unlabelled metric was not imported", string(output))
	}
	if !strings.Contains(string(output), `metric="gopulse_rabbitmq_pending_messages",kind="gauge",unit="none",label_keys=""`) {
		t.Fatal("unlabelled metric metadata was not imported", string(output))
	}
	// A duplicate must fail at the storage boundary, before it reaches the importer.
	var duplicate map[string]any
	_ = json.Unmarshal(body, &duplicate)
	p := duplicate["payload"].(map[string]any)
	samples := p["samples"].([]any)
	p["samples"] = append(samples, samples[1])
	invalid, _ := json.Marshal(duplicate)
	if _, err := (envelope.Decoder{FutureSkew: time.Minute}).Decode([]byte(id), invalid); err == nil {
		t.Fatal("duplicate accepted by storage ingress")
	}
}

func TestTransformDeterministicSortedAndMillisecondTimestamp(t *testing.T) {
	message := envelope.Envelope{Timestamp: time.Date(2026, 9, 4, 1, 2, 3, 987654321, time.UTC), Payload: envelope.Payload{Samples: []envelope.Sample{{Name: "metric_z", Labels: map[string]string{"mode": "user"}, FloatValue: 1.5}, {Name: "metric_a", Labels: map[string]string{"db": "0"}, FloatValue: 2}}}}
	body1, err := (Transformer{}).Transform(message)
	if err != nil {
		t.Fatal(err)
	}
	message.Payload.Samples[0], message.Payload.Samples[1] = message.Payload.Samples[1], message.Payload.Samples[0]
	body2, _ := (Transformer{}).Transform(message)
	if string(body1) != string(body2) {
		t.Fatalf("non deterministic:\n%s\n%s", body1, body2)
	}
	expectedPrefix := `metric_a{source="redis",target_id="redis-exporter-local",db="0"} 2 `
	if !strings.HasPrefix(string(body1), expectedPrefix) || !strings.Contains(string(body1), "1788483723987\n") {
		t.Fatalf("unexpected body: %s", body1)
	}
}
func TestTransformEscapesAndBoundsOutput(t *testing.T) {
	message := envelope.Envelope{Timestamp: time.Unix(0, 0).UTC(), Payload: envelope.Payload{Samples: []envelope.Sample{{Name: "metric", Labels: map[string]string{"label": "a\\b\n\"c"}, FloatValue: 1}}}}
	body, err := (Transformer{}).Transform(message)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `label="a\\b\n\"c"`) {
		t.Fatalf("not escaped: %s", body)
	}
	if _, err := (Transformer{MaxBytes: 8}).Transform(message); err != ErrOutputTooLarge {
		t.Fatalf("expected bound error, got %v", err)
	}
}

func TestRedisV2PreservesHistoricalStorageLabels(t *testing.T) {
	m := envelope.Envelope{SchemaVersion: 1, Source: "redis", Timestamp: time.Now().UTC(), Payload: envelope.Payload{TargetID: "redis-exporter-local", Samples: []envelope.Sample{{Name: "gopulse_redis_up", Kind: "gauge", FloatValue: 1, Labels: map[string]string{}}}}}
	old, err := (Transformer{}).Transform(m)
	if err != nil {
		t.Fatal(err)
	}
	m.SchemaVersion = 2
	m.Payload.ProducerKind = "exporter_plugin"
	m.Payload.ProducerID = "redis-exporter"
	m.Payload.ProducerVersion = "1.11.1"
	next, err := (Transformer{}).Transform(m)
	if err != nil || string(next) != string(old) || strings.Contains(strings.SplitN(string(next), "\n", 2)[0], "producer_") {
		t.Fatal("Redis series forked", err)
	}
}

func TestTransformPreservesFixedLatencyBucketSamples(t *testing.T) {
	m := envelope.Envelope{
		SchemaVersion: 2, Source: "backend", Timestamp: time.Unix(0, 0).UTC(),
		Payload: envelope.Payload{TargetID: "backend-local", ProducerKind: "component", ProducerID: "backend", ProducerVersion: "2.1.1", Samples: []envelope.Sample{
			{Name: "gopulse_backend_http_request_duration_seconds_bucket", Labels: map[string]string{"method": "GET", "route": "/health", "status_class": "2xx", "le": "0.25"}, FloatValue: 3},
			{Name: "gopulse_backend_http_request_duration_seconds_count", Labels: map[string]string{"method": "GET", "route": "/health", "status_class": "2xx"}, FloatValue: 3},
			{Name: "gopulse_backend_http_request_duration_seconds_sum", Labels: map[string]string{"method": "GET", "route": "/health", "status_class": "2xx"}, FloatValue: .75},
		}},
	}
	body, err := (Transformer{}).Transform(m)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `le="0.25"`) || !strings.Contains(text, `route="/health"`) || !strings.Contains(text, `producer_id="backend"`) {
		t.Fatalf("fixed distribution labels were not preserved: %s", text)
	}
}
