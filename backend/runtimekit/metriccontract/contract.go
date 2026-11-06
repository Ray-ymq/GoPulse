// Package metriccontract validates metric transport without a metric catalog.
package metriccontract

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxBodyBytes       = 1 << 20
	MaxFamilies        = 512
	MaxSamples         = 8192
	MaxLabels          = 16
	MaxNameBytes       = 128
	MaxLabelNameBytes  = 64
	MaxLabelValueBytes = 256
	MetadataName       = "gopulse_metric_metadata"
)

var namePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type Sample struct {
	Name   string
	Kind   string
	Labels map[string]string
	Value  float64
}

func ValidName(source, name string) bool {
	if source == "node" && strings.HasPrefix(name, "node_") {
		return len(name) > len("node_") && len(name) <= MaxNameBytes && namePattern.MatchString(name)
	}
	prefix := "gopulse_" + strings.ReplaceAll(source, "-", "_") + "_"
	return len(name) <= MaxNameBytes && namePattern.MatchString(name) && strings.HasPrefix(name, prefix) && len(name) > len(prefix)
}

func ValidKind(kind string) bool { return kind == "gauge" || kind == "counter" }

func ValidLabels(labels map[string]string) bool {
	if labels == nil || len(labels) > MaxLabels {
		return false
	}
	for key, value := range labels {
		if len(key) > MaxLabelNameBytes || !namePattern.MatchString(key) || strings.HasPrefix(key, "__") || !utf8.ValidString(value) || len(value) > MaxLabelValueBytes || strings.ContainsRune(value, 0) {
			return false
		}
		switch key {
		case "source", "target_id", "producer_kind", "producer_id", "producer_version":
			return false
		}
	}
	return true
}

func ValidateSample(source string, s Sample) error {
	if !ValidName(source, s.Name) || !ValidKind(s.Kind) || !ValidLabels(s.Labels) {
		return errors.New("invalid metric sample")
	}
	if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) || (s.Kind == "counter" && s.Value < 0) {
		return errors.New("invalid metric value")
	}
	if text, ok := s.Labels["quantile"]; ok {
		quantile, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(quantile) || quantile < 0 || quantile > 1 {
			return errors.New("invalid summary quantile")
		}
	}
	if strings.HasSuffix(s.Name, "_bucket") {
		bound, err := strconv.ParseFloat(s.Labels["le"], 64)
		if err != nil || math.IsNaN(bound) || math.IsInf(bound, -1) || s.Kind != "counter" || s.Value != math.Trunc(s.Value) {
			return errors.New("invalid histogram bucket")
		}
	}
	return nil
}

func Key(name string, labels map[string]string) string {
	b, _ := json.Marshal(labels)
	return name + string(b)
}

func Validate(source string, samples []Sample) error {
	if len(samples) == 0 || len(samples) > MaxSamples {
		return errors.New("invalid metric sample budget")
	}
	seen := make(map[string]Sample, len(samples))
	kinds := map[string]string{}
	labelKeys := map[string]map[string]bool{}
	for _, s := range samples {
		if err := ValidateSample(source, s); err != nil {
			return err
		}
		key := Key(s.Name, s.Labels)
		if _, exists := seen[key]; exists {
			return errors.New("duplicate metric series")
		}
		if kind, exists := kinds[s.Name]; exists && kind != s.Kind {
			return errors.New("conflicting metric types")
		}
		kinds[s.Name] = s.Kind
		if labelKeys[s.Name] == nil {
			labelKeys[s.Name] = map[string]bool{}
		}
		for key := range s.Labels {
			labelKeys[s.Name][key] = true
		}
		if len(labelKeys[s.Name]) > MaxLabels || len(kinds) > MaxFamilies {
			return errors.New("invalid metric family budget")
		}
		seen[key] = s
	}
	if err := validateSummaries(samples, seen); err != nil {
		return err
	}
	return validateBuckets(samples, seen)
}

