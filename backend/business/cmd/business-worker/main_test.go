package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestExecuteShipsWorkerLogsWithoutChangingOperationResult(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation func(config.WorkerConfig, *slog.Logger) error
		wantCode  int
	}{
		{name: "success", operation: func(_ config.WorkerConfig, logger *slog.Logger) error {
			logger.Info("event processed", "event_id", "123e4567-e89b-12d3-a456-426614174000", "event_type", "comment.created", "attempt", 0, "reason", "processed")
			return nil
		}, wantCode: 0},
		{name: "failure", operation: func(config.WorkerConfig, *slog.Logger) error { return errors.New("business failed") }, wantCode: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			received := make(chan []byte, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				received <- body
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()
			var stdout bytes.Buffer
			code := execute(&stdout, func() (config.WorkerConfig, error) {
				return config.WorkerConfig{LogShip: testLogShipConfig(server.URL)}, nil
			}, test.operation)
			if code != test.wantCode {
				t.Fatalf("execute() = %d, want %d", code, test.wantCode)
			}
			select {
			case body := <-received:
				if !bytes.Contains(body, []byte(`"service":"business-worker"`)) {
					t.Fatalf("remote body = %q", body)
				}
			default:
				t.Fatal("worker log was not shipped")
			}
		})
	}
}

func testLogShipConfig(endpoint string) config.LogShipConfig {
	return config.LogShipConfig{
		Endpoint: endpoint, Token: "01234567890123456789012345678901",
		RequestTimeout: time.Second, QueueCapacity: 8, RetryMin: time.Millisecond,
		RetryMax: time.Millisecond, ShutdownTimeout: time.Second,
	}
}
