package collector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/exporters/mysql/common"
	"github.com/Ray-ymq/GoPulse/exporters/mysql/config"
	"github.com/Ray-ymq/GoPulse/exporters/mysql/logger"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const maxBodyBytes = 1 << 20
const maxSamples = 8192

var metricName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var logging = logger.GetStdLogger()

type ExporterFtx interface {
	prometheus.Collector
	Stop()
}

type Exporter struct {
	Config   *config.Config
	client   *Client
	targetUp *prometheus.Desc
	tasks    []namedScrape
	gate     chan struct{}
}
type namedScrape struct {
	name string
	fn   ScrapeFn
}

func New(conf *config.Config) (*Exporter, error) {
	client, err := NewClient(conf)
	if err != nil {
		return nil, err
	}
	return newExporter(conf, client), nil
}
func newExporter(conf *config.Config, client *Client) *Exporter {
	e := &Exporter{Config: conf, client: client, gate: make(chan struct{}, 1), targetUp: NewDesc("up", "本轮必需采集是否成功。", nil)}
	for name, fn := range ScrapeFns {
		e.tasks = append(e.tasks, namedScrape{name, fn})
	}
	sort.Slice(e.tasks, func(i, j int) bool { return e.tasks[i].name < e.tasks[j].name })
	return e
}

// Describe 不访问目标。动态采集函数采用 Prometheus 的 unchecked collector 模式。
func (e *Exporter) Describe(chan<- *prometheus.Desc) {}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), e.Config.ScrapeTimeout)
	defer cancel()
	metrics, err := e.snapshot(ctx)
	if err != nil {
		metrics = []prometheus.Metric{e.up(0)}
	}
	for _, m := range metrics {
		ch <- m
	}
}

func (e *Exporter) up(value float64) prometheus.Metric {
	return prometheus.MustNewConstMetric(e.targetUp, prometheus.GaugeValue, value)
}

func (e *Exporter) snapshot(ctx context.Context) ([]prometheus.Metric, error) {
	select {
	case e.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ErrUnavailable
	}
	defer func() { <-e.gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(e.tasks) == 0 {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	metrics := make(chan prometheus.Metric)
	jobs := make(chan namedScrape)
	var wg sync.WaitGroup
	var failed bool
	var mu sync.Mutex
	fail := func() { mu.Lock(); failed = true; mu.Unlock(); cancel() }
	for range min(4, len(e.tasks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				if ctx.Err() != nil {
					continue
				}
				err := invoke(ctx, task.fn, e.client, metrics)
				if err != nil {
					logging.Warn("scrape failed", "scraper", task.name)
					fail()
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, task := range e.tasks {
			select {
			case jobs <- task:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(metrics) }()
	result := []prometheus.Metric{e.up(1)}
	for metric := range metrics {
		if metric == nil || len(result) >= maxSamples {
			fail()
			continue
		}
		result = append(result, metric)
	}
	if failed || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	return result, nil
}

func invoke(ctx context.Context, fn ScrapeFn, client *Client, ch chan<- prometheus.Metric) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrUnavailable
		}
	}()
	if fn == nil {
		return ErrUnavailable
	}
	return fn(ctx, client, ch)
}

type snapshotCollector struct{ metrics []prometheus.Metric }

func (snapshotCollector) Describe(chan<- *prometheus.Desc) {}
func (c snapshotCollector) Collect(ch chan<- prometheus.Metric) {
	for _, m := range c.metrics {
		ch <- m
	}
}

func (e *Exporter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !validateRequest(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, healthBody())
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !validateRequest(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), e.Config.ScrapeTimeout)
		defer cancel()
		metrics, err := e.snapshot(ctx)
		var body []byte
		if err == nil {
			body, err = encode(metrics)
		}
		w.Header().Set("Content-Type", metricsContentType)
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "# TYPE "+common.NameSpace+"_up gauge\n"+common.NameSpace+"_up 0\n")
			return
		}
		_, _ = w.Write(body)
	})
	return mux
}

func healthBody() string { return `{"status":"ok","contract_version":"2"}` }

func encode(metrics []prometheus.Metric) ([]byte, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(snapshotCollector{metrics}); err != nil {
		return nil, ErrUnavailable
	}
	families, err := registry.Gather()
	if err != nil || len(families) == 0 || len(families) > 512 {
		return nil, ErrUnavailable
	}
	buffer := &limitedBuffer{}
	count := 0
	for _, family := range families {
		name := family.GetName()
		if len(name) > 128 || !metricName.MatchString(name) || !strings.HasPrefix(name, common.NameSpace+"_") || strings.HasSuffix(name, "_bucket") {
			return nil, ErrUnavailable
		}
		if family.GetType() != dto.MetricType_GAUGE && family.GetType() != dto.MetricType_COUNTER {
			return nil, ErrUnavailable
		}
		for _, metric := range family.Metric {
			count++
			if count > maxSamples || metric.TimestampMs != nil || len(metric.Label) > 16 {
				return nil, ErrUnavailable
			}
			for _, label := range metric.Label {
				key, value := label.GetName(), label.GetValue()
				if len(key) > 64 || !metricName.MatchString(key) || strings.HasPrefix(key, "__") || !utf8.ValidString(value) || len(value) > 256 || strings.ContainsRune(value, 0) {
					return nil, ErrUnavailable
				}
				switch key {
				case "source", "target_id", "producer_kind", "producer_id", "producer_version":
					return nil, ErrUnavailable
				}
			}
			var value float64
			if family.GetType() == dto.MetricType_GAUGE {
				value = metric.GetGauge().GetValue()
			} else {
				value = metric.GetCounter().GetValue()
				if value < 0 {
					return nil, ErrUnavailable
				}
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, ErrUnavailable
			}
			if name == common.NameSpace+"_up" && (len(family.Metric) != 1 || len(metric.Label) != 0 || family.GetType() != dto.MetricType_GAUGE || value != 1) {
				return nil, ErrUnavailable
			}
		}
		if _, err := expfmt.MetricFamilyToText(buffer, family); err != nil {
			return nil, ErrUnavailable
		}
	}
	return buffer.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxBodyBytes {
		return 0, errors.New("response_too_large")
	}
	return b.Buffer.Write(p)
}
func (b *limitedBuffer) WriteString(s string) (int, error) { return b.Write([]byte(s)) }
func (b *limitedBuffer) WriteByte(c byte) error            { _, err := b.Write([]byte{c}); return err }

func (e *Exporter) Stop()                                     { e.client.CloseIdleConnections() }
func (e *Exporter) CheckConnection(ctx context.Context) error { _, err := e.snapshot(ctx); return err }
func NewDesc(name, help string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(common.NameSpace, "", name), help, labels, nil)
}

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

func validateRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method_not_allowed", http.StatusMethodNotAllowed)
		return false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Body != nil && r.Body != http.NoBody {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return false
	}
	return true
}