func validateSummaries(samples []Sample, seen map[string]Sample) error {
	quantiles := map[string]map[float64]bool{}
	for _, s := range samples {
		text, exists := s.Labels["quantile"]
		if !exists {
			continue
		}
		if s.Kind != "gauge" {
			return errors.New("invalid summary type")
		}
		labels := make(map[string]string, len(s.Labels)-1)
		for key, value := range s.Labels {
			if key != "quantile" {
				labels[key] = value
			}
		}
		count, ok := seen[Key(s.Name+"_count", labels)]
		if !ok || count.Kind != "counter" || count.Value != math.Trunc(count.Value) {
			return errors.New("missing summary count")
		}
		if _, ok := seen[Key(s.Name+"_sum", labels)]; !ok {
			return errors.New("missing summary sum")
		}
		key := Key(s.Name, labels)
		if quantiles[key] == nil {
			quantiles[key] = map[float64]bool{}
		}
		quantile, _ := strconv.ParseFloat(text, 64)
		if quantiles[key][quantile] {
			return errors.New("duplicate summary quantile")
		}
		quantiles[key][quantile] = true
	}
	return nil
}

func validateBuckets(samples []Sample, seen map[string]Sample) error {
	type bucket struct{ bound, count float64 }
	type group struct {
		name    string
		labels  map[string]string
		buckets []bucket
	}
	groups := map[string]*group{}
	for _, s := range samples {
		le, exists := s.Labels["le"]
		if !strings.HasSuffix(s.Name, "_bucket") || !exists {
			continue
		}
		bound, err := strconv.ParseFloat(le, 64)
		if err != nil || math.IsNaN(bound) || math.IsInf(bound, -1) || s.Kind != "counter" || s.Value != math.Trunc(s.Value) {
			return errors.New("invalid histogram bucket")
		}
		labels := make(map[string]string, len(s.Labels)-1)
		for k, v := range s.Labels {
			if k != "le" {
				labels[k] = v
			}
		}
		name := strings.TrimSuffix(s.Name, "_bucket")
		key := Key(name, labels)
		g := groups[key]
		if g == nil {
			g = &group{name: name, labels: labels}
			groups[key] = g
		}
		g.buckets = append(g.buckets, bucket{bound, s.Value})
	}
	for _, g := range groups {
		count, ok := seen[Key(g.name+"_count", g.labels)]
		if !ok || count.Kind != "counter" || count.Value != math.Trunc(count.Value) {
			return errors.New("missing histogram count")
		}
		if _, ok := seen[Key(g.name+"_sum", g.labels)]; !ok {
			return errors.New("missing histogram sum")
		}
		sort.Slice(g.buckets, func(i, j int) bool { return g.buckets[i].bound < g.buckets[j].bound })
		for i, b := range g.buckets {
			if i > 0 && (b.bound <= g.buckets[i-1].bound || b.count < g.buckets[i-1].count) {
				return errors.New("invalid cumulative histogram")
			}
		}
		last := g.buckets[len(g.buckets)-1]
		if !math.IsInf(last.bound, 1) || last.count != count.Value {
			return errors.New("histogram count mismatch")
		}
	}
	return nil
}

