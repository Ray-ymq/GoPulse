package componentmetrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeConfigurationRejectsUnsafeInputs(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"GOPULSE_RUNTIME_MODE", "invalid-canary"}, {"ROUTER_SHUTDOWN_TIMEOUT", "30s"}, {"ROUTER_API_TOKEN", "short-canary"}} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			err := ValidateRuntimeEnvironment("router")
			if err == nil || strings.Contains(err.Error(), tc.value) {
				t.Fatal("unsafe configuration accepted or echoed")
			}
		})
	}
	t.Setenv("ROUTER_API_TOKEN", strings.Repeat("a", 32))
	t.Setenv("ROUTER_METRICS_TOKEN", strings.Repeat("a", 32))
	if ValidateRuntimeEnvironment("router") == nil {
		t.Fatal("credential reuse accepted")
	}
}
func TestHTTPCorrelationErrorsAndPanic(t *testing.T) {
	logger := NewLogger("router", &strings.Builder{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) { panic("secret-canary") })
	mux.HandleFunc("GET /forward", func(w http.ResponseWriter, r *http.Request) {
		next, err := NewRequest(r.Context(), "GET", "http://private.invalid", nil)
		if err != nil {
			t.Fatal(err)
		}
		if next.Header.Get("X-Request-ID") != w.Header().Get("X-Request-ID") {
			t.Fatal("lost propagated id")
		}
		WriteError(w, 503, "upstream_unavailable", "upstream unavailable")
	})
	h := HTTP(mux, logger)
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/panic", 500}, {"GET", "/missing", 404}, {"POST", "/forward", 405}, {"GET", "/forward", 503}} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("X-Request-ID", "bad-canary")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
		var b struct {
			Error struct {
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if json.Unmarshal(w.Body.Bytes(), &b) != nil || !ValidRequestID(b.Error.RequestID) || b.Error.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatal("invalid correlated error")
		}
		if strings.Contains(w.Body.String(), "canary") {
			t.Fatal("unsafe response")
		}
	}
	r := httptest.NewRequest("GET", "/forward", nil)
	r.Header.Set("X-Request-ID", strings.Repeat("a", 32))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("X-Request-ID") != strings.Repeat("a", 32) {
		t.Fatal("valid private ID replaced")
	}
}

func TestDependencyLogStateRateLimit(t *testing.T) {
	var output strings.Builder
	log := NewLogger("router", &output)
	log.Warn("connection unavailable")
	log.Warn("connection unavailable")
	log.Info("connection restored")
	log.Warn("connection unavailable")
	if strings.Count(output.String(), "\n") != 3 {
		t.Fatal("dependency transition or rate limit lost")
	}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			t.Fatal("not JSON")
		}
		for _, key := range []string{"log_schema_version", "timestamp", "level", "service", "module", "message", "event", "version", "revision"} {
			if record[key] == nil {
				t.Fatal("missing log field: " + key)
			}
		}
	}
}
