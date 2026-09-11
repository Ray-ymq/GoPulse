package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/events"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/envelope"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const MaxResponseBytes int64 = 1 << 20

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
	if cfg.Source != "redis" && cfg.Source != "mysql" && cfg.Source != "rabbitmq" && cfg.Source != "kafka" && cfg.Source != "elasticsearch" && cfg.Source != "victoriametrics" {
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
	status, samples, completedAt, err := m.fetch(ctx, manifest.MetricsPath)
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
	newEnvelope := envelope.New
	if manifest.MetricsContractVersion == 2 {
		newEnvelope = envelope.NewV2
	}
	message, err := newEnvelope(manifest.ID, manifest.Version, status, samples, completedAt)
	if err != nil {
		m.cfg.Update(Update{ErrorCode: "message_id_failed", ErrorMessage: "metrics message could not be created"})
		m.recordCollectionFailure("message_id_failed")
		return
	}
	publishCtx, publishCancel := context.WithTimeout(parent, m.cfg.PublishTimeout)
	err = m.cfg.Publisher.Publish(publishCtx, message)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
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

type familyContract struct {
	kind   dto.MetricType
	labels map[string]bool
	count  int
}

var contracts = map[string]familyContract{
	"gopulse_redis_up":                       {dto.MetricType_GAUGE, nil, 1},
	"gopulse_redis_uptime_seconds":           {dto.MetricType_GAUGE, nil, 1},
	"gopulse_redis_connected_clients":        {dto.MetricType_GAUGE, nil, 1},
	"gopulse_redis_used_memory_bytes":        {dto.MetricType_GAUGE, nil, 1},
	"gopulse_redis_commands_processed_total": {dto.MetricType_COUNTER, nil, 1},
	"gopulse_redis_keyspace_hits_total":      {dto.MetricType_COUNTER, nil, 1},
	"gopulse_redis_keyspace_misses_total":    {dto.MetricType_COUNTER, nil, 1},
	"gopulse_redis_cpu_seconds_total":        {dto.MetricType_COUNTER, map[string]bool{"mode": true}, 2},
	"gopulse_redis_db_keys":                  {dto.MetricType_GAUGE, map[string]bool{"db": true}, 1},
	"gopulse_redis_db_expiring_keys":         {dto.MetricType_GAUGE, map[string]bool{"db": true}, 1},
}

func contractsFor(source string) map[string]familyContract {
	switch source {
	case "redis":
		return contracts
	case "mysql":
		return map[string]familyContract{
			"gopulse_mysql_up":                      {dto.MetricType_GAUGE, nil, 1},
			"gopulse_mysql_uptime_seconds":          {dto.MetricType_GAUGE, nil, 1},
			"gopulse_mysql_connections":             {dto.MetricType_GAUGE, nil, 1},
			"gopulse_mysql_max_connections":         {dto.MetricType_GAUGE, nil, 1},
			"gopulse_mysql_threads_running":         {dto.MetricType_GAUGE, nil, 1},
			"gopulse_mysql_queries_total":           {dto.MetricType_COUNTER, nil, 1},
			"gopulse_mysql_slow_queries_total":      {dto.MetricType_COUNTER, nil, 1},
			"gopulse_mysql_transactions_total":      {dto.MetricType_COUNTER, map[string]bool{"result": true}, 2},
			"gopulse_mysql_buffer_pool_data_bytes":  {dto.MetricType_GAUGE, nil, 1},
			"gopulse_mysql_buffer_pool_dirty_bytes": {dto.MetricType_GAUGE, nil, 1},
		}
	case "rabbitmq":
		return map[string]familyContract{
			"gopulse_rabbitmq_up":              {dto.MetricType_GAUGE, nil, 1},
			"gopulse_rabbitmq_connections":     {dto.MetricType_GAUGE, nil, 1},
			"gopulse_rabbitmq_channels":        {dto.MetricType_GAUGE, nil, 1},
			"gopulse_rabbitmq_queues":          {dto.MetricType_GAUGE, nil, 1},
			"gopulse_rabbitmq_consumers":       {dto.MetricType_GAUGE, nil, 1},
			"gopulse_rabbitmq_messages":        {dto.MetricType_GAUGE, map[string]bool{"state": true}, 2},
			"gopulse_rabbitmq_published_total": {dto.MetricType_COUNTER, nil, 1},
			"gopulse_rabbitmq_delivered_total": {dto.MetricType_COUNTER, nil, 1},
			"gopulse_rabbitmq_acked_total":     {dto.MetricType_COUNTER, nil, 1},
		}
	case "kafka":
		return map[string]familyContract{
			"gopulse_kafka_up":                          {dto.MetricType_GAUGE, nil, 1},
			"gopulse_kafka_brokers":                     {dto.MetricType_GAUGE, nil, 1},
			"gopulse_kafka_controller_available":        {dto.MetricType_GAUGE, nil, 1},
			"gopulse_kafka_partitions":                  {dto.MetricType_GAUGE, nil, 1},
			"gopulse_kafka_under_replicated_partitions": {dto.MetricType_GAUGE, nil, 1},
			"gopulse_kafka_offline_partitions":          {dto.MetricType_GAUGE, nil, 1},
			"gopulse_kafka_consumer_group_lag":          {dto.MetricType_GAUGE, nil, 1},
		}

	case "victoriametrics":
		return map[string]familyContract{
			"gopulse_victoriametrics_up":                         {dto.MetricType_GAUGE, nil, 1},
			"gopulse_victoriametrics_rows_inserted_total":        {dto.MetricType_COUNTER, nil, 1},
			"gopulse_victoriametrics_query_requests_total":       {dto.MetricType_COUNTER, nil, 1},
			"gopulse_victoriametrics_active_timeseries":          {dto.MetricType_GAUGE, nil, 1},
			"gopulse_victoriametrics_storage_rows":               {dto.MetricType_GAUGE, nil, 1},
			"gopulse_victoriametrics_storage_size_bytes":         {dto.MetricType_GAUGE, nil, 1},
			"gopulse_victoriametrics_free_disk_space_bytes":      {dto.MetricType_GAUGE, nil, 1},
			"gopulse_victoriametrics_active_merges":              {dto.MetricType_GAUGE, nil, 1},
			"gopulse_victoriametrics_storage_rows_deleted_total": {dto.MetricType_COUNTER, nil, 1},
		}
	case "elasticsearch":
		return map[string]familyContract{
			"gopulse_elasticsearch_up":                    {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_cluster_health_status": {dto.MetricType_GAUGE, map[string]bool{"status": true}, 3},
			"gopulse_elasticsearch_nodes":                 {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_data_nodes":            {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_active_primary_shards": {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_active_shards":         {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_relocating_shards":     {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_initializing_shards":   {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_unassigned_shards":     {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_pending_tasks":         {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_documents":             {dto.MetricType_GAUGE, nil, 1},
			"gopulse_elasticsearch_store_size_bytes":      {dto.MetricType_GAUGE, nil, 1},
		}

	}
	return nil
}
func parse(httpStatus int, body []byte) (string, []envelope.Sample, error) {
	return parseSource("redis", httpStatus, body)
}
func parseSource(source string, httpStatus int, body []byte) (string, []envelope.Sample, error) {
	contracts := contractsFor(source)
	if contracts == nil {
		return "", nil, errors.New("contract_invalid")
	}
	upName := "gopulse_" + source + "_up"
	if bytes.Contains(body, []byte("\x00")) {
		return "", nil, errors.New("parse_failed")
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return "", nil, errors.New("parse_failed")
	}
	if len(families) > 128 {
		return "", nil, errors.New("contract_invalid")
	}
	if httpStatus == http.StatusServiceUnavailable {
		if len(families) != 1 {
			return "", nil, errors.New("contract_invalid")
		}
		family := families[upName]
		samples, err := validateFamilySource(source, upName, family)
		if err != nil || len(samples) != 1 || samples[0].Value != 0 {
			return "", nil, errors.New("contract_invalid")
		}
		return "target_unavailable", samples, nil
	}
	if httpStatus != http.StatusOK {
		return "", nil, errors.New("http_invalid")
	}
	if len(families) != len(contracts) {
		return "", nil, errors.New("contract_invalid")
	}
	all := make([]envelope.Sample, 0, 16)
	seen := map[string]bool{}
	for name, contract := range contracts {
		family := families[name]
		samples, err := validateFamilySource(source, name, family)
		if err != nil || len(samples) != contract.count {
			return "", nil, errors.New("contract_invalid")
		}
		if len(all)+len(samples) > 1024 {
			return "", nil, errors.New("contract_invalid")
		}
		for _, sample := range samples {
			key := sampleKey(sample)
			if seen[key] {
				return "", nil, errors.New("contract_invalid")
			}
			seen[key] = true
			all = append(all, sample)
		}
	}
	for name := range families {
		if _, ok := contracts[name]; !ok {
			return "", nil, errors.New("contract_invalid")
		}
	}
	var up bool
	healthSum := 0.0
	modes := map[string]bool{}
	dbValues := map[string]bool{}
	for _, s := range all {
		if s.Name == "gopulse_elasticsearch_cluster_health_status" {
			if s.Value != 0 && s.Value != 1 {
				return "", nil, errors.New("contract_invalid")
			}
			healthSum += s.Value
		}
		if s.Name == upName {
			up = s.Value == 1
		}
		if s.Name == "gopulse_redis_cpu_seconds_total" {
			modes[s.Labels["mode"]] = true
		}
		if s.Name == "gopulse_redis_db_keys" || s.Name == "gopulse_redis_db_expiring_keys" {
			dbValues[s.Labels["db"]] = true
		}
	}
	if (source == "elasticsearch" && healthSum != 1) || !up || (source == "redis" && (!modes["user"] || !modes["system"] || len(modes) != 2 || len(dbValues) != 1)) {
		return "", nil, errors.New("contract_invalid")
	}
	sort.Slice(all, func(i, j int) bool { return sampleKey(all[i]) < sampleKey(all[j]) })
	return "success", all, nil
}

func validateFamily(name string, family *dto.MetricFamily) ([]envelope.Sample, error) {
	return validateFamilySource("redis", name, family)
}
func validateFamilySource(source, name string, family *dto.MetricFamily) ([]envelope.Sample, error) {
	contract, ok := contractsFor(source)[name]
	if !ok || family == nil || family.GetName() != name || family.Type == nil || family.GetType() != contract.kind {
		return nil, errors.New("contract_invalid")
	}
	if len(name) > 128 || len(family.Metric) > 1024 {
		return nil, errors.New("contract_invalid")
	}
	out := make([]envelope.Sample, 0, len(family.Metric))
	for _, metric := range family.Metric {
		if metric.TimestampMs != nil || len(metric.Label) > 16 {
			return nil, errors.New("contract_invalid")
		}
		labels := map[string]string{}
		for _, pair := range metric.Label {
			key, value := pair.GetName(), pair.GetValue()
			if len(key) > 128 || len(value) > 256 || contract.labels == nil || !contract.labels[key] || labels[key] != "" {
				return nil, errors.New("contract_invalid")
			}
			if key == "mode" && value != "user" && value != "system" {
				return nil, errors.New("contract_invalid")
			}
			if key == "status" && value != "green" && value != "yellow" && value != "red" {
				return nil, errors.New("contract_invalid")
			}
			if (key == "result" && value != "commit" && value != "rollback") || (key == "state" && value != "ready" && value != "unacked") {
				return nil, errors.New("contract_invalid")
			}
			if key == "db" {
				if _, err := strconv.ParseUint(value, 10, 31); err != nil {
					return nil, errors.New("contract_invalid")
				}
			}
			labels[key] = value
		}
		if len(labels) != len(contract.labels) {
			return nil, errors.New("contract_invalid")
		}
		var value float64
		if contract.kind == dto.MetricType_GAUGE && metric.Gauge != nil {
			value = metric.Gauge.GetValue()
		} else if contract.kind == dto.MetricType_COUNTER && metric.Counter != nil {
			value = metric.Counter.GetValue()
		} else {
			return nil, errors.New("contract_invalid")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || (contract.kind == dto.MetricType_COUNTER && value < 0) {
			return nil, errors.New("contract_invalid")
		}
		out = append(out, envelope.Sample{Name: name, Kind: strings.ToLower(contract.kind.String()), Labels: labels, Value: value})
	}
	return out, nil
}

func sampleKey(s envelope.Sample) string {
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(s.Name)
	for _, k := range keys {
		fmt.Fprintf(&b, "\x00%s=%s", k, s.Labels[k])
	}
	return b.String()
}
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
