package collector

import (
	"context"
	"errors"
	"github.com/Ray-ymq/GoPulse/monitor/internal/instrumentation"
	"github.com/Ray-ymq/GoPulse/runtimekit"
	"github.com/Ray-ymq/GoPulse/runtimekit/metriccontract"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/events"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

const MaxResponseBytes int64 = metriccontract.MaxBodyBytes

type Update struct {
	ScrapeAt     *time.Time
	SuccessAt    *time.Time
	ErrorCode    string
	ErrorMessage string
}

type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}
type realTicker struct{ *time.Ticker }

func (t realTicker) Chan() <-chan time.Time { return t.C }

type EventRecorder interface {
	Record(events.Event) bool
}

type Config struct {
	Source         string
	Host           string
	Port           string
	Interval       time.Duration
	Timeout        time.Duration
	PublishTimeout time.Duration
	Publisher      publisher.Publisher
	Now            func() time.Time
	NewTicker      func(time.Duration) Ticker
	Update         func(Update)
	Events         EventRecorder
}

type Monitor struct {
	cfg               Config
	client            *http.Client
	mu                sync.Mutex
	cancel            context.CancelFunc
	done              chan struct{}
	episodeMu         sync.Mutex
	failureActive     bool
	targetUnavailable bool
}

func New(cfg Config) (*Monitor, error) {
	if cfg.Source == "" {
		cfg.Source = "redis"
	}
	if strings.ContainsAny(cfg.Source, "\x00\r\n \t") || !validMetricIdentifier(cfg.Source) {
		return nil, errors.New("invalid source")
	}
	if cfg.Interval <= 0 || cfg.Timeout <= 0 || cfg.Timeout >= cfg.Interval {
		return nil, errors.New("scrape timeout must be positive and less than interval")
	}
	if cfg.PublishTimeout <= 0 {
		return nil, errors.New("publisher timeout must be positive")
	}
	ip := net.ParseIP(cfg.Host)
	port, err := strconv.Atoi(cfg.Port)
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
		return nil, errors.New("exporter scrape address must be loopback")
	}
	if cfg.Publisher == nil {
		cfg.Publisher = publisher.Discard{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewTicker == nil {
		cfg.NewTicker = func(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }
	}
	if cfg.Update == nil {
		cfg.Update = func(Update) {}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = cfg.Timeout
	client := &http.Client{Timeout: cfg.Timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Monitor{cfg: cfg, client: client}, nil
}

func validMetricIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
		if i == 0 && r >= '0' && r <= '9' {
			return false
		}
	}
	return true
}

func (m *Monitor) Enable(manifest plugin.Manifest) {
	m.Disable(context.Background())
	m.episodeMu.Lock()
	m.failureActive, m.targetUnavailable = false, false
	m.episodeMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.mu.Lock()
	m.cancel, m.done = cancel, done
	m.mu.Unlock()
	go m.run(ctx, done, manifest)
}

func (m *Monitor) Disable(ctx context.Context) error {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		m.mu.Lock()
		if m.done == done {
			m.cancel, m.done = nil, nil
		}
		m.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Monitor) Shutdown(ctx context.Context) error { return m.Disable(ctx) }

func (m *Monitor) run(ctx context.Context, done chan struct{}, manifest plugin.Manifest) {
	defer close(done)
	m.scrape(ctx, manifest)
	ticker := m.cfg.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			m.scrape(ctx, manifest)
		}
	}
}

func (m *Monitor) scrape(parent context.Context, manifest plugin.Manifest) {
	ctx, cancel := context.WithTimeout(parent, m.cfg.Timeout)
	defer cancel()
	targetID := manifest.TargetID
	if targetID == "" {
		targetID = manifest.ID + "-local"
	}
	started := time.Now()
	status, samples, completedAt, err := m.fetch(ctx, manifest.MetricsPath)
	result := "scrape_success"
	if err != nil || status != "success" {
		result = "scrape_failure"
	}
	instrumentation.Active().Observe("scrapes_total", time.Since(started), "exporter_plugin", targetID, result)
	if result == "scrape_success" {
		instrumentation.Active().Set("last_scrape_success_timestamp_seconds", float64(completedAt.Unix()), "exporter_plugin", targetID)
	}
	if err != nil {
		if parent.Err() != nil {
			return
		}
		code := classify(err)
		m.cfg.Update(Update{ErrorCode: code, ErrorMessage: safeMessage(err)})
		m.recordCollectionFailure(code)
		return
	}
	if parent.Err() != nil {
		return
	}
	message, err := envelope.NewSource(m.cfg.Source, samples, completedAt)
	if err != nil {
		m.cfg.Update(Update{ErrorCode: "message_id_failed", ErrorMessage: "metrics message could not be created"})
		m.recordCollectionFailure("message_id_failed")
		return
	}
	message.Payload.ScrapeStatus = status
	message.Payload.TargetID = targetID
	publishCtx, publishCancel := context.WithTimeout(parent, m.cfg.PublishTimeout)
	started = time.Now()
	err = m.cfg.Publisher.Publish(publishCtx, message)
	result = "publish_success"
	if err != nil {
		result = "publish_failure"
	}
	instrumentation.Active().Observe("scrapes_total", time.Since(started), "exporter_plugin", targetID, result)
	publishCancel()
	if err != nil {
		if parent.Err() == nil {
			m.cfg.Update(Update{ErrorCode: "publish_failed", ErrorMessage: "metrics message could not be published"})
			m.recordCollectionFailure("publish_failed")
		}
		return
	}
	update := Update{ScrapeAt: &completedAt}
	if status == "success" {
		update.SuccessAt = &completedAt
	} else {
		update.ErrorCode = "network_failed"
		update.ErrorMessage = "metrics target is unavailable"
	}
	m.cfg.Update(update)
	m.recordPublished(status)
}

