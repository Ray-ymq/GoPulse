package middleware

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

type lockedLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedLogBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}
func (b *lockedLogBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
func TestOverloadLogsRetainFiveAndFlushWithoutMoreTraffic(t *testing.T) {
	output := &lockedLogBuffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	limiter := &overloadLogs{}
	// Begin near the start of a second so this burst belongs to one window.
	time.Sleep(time.Until(time.Unix(time.Now().Unix()+1, 0)) + time.Millisecond)
	for i := 0; i < 12; i++ {
		if got := limiter.allow(logger, "/posts/:id", apperror.CodeBackendBusy); got != (i < 5) {
			t.Fatalf("request %d allow=%t", i, got)
		}
	}
	if !limiter.allow(logger, "/other", apperror.CodeBackendBusy) || !limiter.allow(logger, "/posts/:id", apperror.CodeRateLimitUnavailable) {
		t.Fatal("independent tuple suppressed")
	}
	deadline := time.Now().Add(1500 * time.Millisecond)
	for !strings.Contains(output.String(), `"suppressed":7`) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(output.String(), `"suppressed":7`) {
		t.Fatalf("missing final summary: %s", output.String())
	}
	if expectedOverload(apperror.CodeInternal) {
		t.Fatal("internal errors must remain unsampled")
	}
}
