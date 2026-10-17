package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sort"
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
	families := []*dto.MetricFamily{
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
	if snapshot.MaxMemoryBytes != nil {
		families = append(families, gaugeFamily("gopulse_redis_maxmemory_bytes", "Configured maximum Redis memory in bytes.", float64(*snapshot.MaxMemoryBytes), nil))
	}
	if snapshot.MaxClients != nil {
		families = append(families, gaugeFamily("gopulse_redis_max_clients", "Configured maximum Redis client connections.", float64(*snapshot.MaxClients), nil))
	}
	if snapshot.UsedMemoryRSSBytes != nil {
		families = append(families, gaugeFamily("gopulse_redis_used_memory_rss_bytes", "Resident memory used by Redis in bytes.", float64(*snapshot.UsedMemoryRSSBytes), nil))
	}
	if snapshot.MemoryFragmentation != nil {
		families = append(families, gaugeFamily("gopulse_redis_memory_fragmentation_ratio", "Redis memory fragmentation ratio.", *snapshot.MemoryFragmentation, nil))
	}
	if snapshot.BlockedClients != nil {
		families = append(families, gaugeFamily("gopulse_redis_blocked_clients", "Current number of clients blocked on Redis operations.", float64(*snapshot.BlockedClients), nil))
	}
	if snapshot.RejectedConnections != nil {
		families = append(families, counterFamily("gopulse_redis_rejected_connections_total", "Total Redis connections rejected by the server.", float64(*snapshot.RejectedConnections), nil))
	}
	if snapshot.EvictedKeysTotal != nil {
		families = append(families, counterFamily("gopulse_redis_evicted_keys_total", "Total Redis keys evicted because of the memory policy.", float64(*snapshot.EvictedKeysTotal), nil))
	}
	if snapshot.ExpiredKeysTotal != nil {
		families = append(families, counterFamily("gopulse_redis_expired_keys_total", "Total Redis keys removed because they expired.", float64(*snapshot.ExpiredKeysTotal), nil))
	}
	if snapshot.NetworkInputBytesTotal != nil {
		families = append(families, counterFamily("gopulse_redis_network_input_bytes_total", "Total bytes received by Redis from clients.", float64(*snapshot.NetworkInputBytesTotal), nil))
	}
	if snapshot.NetworkOutputBytesTotal != nil {
		families = append(families, counterFamily("gopulse_redis_network_output_bytes_total", "Total bytes sent by Redis to clients.", float64(*snapshot.NetworkOutputBytesTotal), nil))
	}
	commands := make([]string, 0, len(snapshot.CommandStats))
	for command := range snapshot.CommandStats {
		commands = append(commands, command)
	}
	sort.Strings(commands)
	commandFamilies := make(map[string]*dto.MetricFamily, 4)
	for _, command := range commands {
		stats := snapshot.CommandStats[command]
		labels := map[string]string{"command": command}
		for _, family := range []*dto.MetricFamily{
			counterFamily("gopulse_redis_command_calls_total", "Total calls for the tracked Redis command.", float64(stats.CallsTotal), labels),
			counterFamily("gopulse_redis_command_duration_seconds_total", "Total execution time for the tracked Redis command in seconds.", float64(stats.DurationUsecTotal)/1e6, labels),
			counterFamily("gopulse_redis_command_rejected_total", "Total tracked Redis command calls rejected by the server.", float64(stats.RejectedTotal), labels),
			counterFamily("gopulse_redis_command_failed_total", "Total tracked Redis command calls that failed.", float64(stats.FailedTotal), labels),
		} {
			// Emit one declaration per family, with a sample for each command.
			if existing := commandFamilies[family.GetName()]; existing != nil {
				existing.Metric = append(existing.Metric, family.Metric...)
			} else {
				commandFamilies[family.GetName()] = family
				families = append(families, family)
			}
		}
	}
	latencyCommands := make([]string, 0, len(snapshot.CommandLatencyStats))
	for command := range snapshot.CommandLatencyStats {
		if _, ok := snapshot.CommandStats[command]; ok {
			latencyCommands = append(latencyCommands, command)
		}
	}
	sort.Strings(latencyCommands)
	latencySamples := make([]gaugeSample, 0)
	latencyCounts := make([]counterSample, 0, len(latencyCommands))
	latencySums := make([]gaugeSample, 0, len(latencyCommands))
	for _, command := range latencyCommands {
		stats := snapshot.CommandStats[command]
		latencyCounts = append(latencyCounts, counterSample{Labels: map[string]string{"command": command}, Value: float64(stats.CallsTotal)})
		latencySums = append(latencySums, gaugeSample{Labels: map[string]string{"command": command}, Value: float64(stats.DurationUsecTotal) / 1e6})
		quantiles := make([]float64, 0, len(snapshot.CommandLatencyStats[command].PercentilesUsec))
		for quantile := range snapshot.CommandLatencyStats[command].PercentilesUsec {
			quantiles = append(quantiles, quantile)
		}
		sort.Float64s(quantiles)
		for _, quantile := range quantiles {
			latencySamples = append(latencySamples, gaugeSample{
				Labels: map[string]string{"command": command, "quantile": strconv.FormatFloat(quantile, 'g', -1, 64)},
				Value:  snapshot.CommandLatencyStats[command].PercentilesUsec[quantile] / 1e6,
			})
		}
	}
	if len(latencySamples) > 0 {
		families = append(families,
			gaugeSamplesFamily("gopulse_redis_command_latency_seconds", "Redis command latency percentiles in seconds for the configured latency-tracking window.", latencySamples),
			counterSamplesFamily("gopulse_redis_command_latency_seconds_count", "Number of calls represented by the Redis command latency percentiles.", latencyCounts),
			gaugeSamplesFamily("gopulse_redis_command_latency_seconds_sum", "Total command execution time represented by the Redis command latency percentiles in seconds.", latencySums),
		)
	}
	if snapshot.Replication.Role != "" {
		families = append(families, gaugeFamily("gopulse_redis_replication_role", "Current Redis replication role.", 1, map[string]string{"role": snapshot.Replication.Role}))
	}
	if snapshot.Replication.ConnectedReplicas != nil {
		families = append(families, gaugeFamily("gopulse_redis_replication_connected_replicas", "Current number of connected Redis replicas.", float64(*snapshot.Replication.ConnectedReplicas), nil))
	}
	if snapshot.Replication.MasterReplOffset != nil {
		families = append(families, gaugeFamily("gopulse_redis_replication_master_repl_offset", "Redis replication offset reported by the master.", float64(*snapshot.Replication.MasterReplOffset), nil))
	}
	if snapshot.Replication.MasterLastIOSecondsAgo != nil {
		families = append(families, gaugeFamily("gopulse_redis_replication_master_last_io_seconds_ago", "Seconds since the last replication I/O with the master.", float64(*snapshot.Replication.MasterLastIOSecondsAgo), nil))
	}
	if snapshot.Replication.MasterLinkUp != nil {
		families = append(families, gaugeFamily("gopulse_redis_replication_master_link_up", "Whether the Redis replica link to its master is up.", boolFloat(snapshot.Replication.MasterLinkUp), nil))
	}
	if snapshot.Replication.SyncInProgress != nil {
		families = append(families, gaugeFamily("gopulse_redis_replication_sync_in_progress", "Whether Redis replication synchronization is in progress.", boolFloat(snapshot.Replication.SyncInProgress), nil))
	}
	if len(snapshot.Replication.Replicas) > 0 {
		offsets := make([]gaugeSample, 0, len(snapshot.Replication.Replicas))
		lags := make([]gaugeSample, 0, len(snapshot.Replication.Replicas))
		links := make([]gaugeSample, 0, len(snapshot.Replication.Replicas))
		for _, replica := range snapshot.Replication.Replicas {
			labels := map[string]string{"replica": strconv.Itoa(replica.Index)}
			offsets = append(offsets, gaugeSample{Labels: labels, Value: float64(replica.Offset)})
			lags = append(lags, gaugeSample{Labels: labels, Value: float64(replica.LagSeconds)})
			links = append(links, gaugeSample{Labels: labels, Value: boolFloat(&replica.LinkUp)})
		}
		families = append(families,
			gaugeSamplesFamily("gopulse_redis_replica_offset", "Replication offset reported for each Redis replica.", offsets),
			gaugeSamplesFamily("gopulse_redis_replica_lag_seconds", "Replication lag reported for each Redis replica in seconds.", lags),
			gaugeSamplesFamily("gopulse_redis_replica_link_up", "Whether each Redis replica is online.", links),
		)
	}
	if snapshot.Persistence.RDBChangesSinceLastSave != nil {
		families = append(families, gaugeFamily("gopulse_redis_rdb_changes_since_last_save", "Redis changes since the last RDB save.", float64(*snapshot.Persistence.RDBChangesSinceLastSave), nil))
	}
	if snapshot.Persistence.RDBBGSAVEInProgress != nil {
		families = append(families, gaugeFamily("gopulse_redis_rdb_bgsave_in_progress", "Whether a Redis RDB background save is in progress.", boolFloat(snapshot.Persistence.RDBBGSAVEInProgress), nil))
	}
	if snapshot.Persistence.RDBLastSaveTimestamp != nil {
		families = append(families, gaugeFamily("gopulse_redis_rdb_last_save_timestamp_seconds", "Unix timestamp of the last Redis RDB save.", float64(*snapshot.Persistence.RDBLastSaveTimestamp), nil))
	}
	if snapshot.Persistence.RDBLastBGSAVESuccess != nil {
		families = append(families, gaugeFamily("gopulse_redis_rdb_last_bgsave_success", "Whether the last Redis RDB background save succeeded.", boolFloat(snapshot.Persistence.RDBLastBGSAVESuccess), nil))
	}
	if snapshot.Persistence.RDBCurrentBGSAVEDuration != nil {
		families = append(families, gaugeFamily("gopulse_redis_rdb_current_bgsave_duration_seconds", "Current Redis RDB background save duration in seconds.", float64(*snapshot.Persistence.RDBCurrentBGSAVEDuration), nil))
	}
	if snapshot.Persistence.AOFEnabled != nil {
		families = append(families, gaugeFamily("gopulse_redis_aof_enabled", "Whether Redis AOF persistence is enabled.", boolFloat(snapshot.Persistence.AOFEnabled), nil))
	}
	if snapshot.Persistence.AOFEnabled != nil && *snapshot.Persistence.AOFEnabled {
		if snapshot.Persistence.AOFRewriteInProgress != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_rewrite_in_progress", "Whether a Redis AOF rewrite is in progress.", boolFloat(snapshot.Persistence.AOFRewriteInProgress), nil))
		}
		if snapshot.Persistence.AOFRewriteScheduled != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_rewrite_scheduled", "Whether a Redis AOF rewrite is scheduled.", boolFloat(snapshot.Persistence.AOFRewriteScheduled), nil))
		}
		if snapshot.Persistence.AOFLastRewriteSuccess != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_last_rewrite_success", "Whether the last Redis AOF rewrite succeeded.", boolFloat(snapshot.Persistence.AOFLastRewriteSuccess), nil))
		}
		if snapshot.Persistence.AOFCurrentRewriteDuration != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_current_rewrite_duration_seconds", "Current Redis AOF rewrite duration in seconds.", float64(*snapshot.Persistence.AOFCurrentRewriteDuration), nil))
		}
		if snapshot.Persistence.AOFLastWriteSuccess != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_last_write_success", "Whether the last Redis AOF write succeeded.", boolFloat(snapshot.Persistence.AOFLastWriteSuccess), nil))
		}
		if snapshot.Persistence.AOFRewritesTotal != nil {
			families = append(families, counterFamily("gopulse_redis_aof_rewrites_total", "Total Redis AOF rewrite operations.", float64(*snapshot.Persistence.AOFRewritesTotal), nil))
		}
		if snapshot.Persistence.AOFRewriteFailuresTotal != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_rewrite_consecutive_failures", "Consecutive Redis AOF rewrite failures.", float64(*snapshot.Persistence.AOFRewriteFailuresTotal), nil))
		}
		if snapshot.Persistence.AOFBaseSizeBytes != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_base_size_bytes", "Redis AOF base size in bytes.", float64(*snapshot.Persistence.AOFBaseSizeBytes), nil))
		}
		if snapshot.Persistence.AOFCurrentSizeBytes != nil {
			families = append(families, gaugeFamily("gopulse_redis_aof_current_size_bytes", "Current Redis AOF size in bytes.", float64(*snapshot.Persistence.AOFCurrentSizeBytes), nil))
		}
	}
	return families
}

