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
	}
	for i := range items {
		items[i].Source = "redis"
		items[i].TargetID = "redis-exporter-local"
		items[i].ProducerKind = "exporter_plugin"
		items[i].ProducerID = "redis-exporter"
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
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_up", Kind: "gauge", Unit: "boolean", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_connections", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_channels", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_queues", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_consumers", Kind: "gauge", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_messages", Kind: "gauge", Unit: "count", label: "state"})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_published_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_delivered_total", Kind: "counter", Unit: "count", label: ""})
	items = append(items, Definition{Source: "rabbitmq", TargetID: "rabbitmq-exporter-local", ProducerKind: "exporter_plugin", ProducerID: "rabbitmq-exporter", Metric: "gopulse_rabbitmq_acked_total", Kind: "counter", Unit: "count", label: ""})
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
			items = append(items, Definition{Source: id, TargetID: componentmetrics.Target(id), ProducerKind: "component", ProducerID: id, Metric: f.Name, Kind: f.Kind, Unit: f.Unit})
		}
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
