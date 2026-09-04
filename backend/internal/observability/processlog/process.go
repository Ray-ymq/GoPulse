package processlog

import (
	"io"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logship"
)

// Open adapts application configuration to the shared process logging runtime.
func Open(service string, stdout io.Writer, cfg config.LogShipConfig) (*logship.Runtime, error) {
	return logship.Open(service, stdout, logship.Config{
		Endpoint: cfg.Endpoint, Token: cfg.Token, RequestTimeout: cfg.RequestTimeout,
		QueueCapacity: cfg.QueueCapacity, RetryMin: cfg.RetryMin, RetryMax: cfg.RetryMax,
		ShutdownTimeout: cfg.ShutdownTimeout,
	})
}
