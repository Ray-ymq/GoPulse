package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadMetricsPreservesHTTPFailureAndRejectsOverflow(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		failed bool
	}{
		{"success", 200, "# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 1\n", false},
		{"failed scrape", 503, "# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 0\n", false},
		{"overflow", 200, strings.Repeat("x", (1<<20)+1), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			status, body, err := readMetrics(context.Background(), server.Client(), server.URL+"/metrics")
			if (err != nil) != test.failed {
				t.Fatal("unexpected read result", err)
			}
			if !test.failed && (status != test.status || body != test.body) {
				t.Fatal("source status or data rewritten")
			}
		})
	}
}

func TestMetricsRequestHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := readMetrics(ctx, server.Client(), server.URL+"/metrics"); err == nil {
		t.Fatal("cancellation ignored")
	}
}
