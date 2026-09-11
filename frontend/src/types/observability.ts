export type MetricName =
  | 'gopulse_redis_up'
  | 'gopulse_redis_uptime_seconds'
  | 'gopulse_redis_connected_clients'
  | 'gopulse_redis_used_memory_bytes'
  | 'gopulse_redis_commands_processed_total'
  | 'gopulse_redis_keyspace_hits_total'
  | 'gopulse_redis_keyspace_misses_total'
  | 'gopulse_redis_cpu_seconds_total'
  | 'gopulse_redis_db_keys'
  | 'gopulse_redis_db_expiring_keys'
  | 'gopulse_mysql_up'
  | 'gopulse_mysql_uptime_seconds'
  | 'gopulse_mysql_connections'
  | 'gopulse_mysql_max_connections'
  | 'gopulse_mysql_threads_running'
  | 'gopulse_mysql_queries_total'
  | 'gopulse_mysql_slow_queries_total'
  | 'gopulse_mysql_transactions_total'
  | 'gopulse_mysql_buffer_pool_data_bytes'
  | 'gopulse_mysql_buffer_pool_dirty_bytes'
  | 'gopulse_rabbitmq_up'
  | 'gopulse_rabbitmq_connections'
  | 'gopulse_rabbitmq_channels'
  | 'gopulse_rabbitmq_queues'
  | 'gopulse_rabbitmq_consumers'
  | 'gopulse_rabbitmq_messages'
  | 'gopulse_rabbitmq_published_total'
  | 'gopulse_rabbitmq_delivered_total'
  | 'gopulse_rabbitmq_acked_total'
  | 'gopulse_kafka_up'
  | 'gopulse_kafka_brokers'
  | 'gopulse_kafka_controller_available'
  | 'gopulse_kafka_partitions'
  | 'gopulse_kafka_under_replicated_partitions'
  | 'gopulse_kafka_offline_partitions'
  | 'gopulse_kafka_consumer_group_lag'
  | 'gopulse_elasticsearch_up'
  | 'gopulse_elasticsearch_cluster_health_status'
  | 'gopulse_elasticsearch_nodes'
  | 'gopulse_elasticsearch_data_nodes'
  | 'gopulse_elasticsearch_active_primary_shards'
  | 'gopulse_elasticsearch_active_shards'
  | 'gopulse_elasticsearch_relocating_shards'
  | 'gopulse_elasticsearch_initializing_shards'
  | 'gopulse_elasticsearch_unassigned_shards'
  | 'gopulse_elasticsearch_pending_tasks'
  | 'gopulse_elasticsearch_documents'
  | 'gopulse_elasticsearch_store_size_bytes'
  | 'gopulse_victoriametrics_up'
  | 'gopulse_victoriametrics_rows_inserted_total'
  | 'gopulse_victoriametrics_query_requests_total'
  | 'gopulse_victoriametrics_active_timeseries'
  | 'gopulse_victoriametrics_storage_rows'
  | 'gopulse_victoriametrics_storage_size_bytes'
  | 'gopulse_victoriametrics_free_disk_space_bytes'
  | 'gopulse_victoriametrics_active_merges'
  | 'gopulse_victoriametrics_storage_rows_deleted_total'
export type QueryRange = '15m' | '1h' | '6h' | '24h'
export interface MetricPoint { timestamp: string; value: number }
export interface MetricSeries { labels: { mode?: 'user' | 'system'; db?: string; result?: 'commit' | 'rollback'; state?: 'ready' | 'unacked'; status?: 'green' | 'yellow' | 'red' }; points: MetricPoint[] }
export interface MetricResult {
  metric: MetricName
  kind: 'gauge' | 'counter'
  unit: 'boolean' | 'seconds' | 'count' | 'bytes'
  range: QueryRange
  from: string
  to: string
  step_seconds: number
  series: MetricSeries[]
}

export interface LogEntry {
  timestamp: string
  level: 'info' | 'warn' | 'error'
  service: string
  module: string
  message: string
  request_id?: string
  event_id?: string
  event_type?: string
  user_id?: number
  post_id?: number
  comment_id?: number
  notification_id?: number
  outbox_id?: number
  method?: string
  route?: string
  status?: number
  duration_ms?: number
  response_bytes?: number
  error_code?: string
  reason?: string
  operation?: string
  resource?: string
  stage?: string
  result?: string
  attempt?: number
  batch_size?: number
  document_count?: number
  panic_recovered?: boolean
  response_committed?: boolean
}
export interface LogFilters {
  range: QueryRange
  service: string
  module: string
  level: string
  message: string
  request_id: string
  event_id: string
  error_code: string
}

export interface EventMetadata {
  plugin_id: string
  plugin_version?: string
  previous_plugin_version?: string
  operation?: string
  from_state?: string
  to_state?: string
  error_code?: string
  scrape_status?: string
}
export interface EventEntry {
  timestamp: string
  event_name: string
  source: string
  severity: 'info' | 'warn' | 'error'
  message: string
  metadata: EventMetadata
}
export interface EventFilters {
  range: QueryRange
  source: string
  event_name: string
  severity: string
  plugin_id: string
  operation: string
  error_code: string
}
