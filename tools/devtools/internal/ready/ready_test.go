package ready

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWaitAcceptsReadyEndpointAndSendsToken(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen <- request.Header.Get("Authorization")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := Wait(server.URL, 5*time.Second, "secret-token", "Backend", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if token := <-seen; token != "Bearer secret-token" {
		t.Errorf("readiness token = %q", token)
	}
}

func TestWaitReportsExitedProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := Wait(server.URL, 5*time.Second, "", "Backend", func() bool { return false })
	if err == nil {
		t.Fatal("a watched process that exited must fail readiness")
	}
	if !strings.Contains(err.Error(), "exited before readiness") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitTimesOutOnUnreadyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := Wait(server.URL, 300*time.Millisecond, "", "Monitor", nil)
	if err == nil {
		t.Fatal("an unready endpoint must fail readiness")
	}
	if !strings.Contains(err.Error(), "did not become ready") || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("unexpected error: %v", err)
	}
}
