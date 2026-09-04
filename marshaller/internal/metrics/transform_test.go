package metrics

import (
	"github.com/Ray-ymq/GoPulse/marshaller/internal/envelope"
	"strings"
	"testing"
	"time"
)

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
