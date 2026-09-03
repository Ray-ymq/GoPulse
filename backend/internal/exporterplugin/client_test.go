package exporterplugin

import (
	"context"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientMapsSafeMonitorErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer 01234567890123456789012345678901" {
			t.Error("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"plugin_not_found","message":"internal"}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "01234567890123456789012345678901", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(context.Background(), "redis-exporter")
	app, ok := apperror.As(err)
	if !ok || app.Code != apperror.CodePluginNotFound || app.Message == "internal" {
		t.Fatalf("unexpected mapped error: %#v", err)
	}
}
func TestClientDoesNotExposeUnavailableDetails(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", "01234567890123456789012345678901", 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.List(context.Background())
	app, ok := apperror.As(err)
	if !ok || app.Code != apperror.CodeMonitorUnavailable {
		t.Fatalf("unexpected error: %v", err)
	}
}
