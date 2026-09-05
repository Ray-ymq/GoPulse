package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	eventtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/events"
)

func TestEventsClientUsesIndependentTemplateIndexAndAlias(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/_index_template/"):
			w.Write([]byte(`{"acknowledged":true}`))
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			writeEventMappingResponse(t, w, "gopulse-events-v1-2026.09.05")
		case strings.Contains(r.URL.Path, "/_alias/"):
			w.Write([]byte(`{"gopulse-events-v1-2026.09.05":{"aliases":{"gopulse-events-v1-read":{}}}}`))
		default:
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"_index":"gopulse-events-v1-2026.09.05","_id":"abcdef0123456789abcdef0123456789","result":"created"}`))
		}
	}))
	defer server.Close()
	client, err := NewEvents(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	document := json.RawMessage(`{"@timestamp":"2026-09-05T08:00:00Z","event_schema_version":1,"event_name":"exporter_plugin_started","source":"monitor","severity":"info","message":"exporter plugin started","metadata":{"plugin_id":"redis-exporter","plugin_version":"1.7.1","operation":"start","from_state":"stopped","to_state":"running"}}`)
	body, _ := json.Marshal(eventtransform.WriteRequest{MessageID: "abcdef0123456789abcdef0123456789", IndexDate: "2026.09.05", Document: document})
	if err := client.Write(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	want := []string{"PUT /_index_template/" + EventTemplateName, "PUT /gopulse-events-v1-2026.09.05/_doc/abcdef0123456789abcdef0123456789", "GET /gopulse-events-v1-2026.09.05/_mapping", "GET /gopulse-events-v1-2026.09.05/_alias/" + EventReadAlias}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths=%v want=%v", paths, want)
	}
	if strings.Contains(eventTemplateBody, "gopulse-logs") {
		t.Fatal("event template references log storage")
	}
}

func writeEventMappingResponse(t *testing.T, w http.ResponseWriter, index string) {
	t.Helper()
	metadata := map[string]any{}
	for _, field := range []string{"plugin_id", "plugin_version", "previous_plugin_version", "operation", "from_state", "to_state"} {
		metadata[field] = map[string]string{"type": "keyword"}
	}
	properties := map[string]any{
		"@timestamp": map[string]string{"type": "date_nanos"}, "event_schema_version": map[string]string{"type": "integer"},
		"event_name": map[string]string{"type": "keyword"}, "source": map[string]string{"type": "keyword"}, "severity": map[string]string{"type": "keyword"}, "message": map[string]string{"type": "keyword"},
		"metadata": map[string]any{"dynamic": "strict", "properties": metadata},
	}
	if err := json.NewEncoder(w).Encode(map[string]any{index: map[string]any{"mappings": map[string]any{"dynamic": "strict", "properties": properties}}}); err != nil {
		t.Fatal(err)
	}
}
