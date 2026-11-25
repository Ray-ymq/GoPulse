package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestLoggerEmitsJSON(t *testing.T) {
	var output bytes.Buffer
	Module(New("backend", &output), "http").Info("request completed", "request_id", "abc123", "status", 204)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["service"] != "backend" || record["module"] != "http" || record["level"] != "INFO" || record["msg"] != "request completed" || record["request_id"] != "abc123" || record["status"] != float64(204) {
		t.Fatalf("record = %#v", record)
	}
	if _, err := time.Parse(time.RFC3339Nano, record["time"].(string)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runtime_contract_version", "runtime_mode", "log_schema_version", "event", "instance_id"} {
		if _, ok := record[key]; ok {
			t.Fatalf("unexpected field %s", key)
		}
	}
}

func TestLoggerFiltersDebugAndContextUsesExplicitFallback(t *testing.T) {
	var output bytes.Buffer
	fallback := New("backend", &output)
	fallback.Debug("hidden")
	FromContext(context.Background(), fallback).Warn("fallback")
	FromContext(WithContext(context.Background(), Module(fallback, "request")), fallback).Error("scoped")

	decoder := json.NewDecoder(&output)
	var first, second map[string]any
	if err := decoder.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if first["msg"] != "fallback" || first["level"] != "WARN" {
		t.Fatalf("first = %#v", first)
	}
	if second["msg"] != "scoped" || second["module"] != "request" || second["level"] != "ERROR" {
		t.Fatalf("second = %#v", second)
	}
	if decoder.More() {
		t.Fatal("unexpected extra log record")
	}
}

func TestNilModuleLogger(t *testing.T) {
	if Module(nil, "test") != nil {
		t.Fatal("Module(nil) should remain nil")
	}
	_ = slog.Default()
}
