package logship

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
)

// Runtime owns the optional remote shipper and the process logger that writes
// every record to stdout before offering an exact copy to the remote sink.
type synchronizedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *synchronizedWriter) Write(body []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(body)
}

type Runtime struct {
	Logger          *slog.Logger
	shipper         *Shipper
	shutdownTimeout time.Duration
}

// Open creates a stdout-only logger when Endpoint is empty, otherwise it starts
// the shared bounded shipper and returns a logger connected to it.
func Open(service string, stdout io.Writer, cfg Config) (*Runtime, error) {
	serialized := &synchronizedWriter{writer: stdout}
	base := logging.New(service, serialized)
	if cfg.Endpoint == "" {
		return &Runtime{Logger: base}, nil
	}
	shipper, err := New(cfg, logging.Module(base, "logship"))
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Logger:          logging.NewWithSink(service, serialized, shipper),
		shipper:         shipper,
		shutdownTimeout: cfg.ShutdownTimeout,
	}, nil
}

// Close drains accepted queue entries within the configured shutdown bound.
// Delivery failure remains a logging-side result and never changes business
// state that was already decided by the caller.
func (r *Runtime) Close() error {
	if r == nil || r.shipper == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
	defer cancel()
	return r.shipper.Close(ctx)
}
