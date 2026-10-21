package collector

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/std_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
)

func fixture(t *testing.T, handler http.HandlerFunc) *Exporter {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	number, _ := strconv.Atoi(port)
	e := New(&config.Config{Host: host, Port: number, User: "demo", Password: "private", ConnectTimeout: 100 * time.Millisecond, ScrapeTimeout: time.Second})
	t.Cleanup(e.Stop)
	return e
}

func response(e *Exporter) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return w
}

func assertUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "# TYPE gopulse_std_up gauge\ngopulse_std_up 0\n" {
		t.Fatalf("失败响应不符合协议: %d %s", w.Code, w.Body.String())
	}
}

func TestRegistrationAndHealthDoNotConnect(t *testing.T) {
	var calls atomic.Int32
	e := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) })
	registry := prometheus.NewRegistry()
	if err := registry.Register(e); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	e.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if calls.Load() != 0 || w.Code != 200 || w.Body.String() != `{"status":"ok","service":"std-exporter"}` {
		t.Fatalf("健康检查或注册连接了目标: %d %s", calls.Load(), w.Body.String())
	}
}

func TestMetricsAuthenticationAndNoStaleSnapshot(t *testing.T) {
	var broken atomic.Bool
	e := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "demo" || password != "private" {
			w.WriteHeader(401)
			return
		}
		if broken.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/api/stats" {
			_, _ = w.Write([]byte(`{"value":42}`))
		}
	})
	success := response(e)
	if success.Code != 200 || !strings.Contains(success.Body.String(), "gopulse_std_up 1\n") || !strings.Contains(success.Body.String(), "gopulse_std_example_value 42\n") {
		t.Fatalf("采集失败: %d %s", success.Code, success.Body.String())
	}
	broken.Store(true)
	assertUnavailable(t, response(e))
}

func TestTimeoutCancelsTargetRequest(t *testing.T) {
	canceled := make(chan struct{})
	e := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			return
		}
		<-r.Context().Done()
		close(canceled)
	})
	e.Config.ScrapeTimeout = 120 * time.Millisecond
	started := time.Now()
	assertUnavailable(t, response(e))
	if time.Since(started) > time.Second {
		t.Fatal("采集未按超时结束")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("目标请求未被取消")
	}
}

func TestCanceledWaitDoesNotStartAnotherRound(t *testing.T) {
	e := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("等待期间访问了目标") })
	e.gate <- struct{}{}
	defer func() { <-e.gate }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx)
	e.Handler().ServeHTTP(w, req)
	assertUnavailable(t, w)
}

func TestTaskFailureDropsPartialMetrics(t *testing.T) {
	for _, panicTask := range []bool{false, true} {
		e := fixture(t, func(w http.ResponseWriter, r *http.Request) {})
		e.tasks = []namedScrape{{"partial", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
			ch <- prometheus.MustNewConstMetric(exampleDesc, prometheus.GaugeValue, 7)
			if panicTask {
				panic("private")
			}
			return errors.New("private")
		}}}
		assertUnavailable(t, response(e))
	}
}

func TestBoundedWorkersAndSerializedRounds(t *testing.T) {
	e := fixture(t, func(w http.ResponseWriter, r *http.Request) {})
	var active, maximum atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	e.tasks = nil
	for i := 0; i < 8; i++ {
		name := strconv.Itoa(i)
		desc := NewDesc("worker_"+name, "测试指标", nil)
		e.tasks = append(e.tasks, namedScrape{name, func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
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
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 1)
			return nil
		}})
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- response(e) }()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("并发采集未启动")
		}
	}
	close(release)
	wg.Wait()
	close(results)
	for w := range results {
		if w.Code != 200 {
			t.Fatalf("并发请求失败: %s", w.Body.String())
		}
	}
	if maximum.Load() != 4 {
		t.Fatalf("并发数错误: %d", maximum.Load())
	}
}

func TestInvalidMetricsAreRejected(t *testing.T) {
	cases := map[string][]prometheus.Metric{
		"重复序列": {prometheus.MustNewConstMetric(exampleDesc, prometheus.GaugeValue, 1), prometheus.MustNewConstMetric(exampleDesc, prometheus.GaugeValue, 2)},
		"非法前缀": {prometheus.MustNewConstMetric(prometheus.NewDesc("other_metric", "测试", nil, nil), prometheus.GaugeValue, 1)},
		"保留标签": {prometheus.MustNewConstMetric(NewDesc("bad", "测试", []string{"target_id"}), prometheus.GaugeValue, 1, "x")},
		"非有限值": {prometheus.MustNewConstMetric(exampleDesc, prometheus.GaugeValue, math.NaN())},
		"负计数器": {prometheus.MustNewConstMetric(NewDesc("bad_total", "测试", nil), prometheus.CounterValue, -1)},
		"时间戳":  {prometheus.NewMetricWithTimestamp(time.Now(), prometheus.MustNewConstMetric(exampleDesc, prometheus.GaugeValue, 1))},
		"超长标签": {prometheus.MustNewConstMetric(NewDesc("bad", "测试", []string{"name"}), prometheus.GaugeValue, 1, strings.Repeat("x", 257))},
		"重复状态": {prometheus.MustNewConstMetric(NewDesc("up", "测试", nil), prometheus.GaugeValue, 1)},
	}
	for name, metrics := range cases {
		t.Run(name, func(t *testing.T) {
			e := fixture(t, func(w http.ResponseWriter, r *http.Request) {})
			e.tasks = []namedScrape{{"invalid", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
				for _, metric := range metrics {
					ch <- metric
				}
				return nil
			}}}
			assertUnavailable(t, response(e))
		})
	}
}

func TestResponseBudget(t *testing.T) {
	e := fixture(t, func(w http.ResponseWriter, r *http.Request) {})
	e.tasks = []namedScrape{{"large", func(ctx context.Context, c *Client, ch chan<- prometheus.Metric) error {
		desc := NewDesc("large", "测试指标", []string{"id", "value"})
		for i := 0; i < 5000; i++ {
			metric := prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 1, strconv.Itoa(i), strings.Repeat("x", 256))
			select {
			case ch <- metric:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}}
	assertUnavailable(t, response(e))
}
