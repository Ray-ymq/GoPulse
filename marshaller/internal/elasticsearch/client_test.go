package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	logtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/logs"
)

func TestClientEnsuresFixedTemplateAndVerifiesWrittenIndex(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/_index_template/"):
			w.Write([]byte(`{"acknowledged":true}`))
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			writeMappingResponse(t, w, "gopulse-logs-v1-2026.09.04")
		case strings.Contains(r.URL.Path, "/_alias/"):
			w.Write([]byte(`{"gopulse-logs-v1-2026.09.04":{"aliases":{"gopulse-logs-v1-read":{}}}}`))
		default:
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"_index":"gopulse-logs-v1-2026.09.04","_id":"abcdef0123456789abcdef0123456789","result":"created"}`))
		}
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body := writeRequest(t, "2026.09.04")
	if err := client.Write(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PUT /_index_template/" + TemplateName,
		"PUT /gopulse-logs-v1-2026.09.04/_doc/abcdef0123456789abcdef0123456789",
		"GET /gopulse-logs-v1-2026.09.04/_mapping",
		"GET /gopulse-logs-v1-2026.09.04/_alias/" + ReadAlias,
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths=%v, want=%v", paths, want)
	}
}

func TestClientReestablishesTemplateAfterLiveClusterReset(t *testing.T) {
	var mu sync.Mutex
	templatePuts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/_index_template/"):
			templatePuts++
			w.Write([]byte(`{"acknowledged":true}`))
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			index := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/_mapping")
			writeMappingResponse(t, w, index)
		case strings.Contains(r.URL.Path, "/_alias/"):
			index := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
			json.NewEncoder(w).Encode(map[string]any{index: map[string]any{"aliases": map[string]any{ReadAlias: map[string]any{}}}})
		default:
			index := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"_index": index, "_id": "abcdef0123456789abcdef0123456789", "result": "created"})
		}
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(context.Background(), writeRequest(t, "2026.09.04")); err != nil {
		t.Fatal(err)
	}
	// The fake cluster loses all external state while this Client remains alive.
	if err := client.Write(context.Background(), writeRequest(t, "2026.09.05")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if templatePuts != 2 {
		t.Fatalf("template PUT count = %d, want one ensure per write", templatePuts)
	}
}

func TestClientRejectsWrittenIndexWithoutReadAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/_index_template/"):
			w.Write([]byte(`{"acknowledged":true}`))
		case strings.HasSuffix(r.URL.Path, "/_mapping"):
			writeMappingResponse(t, w, "gopulse-logs-v1-2026.09.04")
		case strings.Contains(r.URL.Path, "/_alias/"):
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"alias missing"}`))
		default:
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"_index":"gopulse-logs-v1-2026.09.04","_id":"abcdef0123456789abcdef0123456789","result":"created"}`))
		}
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(context.Background(), writeRequest(t, "2026.09.04")); err == nil {
		t.Fatal("write succeeded without the fixed read alias")
	}
}

func writeRequest(t *testing.T, date string) []byte {
	t.Helper()
	body, err := json.Marshal(logtransform.WriteRequest{
		MessageID: "abcdef0123456789abcdef0123456789",
		IndexDate: date,
		Document:  json.RawMessage(`{"@timestamp":"2026-09-04T12:00:00Z"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func writeMappingResponse(t *testing.T, w http.ResponseWriter, index string) {
	t.Helper()
	properties := make(map[string]any, len(requiredPropertyTypes))
	for field, propertyType := range requiredPropertyTypes {
		properties[field] = map[string]string{"type": propertyType}
	}
	if err := json.NewEncoder(w).Encode(map[string]any{
		index: map[string]any{"mappings": map[string]any{"dynamic": "strict", "properties": properties}},
	}); err != nil {
		t.Fatal(err)
	}
}
