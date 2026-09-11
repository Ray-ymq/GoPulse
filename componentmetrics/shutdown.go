package componentmetrics

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// A process installs one budget alongside its root context. Consumer, HTTP,
// sampler and log shipper then spend the same deadline, not serial full budgets.
type ShutdownBudget struct {
	once    sync.Once
	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
}

var processBudget atomic.Pointer[ShutdownBudget]

func BindShutdown(root context.Context, timeout time.Duration) func() {
	b := &ShutdownBudget{timeout: timeout}
	processBudget.Store(b)
	done := make(chan struct{})
	go func() {
		select {
		case <-root.Done():
			b.begin()
		case <-done:
		}
	}()
	return func() { close(done); b.begin(); b.cancel(); processBudget.CompareAndSwap(b, nil) }
}
func (b *ShutdownBudget) begin() {
	b.once.Do(func() { b.ctx, b.cancel = context.WithTimeout(context.Background(), b.timeout) })
}
func ShutdownContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if b := processBudget.Load(); b != nil {
		b.begin()
		return b.ctx, func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}

// ReleaseShutdown is used by worker entry points after their execute wrapper
// has drained the log shipper (which outlives the consumer's run function).
func ReleaseShutdown() {
	if b := processBudget.Load(); b != nil {
		b.begin()
		b.cancel()
		processBudget.CompareAndSwap(b, nil)
	}
}
