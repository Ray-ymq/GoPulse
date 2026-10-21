package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

type ScrapeFn func(context.Context, *Client, chan<- prometheus.Metric) error

// ScrapeFns 在启动前登记。完整快照作为一个任务，保持相关指标的一致性。
var ScrapeFns = map[string]ScrapeFn{"snapshot": scrapeSnapshot}

func scrapeSnapshot(ctx context.Context, client *Client, ch chan<- prometheus.Metric) error {
	body, err := Collect(ctx, client)
	if err != nil {
		return err
	}
	return emitText(ctx, body, ch)
}
