package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/collector"
)

type checkCollector struct{ fail bool }

func (c checkCollector) Collect(ctx context.Context) (collector.Snapshot, error) {
	if _, ok := ctx.Deadline(); !ok {
		return collector.Snapshot{}, errors.New("missing deadline")
	}
	if c.fail {
		return collector.Snapshot{}, errors.New("redis://phase14-secret-canary@internal-host")
	}
	return collector.Snapshot{UptimeSeconds: 1}, nil
}
func TestCheckSafeResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var out bytes.Buffer
		err := checkSource(context.Background(), checkCollector{fail}, time.Second, &out)
		expected := "{\"reachable\":true}\n"
		if fail {
			expected = "{\"reachable\":false,\"code\":\"target_unavailable\"}\n"
		}
		if out.String() != expected || (err != nil) != fail {
			t.Fatalf("unexpected result: %q %v", out.String(), err)
		}
	}
}