func upFamily(value float64) *dto.MetricFamily {
	return gaugeFamily("gopulse_redis_up", "Whether the current Redis scrape succeeded.", value, nil)
}

func gaugeFamily(name, help string, value float64, labels map[string]string) *dto.MetricFamily {
	gaugeType := dto.MetricType_GAUGE
	return &dto.MetricFamily{Name: stringPtr(name), Help: stringPtr(help), Type: &gaugeType, Metric: []*dto.Metric{{Label: labelPairs(labels), Gauge: &dto.Gauge{Value: floatPtr(value)}}}}
}

type gaugeSample struct {
	Labels map[string]string
	Value  float64
}

type counterSample struct {
	Labels map[string]string
	Value  float64
}

func gaugeSamplesFamily(name, help string, samples []gaugeSample) *dto.MetricFamily {
	gaugeType := dto.MetricType_GAUGE
	metrics := make([]*dto.Metric, 0, len(samples))
	for _, sample := range samples {
		metrics = append(metrics, &dto.Metric{Label: labelPairs(sample.Labels), Gauge: &dto.Gauge{Value: floatPtr(sample.Value)}})
	}
	return &dto.MetricFamily{Name: stringPtr(name), Help: stringPtr(help), Type: &gaugeType, Metric: metrics}
}
func counterSamplesFamily(name, help string, samples []counterSample) *dto.MetricFamily {
	counterType := dto.MetricType_COUNTER
	metrics := make([]*dto.Metric, 0, len(samples))
	for _, sample := range samples {
		metrics = append(metrics, &dto.Metric{Label: labelPairs(sample.Labels), Counter: &dto.Counter{Value: floatPtr(sample.Value)}})
	}
	return &dto.MetricFamily{Name: stringPtr(name), Help: stringPtr(help), Type: &counterType, Metric: metrics}
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

func boolFloat(value *bool) float64 {
	if value != nil && *value {
		return 1
	}
	return 0
}
