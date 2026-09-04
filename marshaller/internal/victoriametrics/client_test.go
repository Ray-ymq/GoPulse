package victoriametrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteRequiresBasicAuth204AndEmptyBody(t *testing.T) {
	var gotBody, gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "user" || password != "password-password" {
			t.Error("missing auth")
		}
		gotType = r.Header.Get("Content-Type")
		data := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(data)
		gotBody = string(data)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := New(server.URL, "user", "password-password", time.Second)
	if err := client.Write(context.Background(), []byte("metric 1\n")); err != nil {
		t.Fatal(err)
	}
	if gotBody != "metric 1\n" || gotType != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("body=%q type=%q", gotBody, gotType)
	}
}
func TestWriteRejectsStatusBodyRedirectAndOversize(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "secret body", http.StatusUnauthorized) },
		"body": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("body"))
		},
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		},
		"oversize": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			if err := New(server.URL, "user", "password-password", time.Second).Write(context.Background(), nil); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe/unexpected error: %v", err)
			}
		})
	}
}
func TestReadyAcceptsBoundedHealthBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer server.Close()
	if err := New(server.URL, "user", "password-password", time.Second).Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}
