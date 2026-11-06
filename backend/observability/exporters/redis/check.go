package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/collector"
)

func checkSource(ctx context.Context, source collector.Collector, timeout time.Duration, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := source.Collect(ctx); err != nil {
		// No raw error is returned or printed, even for an unexpected collector error.
		_, _ = fmt.Fprintln(output, `{"reachable":false,"code":"target_unavailable"}`)
		return errors.New("target unavailable")
	}
	_, err := fmt.Fprintln(output, `{"reachable":true}`)
	return err
}
