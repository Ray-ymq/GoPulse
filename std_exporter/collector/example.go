package collector

import (
	"context"
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus"
	"math"
)

var exampleDesc = NewDesc("example_value", "示例目标返回的数值。", nil)

// scrapeExample 从目标的 /api/stats 读取 {"value": 42}。
func scrapeExample(ctx context.Context, client *Client, ch chan<- prometheus.Metric) error {
	data, err := client.FetchJSON(ctx, "/api/stats")
	if err != nil {
		return err
	}
	value, ok := getFloat64Value(data, "value")
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return ErrUnavailable
	}
	metric, err := prometheus.NewConstMetric(exampleDesc, prometheus.GaugeValue, value)
	if err != nil {
		return ErrUnavailable
	}
	select {
	case ch <- metric:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// 保留 json.Number 的使用示例，客户端改用 UseNumber 时可调用此函数。
func getFloat64Value(m map[string]interface{}, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case json.Number:
		f, err := v.Float64()
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	default:
		return 0, false
	}
}
