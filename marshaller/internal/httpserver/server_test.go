package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type checker struct{ err error }

func (c checker) Ready(context.Context) error { return c.err }
func TestHealthAndProtectedReadiness(t *testing.T) {
	token := "marshaller-token-at-least-32-bytes-long"
	s := New("127.0.0.1", 9093, token, time.Second, checker{}, checker{}, nil)
	health := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatal(health.Code)
	}
	unauthorized := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatal(unauthorized.Code)
	}
	readyReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	readyReq.Header.Set("Authorization", "Bearer "+token)
	ready := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(ready, readyReq)
	if ready.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", ready.Code, ready.Body.String())
	}
	cookieReq := httptest.NewRequest(http.MethodGet, "/ready", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "gopulse_session", Value: token})
	cookie := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(cookie, cookieReq)
	if cookie.Code != http.StatusUnauthorized {
		t.Fatal("cookie unexpectedly authorized")
	}
}
func TestReadinessUnavailableIsFinite(t *testing.T) {
	token := "marshaller-token-at-least-32-bytes-long"
	s := New("127.0.0.1", 9093, token, 20*time.Millisecond, checker{err: errors.New("down")}, checker{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, req)
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "{\"status\":\"unavailable\"}\n" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}
