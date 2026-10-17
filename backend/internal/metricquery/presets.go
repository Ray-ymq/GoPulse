package metricquery

import "github.com/Ray-ymq/GoPulse/componentmetrics"

// Presets supply display hints for historical metrics and supported alert rules.
// Collection and metric queries do not require a preset.
var Presets = func() []Definition {
	items := []Definition{
		{Metric: "gopulse_redis_up", Kind: "gauge", Unit: "boolean"},
		{Metric: "gopulse_redis_uptime_seconds", Kind: "gauge", Unit: "seconds"},
		{Metric: "gopulse_redis_connected_clients", Kind: "gauge", Unit: "count"},
		{Metric: "gopulse_redis_used_memory_bytes", Kind: "gauge", Unit: "bytes"},
		{Metric: "gopulse_redis_commands_processed_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_redis_keyspace_hits_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_redis_keyspace_misses_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_redis_cpu_seconds_total", Kind: "counter", Unit: "seconds", label: "mode"},
		{Metric: "gopulse_redis_db_keys", Kind: "gauge", Unit: "count", label: "db"},
		{Metric: "gopulse_redis_db_expiring_keys", Kind: "gauge", Unit: "count", label: "db"},
		{Metric: "gopulse_redis_max_clients", Kind: "gauge", Unit: "count"},
		{Metric: "gopulse_redis_maxmemory_bytes", Kind: "gauge", Unit: "bytes"},
		{Metric: "gopulse_redis_used_memory_rss_bytes", Kind: "gauge", Unit: "bytes"},
		{Metric: "gopulse_redis_memory_fragmentation_ratio", Kind: "gauge", Unit: "none"},
		{Metric: "gopulse_redis_blocked_clients", Kind: "gauge", Unit: "count"},
		{Metric: "gopulse_redis_rejected_connections_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_redis_evicted_keys_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_redis_expired_keys_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_redis_network_input_bytes_total", Kind: "counter", Unit: "bytes"},
		{Metric: "gopulse_redis_network_output_bytes_total", Kind: "counter", Unit: "bytes"},
		{Metric: "gopulse_redis_command_calls_total", Kind: "counter", Unit: "count", label: "command"},
		{Metric: "gopulse_redis_command_duration_seconds_total", Kind: "counter", Unit: "seconds", label: "command"},
		{Metric: "gopulse_redis_command_rejected_total", Kind: "counter", Unit: "count", label: "command"},
		{Metric: "gopulse_redis_command_failed_total", Kind: "counter", Unit: "count", label: "command"},
	}
	for i := range items {
		items[i].Source = "redis"
		items[i].TargetID = "redis-exporter-local"
		items[i].ProducerKind = "exporter_plugin"
		items[i].ProducerID = "redis-exporter"
		if items[i].label != "" {
			items[i].LabelKeys = []string{items[i].label}
			items[i].labelKeysKnown = true
		}
	}
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_up", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_uptime_seconds", Kind: "gauge", Unit: "seconds", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_connections", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_max_connections", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_threads_running", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_queries_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_slow_queries_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_transactions_total", Kind: "counter", Unit: "count", label: "result"})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_buffer_pool_data_bytes", Kind: "gauge", Unit: "bytes", label: ""})
	items = append(items, Definition{Source: "mysql", TargetID: "mysql-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "mysql-exporter", Metric: "gopulse_mysql_buffer_pool_dirty_bytes", Kind: "gauge", Unit: "bytes", label: ""})
	for _, item := range []Definition{
		{Metric: "gopulse_mysql_connection_attempts_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_aborted_connections_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_aborted_clients_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_threads_created_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_connections_peak", Kind: "gauge", Unit: "count"},
		{Metric: "gopulse_mysql_statements_total", Kind: "counter", Unit: "count", label: "operation"},
		{Metric: "gopulse_mysql_buffer_pool_read_requests_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_buffer_pool_reads_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_buffer_pool_pages", Kind: "gauge", Unit: "count", label: "state"},
		{Metric: "gopulse_mysql_row_lock_waits_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_row_lock_wait_seconds_total", Kind: "counter", Unit: "seconds"},
		{Metric: "gopulse_mysql_row_lock_current_waits", Kind: "gauge", Unit: "count"},
		{Metric: "gopulse_mysql_temporary_tables_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_temporary_disk_tables_total", Kind: "counter", Unit: "count"},
		{Metric: "gopulse_mysql_data_read_bytes_total", Kind: "counter", Unit: "bytes"},
		{Metric: "gopulse_mysql_data_written_bytes_total", Kind: "counter", Unit: "bytes"},
		{Metric: "gopulse_mysql_data_pending", Kind: "gauge", Unit: "count", label: "operation"},
		{Metric: "gopulse_mysql_log_pending", Kind: "gauge", Unit: "count", label: "operation"},
		{Metric: "gopulse_mysql_redo_log_waits_total", Kind: "counter", Unit: "count"},
	} {
		item.Source, item.TargetID, item.ProducerKind, item.ProducerID = "mysql", "mysql-exporter-local", "exporter_plugin", "mysql-exporter"
		items = append(items, item)
	}
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_up", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_connections", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_channels", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_queues", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_consumers", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_messages", Kind: "gauge", Unit: "count", label: "state"})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_published_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_delivered_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_acked_total", Kind: "counter", Unit: "count", label: ""})
	for _, item := range []Definition{
		{Metric: "gopulse_rabbitmq_queue_messages", Kind: "gauge", Unit: "count", LabelKeys: []string{"vhost", "queue", "state"}},
		{Metric: "gopulse_rabbitmq_queue_consumers", Kind: "gauge", Unit: "count", LabelKeys: []string{"vhost", "queue"}},
		{Metric: "gopulse_rabbitmq_queue_consumer_capacity", Kind: "gauge", Unit: "none", LabelKeys: []string{"vhost", "queue"}},
		{Metric: "gopulse_rabbitmq_queue_messages_total", Kind: "counter", Unit: "count", LabelKeys: []string{"vhost", "queue", "operation"}},
		{Metric: "gopulse_rabbitmq_node_memory_used_bytes", Kind: "gauge", Unit: "bytes", label: "node_id"},
		{Metric: "gopulse_rabbitmq_node_memory_limit_bytes", Kind: "gauge", Unit: "bytes", label: "node_id"},
	} {
		item.Source, item.TargetID, item.ProducerKind, item.ProducerID = "rabbitmq", "rabbitmq-exporter-local", "exporter_plugin", "rabbitmq-exporter"
		items = append(items, item)
	}
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_up", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_brokers", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_controller_available", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_partitions", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_under_replicated_partitions", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_offline_partitions", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "kafka", TargetID: "kafka-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "kafka-exporter", Metric: "gopulse_kafka_consumer_group_lag", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_up", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_cluster_health_status", Kind: "gauge", Unit: "boolean", label: "status"})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_nodes", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_data_nodes", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_active_primary_shards", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_active_shards", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_relocating_shards", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_initializing_shards", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_unassigned_shards", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_pending_tasks", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_documents", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "elasticsearch", TargetID: "elasticsearch-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "elasticsearch-exporter", Metric: "gopulse_elasticsearch_store_size_bytes", Kind: "gauge", Unit: "bytes", label: ""})
	for _, item := range []Definition{
		{Metric: "gopulse_elasticsearch_search_operations_total", Kind: "counter", Unit: "count", LabelKeys: []string{"node_id", "operation"}},
		{Metric: "gopulse_elasticsearch_search_duration_seconds_total", Kind: "counter", Unit: "seconds", LabelKeys: []string{"node_id", "operation"}},
		{Metric: "gopulse_elasticsearch_indexing_operations_total", Kind: "counter", Unit: "count", label: "node_id"},
		{Metric: "gopulse_elasticsearch_indexing_duration_seconds_total", Kind: "counter", Unit: "seconds", label: "node_id"},
	} {
		item.Source, item.TargetID, item.ProducerKind, item.ProducerID = "elasticsearch", "elasticsearch-exporter-local", "exporter_plugin", "elasticsearch-exporter"
		items = append(items, item)
	}
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_up", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_rows_inserted_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_query_requests_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_active_timeseries", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_storage_rows", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_storage_size_bytes", Kind: "gauge", Unit: "bytes", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_free_disk_space_bytes", Kind: "gauge", Unit: "bytes", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_active_merges", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "victoriametrics", TargetID: "victoriametrics-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "victoriametrics-exporter", Metric: "gopulse_victoriametrics_storage_rows_deleted_total", Kind: "counter", Unit: "count", label: ""})
	for _, id := range componentmetrics.Components {
		spec, _ := componentmetrics.Catalog(id)
		for _, f := range spec.Families {
			items = append(items, Definition{Source: id, TargetID: componentmetrics.Target(id), ProducerKind: "component", ProducerID: id, Metric: f.Name, Kind: f.Kind, Unit: f.Unit, LabelKeys: append([]string(nil), f.Keys...), labelKeysKnown: true})
		}
	}
	for i := range items {
		if items[i].label != "" && len(items[i].LabelKeys) == 0 {
			items[i].LabelKeys = []string{items[i].label}
		}
		items[i].labelKeysKnown = true
	}
	return items
}()

var presets = func() map[string]Definition {
	result := make(map[string]Definition, len(Presets))
	for _, definition := range Presets {
		result[definition.Metric] = definition
	}
	return result
}()
