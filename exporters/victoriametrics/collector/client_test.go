package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/victoriametrics/config"
)

func TestSafeUnavailableResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("private-canary"))
	}))
	defer server.Close()
	db := &Client{HTTP: server.Client(), Origin: server.URL, Username: "u", Password: "private-canary"}
	out := httptest.NewRecorder()
	newExporter(&config.Config{ScrapeTimeout: time.Second}, db).Handler().ServeHTTP(out, httptest.NewRequest("GET", "/metrics", nil))
	if out.Code != 503 || out.Body.String() != Unavailable {
		t.Fatal("unsafe error response")
	}
}
