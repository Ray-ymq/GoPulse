package componentmetrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestEndpointContract(t *testing.T) {
	calls := 0
	const exposition = "gopulse_business_worker_messages_in_flight 0\n"
	handler, err := NewHandler(testToken, 1024, func() ([]byte, bool) { calls++; return []byte(exposition), true })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, body string
		auth                     []string
		want                     int
	}{
		{"missing auth precedes path", "POST", "/unknown?x=1", "body", nil, 401},
		{"wrong auth", "GET", Path, "", []string{"Bearer wrong"}, 401},
		{"duplicate auth", "GET", Path, "", []string{"Bearer " + testToken, "Bearer " + testToken}, 401},
		{"path precedes method", "POST", "/unknown?x=1", "body", []string{"Bearer " + testToken}, 404},
		{"method precedes query", "POST", Path + "?x=1", "body", []string{"Bearer " + testToken}, 405},
		{"query", "GET", Path + "?x=1", "", []string{"Bearer " + testToken}, 400},
		{"empty query", "GET", Path + "?", "", []string{"Bearer " + testToken}, 400},
		{"body", "GET", Path, "body", []string{"Bearer " + testToken}, 400},
		{"success", "GET", Path, "", []string{"Bearer " + testToken}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			for _, a := range tc.auth {
				request.Header.Add("Authorization", a)
			}
			response := httptest.NewRecorder()
			before := calls
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d; want %d", response.Code, tc.want)
			}
			if strings.Contains(response.Body.String(), testToken) {
				t.Fatal("credential leaked")
			}
			if tc.want != 200 && calls != before {
				t.Fatal("rejected request collected metrics")
			}
			if tc.want == 200 && (response.Body.String() != exposition || response.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8") {
				t.Fatal("invalid exposition response")
			}
		})
	}
}

func TestUnavailableAndBodyBudget(t *testing.T) {
	for _, tc := range []struct {
		body      string
		available bool
	}{{"", false}, {"12345", true}} {
		handler, err := NewHandler(testToken, 4, func() ([]byte, bool) { return []byte(tc.body), tc.available })
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", Path, nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 503 || response.Body.String() != "metrics unavailable\n" {
			t.Fatalf("unsafe unavailable response: %v", response)
		}
	}
	if _, err := NewHandler("short", 1024, func() ([]byte, bool) { return nil, false }); err == nil {
		t.Fatal("short token accepted")
	}
}

func TestFixedAddresses(t *testing.T) {
	for component, port := range map[string]string{"backend": "19101", "business-worker": "19102", "search-indexer": "19103"} {
		for mode, host := range map[string]string{"host": "127.0.0.1", "container": "0.0.0.0"} {
			got, err := Address(mode, component)
			if err != nil || got != net.JoinHostPort(host, port) {
				t.Fatalf("%s/%s: %q %v", mode, component, got, err)
			}
		}
	}
	if _, err := Address("host", "arbitrary"); err == nil {
		t.Fatal("arbitrary component accepted")
	}
	if _, err := Address("arbitrary", "backend"); err == nil {
		t.Fatal("arbitrary runtime mode accepted")
	}
}

func TestListenerStartupAndSharedShutdown(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") })
	if _, err := start(root, occupied.Addr().String(), handler); err == nil {
		t.Fatal("listener conflict not reported at startup")
	}
	listener, err := start(root, "127.0.0.1:0", handler)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := listener.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-listener.done:
	default:
		t.Fatal("HTTP server still running")
	}
}
