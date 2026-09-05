package exporterplugin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

const testToken = "01234567890123456789012345678901"
const validStatusJSON = `{"id":"redis-exporter","name":"Redis Exporter","version":"1.8.2","kind":"metrics-exporter","source":"redis","desired_state":"running","observed_state":"running","installed_at":"2026-09-05T08:00:00Z","updated_at":"2026-09-05T08:01:00Z","started_at":"2026-09-05T08:00:10Z","last_scrape_at":"2026-09-05T08:01:00Z","last_success_at":"2026-09-05T08:01:00Z"}`

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, testToken, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientAcceptsStrictStatusAndList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Error("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "exporter-plugins") {
			fmt.Fprintf(w, `{"data":[%s]}`, validStatusJSON)
			return
		}
		fmt.Fprintf(w, `{"data":%s}`, validStatusJSON)
	})
	items, err := client.List(context.Background())
	if err != nil || len(items) != 1 || items[0].LastError != nil {
		t.Fatalf("unexpected list: %#v %v", items, err)
	}
	item, err := client.Get(context.Background(), "redis-exporter")
	if err != nil || item.Version != "1.8.2" || item.StartedAt == nil {
		t.Fatalf("unexpected status: %#v %v", item, err)
	}
}

func TestClientAcceptsKnownSafeError(t *testing.T) {
	body := strings.TrimSuffix(validStatusJSON, "}") + `,"observed_state":"failed","last_error":{"code":"process_exited","message":"plugin process exited unexpectedly","at":"2026-09-05T08:02:00Z"}}`
	body = strings.Replace(body, `"observed_state":"running",`, "", 1)
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, `{"data":%s}`, body) })
	item, err := client.Get(context.Background(), "redis-exporter")
	if err != nil || item.LastError == nil || item.LastError.Code != "process_exited" {
		t.Fatalf("unexpected safe error: %#v %v", item, err)
	}
}

func TestClientRejectsUntrustedSuccessfulResponses(t *testing.T) {
	tests := map[string]string{
		"duplicate nested key": `{"data":{"id":"redis-exporter","id":"redis-exporter"}}`,
		"unknown status field": strings.TrimSuffix(`{"data":`+validStatusJSON, "}") + `,"pid":123}}`,
		"trailing token":       `{"data":` + validStatusJSON + `} {}`,
		"unsafe last error":    `{"data":` + strings.TrimSuffix(validStatusJSON, "}") + `,"last_error":{"code":"secret","message":"/private/path","at":"2026-09-05T08:02:00Z"}}}`,
		"invalid state time":   `{"data":` + strings.Replace(validStatusJSON, `"started_at":"2026-09-05T08:00:10Z"`, `"started_at":null`, 1) + `}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := client.Get(context.Background(), "redis-exporter")
			assertCode(t, err, apperror.CodeMonitorUnavailable)
		})
	}
}

func TestClientRejectsOversizedAndRedirectResponses(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		})
		_, err := client.List(context.Background())
		assertCode(t, err, apperror.CodeMonitorUnavailable)
	})
	t.Run("redirect", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.com/private", http.StatusFound)
		})
		_, err := client.List(context.Background())
		assertCode(t, err, apperror.CodeMonitorUnavailable)
	})
}

func TestClientMapsOnlyStrictSafeMonitorErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"plugin_not_found","message":"internal"}}`))
	})
	_, err := client.Get(context.Background(), "redis-exporter")
	app, ok := apperror.As(err)
	if !ok || app.Code != apperror.CodePluginNotFound || app.Message == "internal" {
		t.Fatalf("unexpected mapped error: %#v", err)
	}
}

func TestClientDoesNotExposeUnavailableDetails(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", testToken, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.List(context.Background())
	assertCode(t, err, apperror.CodeMonitorUnavailable)
}

func TestNewClientRejectsUnsafeBaseURL(t *testing.T) {
	for _, value := range []string{"http://user@example.com", "http://example.com?token=x", "http://example.com/#fragment"} {
		if _, err := NewClient(value, testToken, time.Second); err == nil {
			t.Fatalf("accepted unsafe URL %q", value)
		}
	}
}

func assertCode(t *testing.T, err error, expected apperror.Code) {
	t.Helper()
	app, ok := apperror.As(err)
	if !ok || app.Code != expected {
		t.Fatalf("unexpected error: %#v", err)
	}
}
