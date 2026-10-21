package collector

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
)

type ScrapeFn func(context.Context, *Client, chan<- prometheus.Metric) error

// ScrapeFns 在启动前注册，运行期间不得修改。新增采集项在此登记。
var ScrapeFns = map[string]ScrapeFn{
	"scrapeExample": scrapeExample,
}
