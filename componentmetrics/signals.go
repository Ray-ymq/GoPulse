package componentmetrics

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalContext owns the process signal handler. After the first signal has
// withdrawn readiness, restore the OS disposition so a second signal can force
// a wedged drain to terminate instead of being swallowed indefinitely.
func SignalContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() { <-ctx.Done(); stop() }()
	return ctx, stop
}
