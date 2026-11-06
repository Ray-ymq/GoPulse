package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewEmitsSchemaV1Record(t *testing.T) {
	var output bytes.Buffer
	New("marshaller", &output).Info("started", "module", "lifecycle", "event", "started")
	var row map[string]any
	if err := json.Unmarshal(output.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["log_schema_version"] != float64(1) || row["service"] != "marshaller" || row["module"] != "lifecycle" || row["message"] != "started" {
		t.Fatalf("unexpected record: %#v", row)
	}
	if _, ok := row["timestamp"]; !ok {
		t.Fatal("missing timestamp")
	}
	if _, ok := row["time"]; ok {
		t.Fatal("unexpected time key")
	}
	if _, ok := row["msg"]; ok {
		t.Fatal("unexpected msg key")
	}
}