func (m *Monitor) recordCollectionFailure(code string) {
	m.episodeMu.Lock()
	if m.failureActive {
		m.episodeMu.Unlock()
		return
	}
	m.failureActive = true
	m.episodeMu.Unlock()
	if m.cfg.Events != nil {
		_ = m.recordMetricsEvent(events.NewMetrics("metrics_collection_failed", code, "", m.cfg.Now()))
	}
}

func (m *Monitor) recordPublished(status string) {
	m.episodeMu.Lock()
	recoveredCollection := m.failureActive
	m.failureActive = false
	becameUnavailable := status == "target_unavailable" && !m.targetUnavailable
	recoveredTarget := status == "success" && m.targetUnavailable
	if status == "target_unavailable" {
		m.targetUnavailable = true
	} else if status == "success" {
		m.targetUnavailable = false
	}
	m.episodeMu.Unlock()
	if m.cfg.Events == nil {
		return
	}
	if recoveredCollection {
		_ = m.recordMetricsEvent(events.NewMetrics("metrics_collection_recovered", "", "success", m.cfg.Now()))
	}
	if becameUnavailable {
		_ = m.recordMetricsEvent(events.NewMetrics("metrics_target_unavailable", "", "target_unavailable", m.cfg.Now()))
	} else if recoveredTarget {
		_ = m.recordMetricsEvent(events.NewMetrics("metrics_target_recovered", "", "success", m.cfg.Now()))
	}
}

func (m *Monitor) fetch(ctx context.Context, path string) (string, []envelope.Sample, time.Time, error) {
	if !validPath(path) {
		return "", nil, time.Time{}, errors.New("contract_invalid")
	}
	target := (&url.URL{Scheme: "http", Host: net.JoinHostPort(m.cfg.Host, m.cfg.Port), Path: path}).String()
	req, err := runtimekit.NewRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", nil, time.Time{}, errors.New("request_failed")
	}
	req.Header.Set("Accept", "text/plain; version=0.0.4")
	response, err := m.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", nil, time.Time{}, errors.New("scrape_timeout")
		}
		return "", nil, time.Time{}, errors.New("network_failed")
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Encoding") != "" {
		return "", nil, time.Time{}, errors.New("content_invalid")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/plain" {
		return "", nil, time.Time{}, errors.New("content_invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		return "", nil, time.Time{}, errors.New("read_failed")
	}
	if int64(len(body)) > MaxResponseBytes {
		return "", nil, time.Time{}, errors.New("response_too_large")
	}
	status, samples, err := parseSource(m.cfg.Source, response.StatusCode, body)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	return status, samples, m.cfg.Now().UTC(), nil
}

func validPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && !strings.ContainsAny(path, "?#\r\n\x00")
}

func parse(httpStatus int, body []byte) (string, []envelope.Sample, error) {
	return parseSource("redis", httpStatus, body)
}

func parseSource(source string, httpStatus int, body []byte) (string, []envelope.Sample, error) {
	if httpStatus != http.StatusOK && httpStatus != http.StatusServiceUnavailable {
		return "", nil, errors.New("http_invalid")
	}
	samples, err := parseExposition(source, body)
	if err != nil {
		return "", nil, err
	}
	upName := "gopulse_" + source + "_up"
	expectedUp, status := 1., "success"
	if httpStatus == http.StatusServiceUnavailable {
		expectedUp, status = 0, "target_unavailable"
		if len(samples) != 1 {
			return "", nil, errors.New("contract_invalid")
		}
	}
	found := false
	for _, s := range samples {
		if s.Name != upName {
			continue
		}
		if found || s.Kind != "gauge" || len(s.Labels) != 0 || s.Value != expectedUp {
			return "", nil, errors.New("contract_invalid")
		}
		found = true
	}
	if !found {
		return "", nil, errors.New("contract_invalid")
	}
	return status, samples, nil
}

func sampleKey(s envelope.Sample) string { return metriccontract.Key(s.Name, s.Labels) }
func classify(err error) string {
	code := err.Error()
	switch code {
	case "scrape_timeout", "network_failed", "response_too_large", "parse_failed", "contract_invalid", "content_invalid", "http_invalid":
		return code
	default:
		return "scrape_failed"
	}
}
func safeMessage(err error) string {
	switch classify(err) {
	case "scrape_timeout":
		return "metrics scrape timed out"
	case "network_failed":
		return "metrics target is unavailable"
	case "response_too_large":
		return "metrics response exceeded the size limit"
	case "parse_failed", "contract_invalid", "content_invalid", "http_invalid":
		return "metrics response was rejected"
	default:
		return "metrics scrape failed"
	}
}

// ValidateSuccessfulSnapshot is also the manager's pre-commit acceptance gate.
func ValidateSuccessfulSnapshot(status int, body []byte) error {
	result, _, err := parse(status, body)
	if err != nil {
		return err
	}
	if result != "success" {
		return errors.New("target unavailable")
	}
	return nil
}

func ValidateSuccessfulSourceSnapshot(source string, status int, body []byte) error {
	result, _, err := parseSource(source, status, body)
	if err != nil {
		return err
	}
	if result != "success" {
		return errors.New("target unavailable")
	}
	return nil
}

func (m *Monitor) recordMetricsEvent(event events.Event) bool {
	event.Metadata.PluginID = m.cfg.Source + "-exporter"
	return m.cfg.Events.Record(event)
}
