package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/redis/internal/collector"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

type Handler struct {
	collector collector.Collector
	timeout   time.Duration
	logger    *slog.Logger
}

func New(source collector.Collector, timeout time.Duration, logger *slog.Logger) http.Handler {
	return &Handler{collector: source, timeout: timeout, logger: logger}
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/health":
		h.health(response, request)
	case "/metrics":
		h.metrics(response, request)
	default:
		http.NotFound(response, request)
	}
}

func validateRequest(response http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return false
	}
	if request.URL.RawQuery != "" || request.ContentLength != 0 || (request.Body != nil && request.Body != http.NoBody) {
		http.Error(response, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return false
	}
	return true
}

func (h *Handler) health(response http.ResponseWriter, request *http.Request) {
	if !validateRequest(response, request) {
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
}

func (h *Handler) metrics(response http.ResponseWriter, request *http.Request) {
	if !validateRequest(response, request) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), h.timeout)
	defer cancel()
	snapshot, err := h.collector.Collect(ctx)
	status := http.StatusOK
	families := successFamilies(snapshot)
	if err != nil {
		status = http.StatusServiceUnavailable
		families = []*dto.MetricFamily{upFamily(0)}
		if h.logger != nil {
			h.logger.Warn("redis scrape failed", slog.String("reason", string(collector.Reason(err))))
		}
	}
	body, encodeErr := encode(families)
	if encodeErr != nil {
		status = http.StatusServiceUnavailable
		body, _ = encode([]*dto.MetricFamily{upFamily(0)})
		if h.logger != nil {
			h.logger.Error("metric encoding failed", slog.String("reason", "encoding_failed"))
		}
	}
	response.Header().Set("Content-Type", metricsContentType)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_, _ = response.Write(body)
}

func encode(families []*dto.MetricFamily) ([]byte, error) {
	var output bytes.Buffer
	for _, family := range families {
		if _, err := expfmt.MetricFamilyToText(&output, family); err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}

func successFamilies(snapshot collector.Snapshot) []*dto.MetricFamily {
	db := strconv.Itoa(snapshot.DB)
	return []*dto.MetricFamily{
		upFamily(1),
		gaugeFamily("gopulse_redis_uptime_seconds", "Current Redis process uptime in seconds.", float64(snapshot.UptimeSeconds), nil),
		gaugeFamily("gopulse_redis_connected_clients", "Current number of Redis client connections.", float64(snapshot.ConnectedClients), nil),
		gaugeFamily("gopulse_redis_used_memory_bytes", "Current number of bytes allocated by Redis.", float64(snapshot.UsedMemoryBytes), nil),
		counterFamily("gopulse_redis_commands_processed_total", "Total number of commands processed by Redis.", float64(snapshot.CommandsProcessedTotal), nil),
		counterFamily("gopulse_redis_keyspace_hits_total", "Total number of Redis keyspace hits.", float64(snapshot.KeyspaceHitsTotal), nil),
		counterFamily("gopulse_redis_keyspace_misses_total", "Total number of Redis keyspace misses.", float64(snapshot.KeyspaceMissesTotal), nil),
		counterFamily("gopulse_redis_cpu_seconds_total", "Total Redis CPU time in seconds by mode.", snapshot.CPUUserSeconds, map[string]string{"mode": "user"}, map[string]float64{"system": snapshot.CPUSystemSeconds}),
		gaugeFamily("gopulse_redis_db_keys", "Current number of keys in the configured Redis database.", float64(snapshot.DBKeys), map[string]string{"db": db}),
		gaugeFamily("gopulse_redis_db_expiring_keys", "Current number of expiring keys in the configured Redis database.", float64(snapshot.DBExpiringKeys), map[string]string{"db": db}),
	}
}

func upFamily(value float64) *dto.MetricFamily {
	return gaugeFamily("gopulse_redis_up", "Whether the current Redis scrape succeeded.", value, nil)
}

func gaugeFamily(name, help string, value float64, labels map[string]string) *dto.MetricFamily {
	gaugeType := dto.MetricType_GAUGE
	return &dto.MetricFamily{Name: stringPtr(name), Help: stringPtr(help), Type: &gaugeType, Metric: []*dto.Metric{{Label: labelPairs(labels), Gauge: &dto.Gauge{Value: floatPtr(value)}}}}
}
func counterFamily(name, help string, value float64, labels map[string]string, extra ...map[string]float64) *dto.MetricFamily {
	counterType := dto.MetricType_COUNTER
	metrics := []*dto.Metric{{Label: labelPairs(labels), Counter: &dto.Counter{Value: floatPtr(value)}}}
	if len(extra) == 1 {
		for label, extraValue := range extra[0] {
			metrics = append(metrics, &dto.Metric{Label: labelPairs(map[string]string{"mode": label}), Counter: &dto.Counter{Value: floatPtr(extraValue)}})
		}
	}
	return &dto.MetricFamily{Name: stringPtr(name), Help: stringPtr(help), Type: &counterType, Metric: metrics}
}
func labelPairs(labels map[string]string) []*dto.LabelPair {
	if len(labels) == 0 {
		return nil
	}
	pairs := make([]*dto.LabelPair, 0, len(labels))
	for name, value := range labels {
		n, v := name, value
		pairs = append(pairs, &dto.LabelPair{Name: &n, Value: &v})
	}
	return pairs
}
func stringPtr(value string) *string  { return &value }
func floatPtr(value float64) *float64 { return &value }