var unitOverrides = map[string]string{
	"node_load1":                                            "count",
	"node_load5":                                            "count",
	"node_load15":                                           "count",
	"node_procs_running":                                    "count",
	"node_procs_blocked":                                    "count",
	"node_boot_time_seconds":                                "unix_seconds",
	"node_time_seconds":                                     "unix_seconds",
	"node_scrape_collector_success":                         "boolean",
	"gopulse_redis_blocked_clients":                         "count",
	"gopulse_redis_command_latency_seconds_count":           "count",
	"gopulse_redis_memory_fragmentation_ratio":              "none",
	"gopulse_redis_replication_role":                        "state",
	"gopulse_redis_replication_connected_replicas":          "count",
	"gopulse_redis_replication_master_repl_offset":          "bytes",
	"gopulse_redis_replication_master_last_io_seconds_ago":  "seconds",
	"gopulse_redis_replication_sync_in_progress":            "boolean",
	"gopulse_redis_replica_offset":                          "bytes",
	"gopulse_redis_rdb_changes_since_last_save":             "count",
	"gopulse_redis_rdb_bgsave_in_progress":                  "boolean",
	"gopulse_redis_rdb_last_bgsave_success":                 "boolean",
	"gopulse_redis_aof_enabled":                             "boolean",
	"gopulse_redis_aof_rewrite_in_progress":                 "boolean",
	"gopulse_redis_aof_rewrite_scheduled":                   "boolean",
	"gopulse_redis_aof_last_rewrite_success":                "boolean",
	"gopulse_redis_aof_last_write_success":                  "boolean",
	"gopulse_redis_aof_rewrite_consecutive_failures":        "count",
	"gopulse_mysql_connections_peak":                        "count",
	"gopulse_mysql_buffer_pool_pages":                       "count",
	"gopulse_mysql_row_lock_current_waits":                  "count",
	"gopulse_mysql_data_pending":                            "count",
	"gopulse_mysql_log_pending":                             "count",
	"gopulse_mysql_undo_history_length":                     "count",
	"gopulse_mysql_undo_slots_used":                         "count",
	"gopulse_mysql_active_transactions":                     "count",
	"gopulse_mysql_transaction_rows_locked":                 "count",
	"gopulse_mysql_transaction_rows_modified":               "count",
	"gopulse_mysql_redo_lsn_current":                        "bytes",
	"gopulse_mysql_redo_lsn_last_checkpoint":                "bytes",
	"gopulse_mysql_redo_checkpoint_age_bytes":               "bytes",
	"gopulse_mysql_redo_log_capacity_bytes":                 "bytes",
	"gopulse_rabbitmq_queue_messages":                       "count",
	"gopulse_rabbitmq_queue_consumers":                      "count",
	"gopulse_rabbitmq_queue_consumer_capacity":              "none",
	"gopulse_rabbitmq_queue_memory_bytes":                   "bytes",
	"gopulse_rabbitmq_queue_message_bytes":                  "bytes",
	"gopulse_rabbitmq_node_file_descriptors_used":           "count",
	"gopulse_rabbitmq_node_file_descriptors_limit":          "count",
	"gopulse_rabbitmq_node_processes_used":                  "count",
	"gopulse_rabbitmq_node_processes_limit":                 "count",
	"gopulse_rabbitmq_connections_blocked":                  "count",
	"gopulse_rabbitmq_connection_received_bytes_total":      "bytes",
	"gopulse_rabbitmq_connection_sent_bytes_total":          "bytes",
	"gopulse_rabbitmq_node_io_operations_total":             "count",
	"gopulse_rabbitmq_node_io_average_duration_seconds":     "seconds",
	"gopulse_rabbitmq_node_memory_alarm":                    "boolean",
	"gopulse_rabbitmq_node_disk_free_alarm":                 "boolean",
	"gopulse_kafka_partition_log_end_offset":                "count",
	"gopulse_kafka_consumer_committed_offset":               "count",
	"gopulse_kafka_consumer_partition_lag":                  "count",
	"gopulse_kafka_partition_replicas":                      "count",
	"gopulse_kafka_partition_isr":                           "count",
	"gopulse_kafka_partition_under_replicated":              "boolean",
	"gopulse_kafka_partition_offline":                       "boolean",
	"gopulse_kafka_partition_leader":                        "boolean",
	"gopulse_kafka_jmx_up":                                  "boolean",
	"gopulse_kafka_broker_bytes_in_total":                   "bytes",
	"gopulse_kafka_broker_bytes_out_total":                  "bytes",
	"gopulse_kafka_broker_messages_in_total":                "count",
	"gopulse_kafka_request_total":                           "count",
	"gopulse_kafka_request_errors_total":                    "count",
	"gopulse_kafka_request_duration_seconds_total":          "seconds",
	"gopulse_kafka_request_queue_duration_seconds_total":    "seconds",
	"gopulse_kafka_request_local_duration_seconds_total":    "seconds",
	"gopulse_kafka_request_remote_duration_seconds_total":   "seconds",
	"gopulse_kafka_request_queue_size":                      "count",
	"gopulse_kafka_network_processor_idle_ratio":            "none",
	"gopulse_kafka_request_handler_idle_ratio":              "none",
	"gopulse_kafka_broker_under_replicated_partitions":      "count",
	"gopulse_kafka_isr_shrinks_total":                       "count",
	"gopulse_kafka_isr_expands_total":                       "count",
	"gopulse_kafka_leader_elections_total":                  "count",
	"gopulse_kafka_unclean_leader_elections_total":          "count",
	"gopulse_kafka_jvm_heap_used_bytes":                     "bytes",
	"gopulse_kafka_jvm_heap_max_bytes":                      "bytes",
	"gopulse_kafka_jvm_threads":                             "count",
	"gopulse_kafka_jvm_gc_collections_total":                "count",
	"gopulse_kafka_jvm_gc_collection_time_seconds_total":    "seconds",
	"gopulse_elasticsearch_thread_pool_active":              "count",
	"gopulse_elasticsearch_thread_pool_queue":               "count",
	"gopulse_elasticsearch_search_in_flight":                "count",
	"gopulse_elasticsearch_indexing_in_flight":              "count",
	"gopulse_elasticsearch_cache_memory_bytes":              "bytes",
	"gopulse_elasticsearch_cache_hits_total":                "count",
	"gopulse_elasticsearch_cache_misses_total":              "count",
	"gopulse_elasticsearch_cache_evictions_total":           "count",
	"gopulse_elasticsearch_merge_operations_total":          "count",
	"gopulse_elasticsearch_merge_duration_seconds_total":    "seconds",
	"gopulse_elasticsearch_merge_in_flight":                 "count",
	"gopulse_elasticsearch_merge_current_size_bytes":        "bytes",
	"gopulse_elasticsearch_refresh_operations_total":        "count",
	"gopulse_elasticsearch_refresh_duration_seconds_total":  "seconds",
	"gopulse_elasticsearch_refresh_listeners":               "count",
	"gopulse_elasticsearch_flush_operations_total":          "count",
	"gopulse_elasticsearch_flush_duration_seconds_total":    "seconds",
	"gopulse_elasticsearch_circuit_breaker_tripped_total":   "count",
	"gopulse_elasticsearch_circuit_breaker_estimated_bytes": "bytes",
	"gopulse_elasticsearch_circuit_breaker_limit_bytes":     "bytes",
	"gopulse_elasticsearch_fielddata_memory_bytes":          "bytes",
	"gopulse_elasticsearch_segments_count":                  "count",
	"gopulse_elasticsearch_segments_memory_bytes":           "bytes",
	"gopulse_victoriametrics_cache_entries":                 "count",
	"gopulse_victoriametrics_write_batches_total":           "count",
	"gopulse_victoriametrics_write_batch_rows_total":        "count",
	"gopulse_victoriametrics_write_requests_total":          "count",
	"gopulse_victoriametrics_write_errors_total":            "count",
	"gopulse_victoriametrics_query_errors_total":            "count",
	"gopulse_victoriametrics_query_duration_seconds_total":  "seconds",
	"gopulse_victoriametrics_active_queries":                "count",
	"gopulse_victoriametrics_query_capacity":                "count",
	"gopulse_victoriametrics_query_limit_reached_total":     "count",
	"gopulse_victoriametrics_query_limit_timeout_total":     "count",
	"gopulse_victoriametrics_process_cpu_seconds_total":     "seconds",
	"gopulse_victoriametrics_process_resident_memory_bytes": "bytes",
	"gopulse_victoriametrics_go_heap_alloc_bytes":           "bytes",
	"gopulse_victoriametrics_go_heap_sys_bytes":             "bytes",
	"gopulse_victoriametrics_go_gc_cycles_total":            "count",
	"gopulse_victoriametrics_go_gc_pause_seconds_total":     "seconds",
	"gopulse_victoriametrics_go_goroutines":                 "count",
	"gopulse_victoriametrics_go_threads":                    "count",
}

// Unit uses explicit overrides and metric suffixes. A gauge without either
// form has an unknown unit.
func Unit(name, kind string) string {
	if unit, ok := unitOverrides[name]; ok {
		return unit
	}
	base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, "_total"), "_sum"), "_bucket"), "_count")
	switch {
	case strings.HasSuffix(base, "_timestamp_seconds"):
		return "unix_seconds"
	case strings.HasSuffix(base, "_seconds"):
		return "seconds"
	case strings.HasSuffix(base, "_bytes"):
		return "bytes"
	case strings.HasSuffix(base, "_up"):
		return "boolean"
	case kind == "counter":
		return "count"
	default:
		return "none"
	}
}
