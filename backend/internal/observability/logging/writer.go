package logging

import (
	"io"
	"log/slog"
)

type RemoteSink interface {
	Enqueue([]byte) bool
}

type shippingWriter struct {
	stdout io.Writer
	sink   RemoteSink
}

func (w shippingWriter) Write(body []byte) (int, error) {
	n, err := w.stdout.Write(body)
	if err == nil && n == len(body) && w.sink != nil {
		w.sink.Enqueue(body)
	}
	return n, err
}

// NewWithSink writes each Schema v1 record to stdout before attempting a
// non-blocking remote enqueue of the exact same JSON bytes.
func NewWithSink(service string, stdout io.Writer, sink RemoteSink) *slog.Logger {
	return New(service, shippingWriter{stdout: stdout, sink: sink})
}
