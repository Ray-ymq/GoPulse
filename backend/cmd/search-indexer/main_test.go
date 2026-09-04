package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestExecuteShipsIndexerLogsAndRemoteFailureDoesNotFailOperation(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	code := execute(&stdout, func() (config.SearchIndexerConfig, error) {
		return config.SearchIndexerConfig{LogShip: config.LogShipConfig{
			Endpoint: server.URL, Token: "01234567890123456789012345678901",
			RequestTimeout: time.Second, QueueCapacity: 2, RetryMin: time.Second,
			RetryMax: time.Second, ShutdownTimeout: 20 * time.Millisecond,
		}}, nil
	}, func(_ config.SearchIndexerConfig, logger *slog.Logger) error {
		logger.Info("event processed", "event_id", "123e4567-e89b-12d3-a456-426614174000", "event_type", "post.created", "post_id", 7, "attempt", 0, "reason", "processed")
		return nil
	})
	if code != 0 {
		t.Fatalf("execute() = %d, want 0", code)
	}
	select {
	case <-started:
	default:
		t.Fatal("indexer log delivery did not start")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"service":"search-indexer"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"reason":"shutdown_timeout"`)) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
