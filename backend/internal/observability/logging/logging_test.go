package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"testing"
)

func TestLoggerEmitsSchemaAndProtectsReservedFields(t *testing.T) {
	var output bytes.Buffer
	logger := Module(New("backend", &output), "http").With(
		"service", "forged",
		"module", "forged",
		"request_id", "abc123",
	)
	logger.Info("request completed", "message", "forged", "status", 204)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %q", err, output.String())
	}
	if record["log_schema_version"] != float64(1) || record["service"] != "backend" || record["module"] != "http" {
		t.Fatalf("fixed fields = %#v", record)
	}
	if record["level"] != "info" || record["message"] != "request completed" || record["request_id"] != "abc123" || record["status"] != float64(204) {
		t.Fatalf("record = %#v", record)
	}
	if matched := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T.*Z$`).MatchString(record["timestamp"].(string)); !matched {
		t.Fatalf("timestamp = %q, want UTC RFC3339", record["timestamp"])
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
	if first["message"] != "fallback" || first["level"] != "warn" {
		t.Fatalf("first = %#v", first)
	}
	if second["message"] != "scoped" || second["module"] != "request" || second["level"] != "error" {
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
