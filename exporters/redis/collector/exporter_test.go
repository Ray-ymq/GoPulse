package collector

import (
	"context"
	"errors"
	"math"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/common"
	"github.com/Ray-ymq/GoPulse/exporters/redis/config"
	"github.com/prometheus/client_golang/prometheus"
)

func requestMetrics(e *Exporter) *httptest.ResponseRecorder {
	out := httptest.NewRecorder()
	e.Handler().ServeHTTP(out, httptest.NewRequest("GET", "/metrics", nil))
	return out
}
func taskExporter(tasks ...namedScrape) *Exporter {
	e := newExporter(&config.Config{ScrapeTimeout: time.Second}, &Client{})
	e.tasks = tasks
	return e
}
func checkFailure(t *testing.T, out *httptest.ResponseRecorder) {
	t.Helper()
	if out.Code != 503 || out.Body.String() != Unavailable {
		t.Fatalf("失败响应不符合协议: %d %s", out.Code, out.Body.String())
	}
}
func TestExporterRegistrationAndHealthDoNotCollect(t *testing.T) {
	var calls atomic.Int32
	e := taskExporter(namedScrape{"unexpected", func(context.Context, *Client, chan<- prometheus.Metric) error { calls.Add(1); return nil }})
	registry := prometheus.NewRegistry()
	if err := registry.Register(e); err != nil {
		t.Fatal(err)
	}
	out := httptest.NewRecorder()
	e.Handler().ServeHTTP(out, httptest.NewRequest("GET", "/health", nil))
	if calls.Load() != 0 || out.Code != 200 {
		t.Fatal("注册或健康检查触发采集")
	}
}
func TestExporterFailureDropsPartialSnapshot(t *testing.T) {
	desc := NewDesc("test_value", "测试指标", nil)
	for _, panicTask := range []bool{false, true} {
		e := taskExporter(namedScrape{"partial", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 42)
			if panicTask {
				panic("private")
			}
			return errors.New("private")
		}})
		checkFailure(t, requestMetrics(e))
	}
}
func TestExporterCancellation(t *testing.T) {
	e := taskExporter(namedScrape{"blocked", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	e.Config.ScrapeTimeout = 50 * time.Millisecond
	started := time.Now()
	checkFailure(t, requestMetrics(e))
	if time.Since(started) > time.Second {
		t.Fatal("超时未取消采集")
	}
	e.gate <- struct{}{}
	defer func() { <-e.gate }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out := httptest.NewRecorder()
	e.Handler().ServeHTTP(out, httptest.NewRequest("GET", "/metrics", nil).WithContext(ctx))
	checkFailure(t, out)
}
func TestExporterConcurrentRequestsStayWithinWorkerLimit(t *testing.T) {
	e := taskExporter()
	var active, maximum atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	for i := 0; i < 8; i++ {
		desc := NewDesc("worker_"+strconv.Itoa(i), "测试指标", nil)
		e.tasks = append(e.tasks, namedScrape{strconv.Itoa(i), func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 1):
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}})
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- requestMetrics(e) }()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("采集未启动")
		}
	}
	close(release)
	wg.Wait()
	close(results)
	for out := range results {
		if out.Code != 200 {
			t.Fatalf("采集失败: %s", out.Body.String())
		}
	}
	if maximum.Load() != 4 {
		t.Fatalf("并发数错误: %d", maximum.Load())
	}
}
func TestExporterRejectsInvalidMetrics(t *testing.T) {
	desc := NewDesc("test_value", "测试", nil)
	cases := map[string][]prometheus.Metric{
		"重复序列": {prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 1), prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 2)},
		"NaN":  {prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, math.NaN())},
		"保留标签": {prometheus.MustNewConstMetric(NewDesc("bad", "测试", []string{"target_id"}), prometheus.GaugeValue, 1, "x")},
		"非法前缀": {prometheus.MustNewConstMetric(prometheus.NewDesc("other_metric", "测试", nil, nil), prometheus.GaugeValue, 1)},
	}
	for name, metrics := range cases {
		t.Run(name, func(t *testing.T) {
			e := taskExporter(namedScrape{"invalid", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
				for _, m := range metrics {
					ch <- m
				}
				return nil
			}})
			checkFailure(t, requestMetrics(e))
		})
	}
}
func TestSnapshotAdapterPreservesMetricValues(t *testing.T) {
	body := "# TYPE " + common.NameSpace + "_up gauge\n" + common.NameSpace + "_up 1\n# TYPE " + common.NameSpace + "_test_total counter\n" + common.NameSpace + "_test_total{label=\"value\"} 42\n"
	e := taskExporter(namedScrape{"snapshot", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
		return emitText(ctx, body, ch)
	}})
	out := requestMetrics(e)
	if out.Code != 200 || !strings.Contains(out.Body.String(), common.NameSpace+`_test_total{label="value"} 42`) {
		t.Fatalf("指标适配改变了样本: %s", out.Body.String())
	}
}
