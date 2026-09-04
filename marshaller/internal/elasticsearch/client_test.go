package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	logtransform "github.com/Ray-ymq/GoPulse/marshaller/internal/logs"
)

func TestClientEnsuresFixedTemplateAndUsesMessageID(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/_index_template/") {
			w.Write([]byte(`{"acknowledged":true}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"_index":"gopulse-logs-v1-2026.09.04","_id":"abcdef0123456789abcdef0123456789","result":"created"}`))
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(logtransform.WriteRequest{MessageID: "abcdef0123456789abcdef0123456789", IndexDate: "2026.09.04", Document: json.RawMessage(`{"@timestamp":"2026-09-04T12:00:00Z"}`)})
	if err := client.Write(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "PUT /_index_template/"+TemplateName || paths[1] != "PUT /gopulse-logs-v1-2026.09.04/_doc/abcdef0123456789abcdef0123456789" {
		t.Fatalf("paths=%v", paths)
	}
}
