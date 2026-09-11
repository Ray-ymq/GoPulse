package runtime

import (
	"github.com/Ray-ymq/GoPulse/exporters/victoriametrics/internal/collector"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSafeUnavailableResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); w.Write([]byte("private-canary")) }))
	defer server.Close()
	db := &collector.Client{HTTP: server.Client(), Origin: server.URL, Username: "u", Password: "private-canary"}
	out := httptest.NewRecorder()
	Handler(db, time.Second).ServeHTTP(out, httptest.NewRequest("GET", "/metrics", nil))
	if out.Code != 503 || out.Body.String() != collector.Unavailable {
		t.Fatal("unsafe error response")
	}
}
func TestControlledOriginAndAuthentication(t *testing.T) {
	for k, v := range map[string]string{"GOPULSE_RUNTIME_MODE": "container", "VICTORIAMETRICS_HOST": "victoriametrics", "VICTORIAMETRICS_PORT": "8428", "VICTORIAMETRICS_USERNAME": "u", "VICTORIAMETRICS_PASSWORD": "private-canary", "VICTORIAMETRICS_EXPORTER_CONNECT_TIMEOUT": "1s", "VICTORIAMETRICS_EXPORTER_SCRAPE_TIMEOUT": "2s", "VICTORIAMETRICS_EXPORTER_HTTP_HOST": "127.0.0.1", "VICTORIAMETRICS_EXPORTER_HTTP_PORT": "9126"} {
		t.Setenv(k, v)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VICTORIAMETRICS_USERNAME", "")
	if _, err := Load(); err == nil {
		t.Fatal("unauthenticated configuration accepted")
	}
}
