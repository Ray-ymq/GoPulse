package envelope

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const testID = "0123456789abcdef0123456789abcdef"

func successJSON(timestamp string) string {
	return fmt.Sprintf(`{"schema_version":1,"message_id":"%s","type":"metrics","source":"redis","timestamp":"%s","payload":{"plugin_id":"redis-exporter","plugin_version":"1.5.1","target_id":"redis-exporter-local","scrape_status":"success","samples":[{"name":"gopulse_redis_up","kind":"gauge","labels":{},"value":1},{"name":"gopulse_redis_uptime_seconds","kind":"gauge","labels":{},"value":12},{"name":"gopulse_redis_connected_clients","kind":"gauge","labels":{},"value":2},{"name":"gopulse_redis_used_memory_bytes","kind":"gauge","labels":{},"value":1000},{"name":"gopulse_redis_commands_processed_total","kind":"counter","labels":{},"value":8},{"name":"gopulse_redis_keyspace_hits_total","kind":"counter","labels":{},"value":5},{"name":"gopulse_redis_keyspace_misses_total","kind":"counter","labels":{},"value":1},{"name":"gopulse_redis_cpu_seconds_total","kind":"counter","labels":{"mode":"user"},"value":1.25},{"name":"gopulse_redis_cpu_seconds_total","kind":"counter","labels":{"mode":"system"},"value":0.5},{"name":"gopulse_redis_db_keys","kind":"gauge","labels":{"db":"0"},"value":3},{"name":"gopulse_redis_db_expiring_keys","kind":"gauge","labels":{"db":"0"},"value":1}]}}`, testID, timestamp)
}
func TestDecodeSuccessAndUnavailable(t *testing.T) {
	now := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	d := Decoder{FutureSkew: 5 * time.Minute, Now: func() time.Time { return now }}
	message, err := d.Decode([]byte(testID), []byte(successJSON(now.Format(time.RFC3339Nano))))
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Payload.Samples) != 11 || message.Payload.Samples[7].FloatValue != 1.25 {
		t.Fatalf("unexpected message: %+v", message)
	}
	unavailable := fmt.Sprintf(`{"schema_version":1,"message_id":"%s","type":"metrics","source":"redis","timestamp":"%s","payload":{"plugin_id":"redis-exporter","plugin_version":"1.5.1","target_id":"redis-exporter-local","scrape_status":"target_unavailable","samples":[{"name":"gopulse_redis_up","kind":"gauge","labels":{},"value":0}]}}`, testID, now.Format(time.RFC3339Nano))
	if _, err := d.Decode([]byte(testID), []byte(unavailable)); err != nil {
		t.Fatal(err)
	}
}
func TestDecodeRejectsStrictContractViolations(t *testing.T) {
	now := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	valid := successJSON(now.Format(time.RFC3339Nano))
	tests := map[string]string{
		"duplicate nested key": strings.Replace(valid, `"labels":{"mode":"user"}`, `"labels":{"mode":"user","mode":"system"}`, 1),
		"unknown field":        strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"extra":true`, 1),
		"trailing token":       valid + ` {}`,
		"null labels":          strings.Replace(valid, `"labels":{}`, `"labels":null`, 1),
		"negative counter":     strings.Replace(valid, `"value":8`, `"value":-8`, 1),
		"partial success":      strings.Replace(valid, `,{"name":"gopulse_redis_db_expiring_keys","kind":"gauge","labels":{"db":"0"},"value":1}`, ``, 1),
		"reserved label":       strings.Replace(valid, `"labels":{"mode":"user"}`, `"labels":{"source":"redis"}`, 1),
	}
	d := Decoder{FutureSkew: 5 * time.Minute, Now: func() time.Time { return now }}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := d.Decode([]byte(testID), []byte(input)); Code(err) == "" {
				t.Fatalf("expected permanent error, got %v", err)
			}
		})
	}
	if _, err := d.Decode([]byte(strings.Repeat("a", 32)), []byte(valid)); Code(err) != "message_id_mismatch" {
		t.Fatalf("unexpected key error: %v", err)
	}
	future := successJSON(now.Add(6 * time.Minute).Format(time.RFC3339Nano))
	if _, err := d.Decode([]byte(testID), []byte(future)); Code(err) != "timestamp_too_far_future" {
		t.Fatalf("unexpected future error: %v", err)
	}
}
func TestDecodeRejectsOversizeAndInvalidUTF8(t *testing.T) {
	d := Decoder{MaxBytes: 4}
	if _, err := d.Decode([]byte(testID), []byte("12345")); Code(err) != "record_too_large" {
		t.Fatal(err)
	}
	d.MaxBytes = 10
	if _, err := d.Decode([]byte(testID), []byte{0xff}); Code(err) != "invalid_utf8" {
		t.Fatal(err)
	}
}
