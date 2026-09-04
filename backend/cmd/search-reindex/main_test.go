package main

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestExecuteRejectsArgumentsBeforeLoadingConfiguration(t *testing.T) {
	var stdout bytes.Buffer
	loaded := false
	code := execute([]string{"--unknown"}, &stdout, func() (config.ReindexConfig, error) {
		loaded = true
		return config.ReindexConfig{}, nil
	}, func(config.ReindexConfig, bool, *slog.Logger) error { return nil })
	if code != 1 || loaded {
		t.Fatalf("code=%d loaded=%v", code, loaded)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"reason":"invalid_arguments"`)) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestExecutePreservesReindexResultWhenShippingCannotDrain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	for _, test := range []struct {
		name     string
		err      error
		wantCode int
	}{
		{name: "success", wantCode: 0},
		{name: "failure", err: errors.New("reindex failed"), wantCode: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			code := execute([]string{"--if-missing"}, &stdout, func() (config.ReindexConfig, error) {
				return config.ReindexConfig{LogShip: config.LogShipConfig{
					Endpoint: server.URL, Token: "01234567890123456789012345678901",
					RequestTimeout: time.Second, QueueCapacity: 4, RetryMin: time.Second,
					RetryMax: time.Second, ShutdownTimeout: 20 * time.Millisecond,
				}}, nil
			}, func(_ config.ReindexConfig, ifMissing bool, logger *slog.Logger) error {
				if !ifMissing {
					t.Fatal("ifMissing = false")
				}
				logger.Info("search reindex started", "batch_size", 500)
				return test.err
			})
			if code != test.wantCode {
				t.Fatalf("execute() = %d, want %d", code, test.wantCode)
			}
			if !bytes.Contains(stdout.Bytes(), []byte(`"reason":"shutdown_timeout"`)) {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}
