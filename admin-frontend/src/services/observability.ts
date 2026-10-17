import { componentContracts } from './componentMetrics'
import type { Page } from '../types/api'
import type { EventEntry, EventFilters, LogEntry, LogFilters, MetricDescriptor, MetricName, MetricResult, QueryRange } from '../types/observability'
import { requestValidatedData, requestValidatedPage } from './http'

export const metricCatalog: ReadonlyArray<{ value: MetricName; label: string }> = [
 ...Object.keys(componentContracts).map(name => ({ value: name as MetricName, label: name.replace(/^gopulse_/, '') })),
  { value: 'gopulse_redis_up', label: 'Redis 可用状态' },
  { value: 'gopulse_redis_uptime_seconds', label: '运行时长' },
  { value: 'gopulse_redis_connected_clients', label: '连接客户端' },
  { value: 'gopulse_redis_used_memory_bytes', label: '已用内存' },
  { value: 'gopulse_redis_commands_processed_total', label: '累计命令' },
  { value: 'gopulse_redis_keyspace_hits_total', label: '累计命中' },
  { value: 'gopulse_redis_keyspace_misses_total', label: '累计未命中' },
  { value: 'gopulse_redis_cpu_seconds_total', label: '累计 CPU 时间' },
  { value: 'gopulse_redis_db_keys', label: '数据库键数' },
  { value: 'gopulse_redis_db_expiring_keys', label: '过期键数' },
  { value: 'gopulse_redis_max_clients', label: '最大客户端连接数' },
  { value: 'gopulse_redis_replication_role', label: '复制角色' },
  { value: 'gopulse_redis_replication_connected_replicas', label: '已连接副本数' },
  { value: 'gopulse_redis_replication_master_repl_offset', label: '主复制偏移' },
  { value: 'gopulse_redis_replication_master_last_io_seconds_ago', label: '主复制最近 I/O 间隔' },
  { value: 'gopulse_redis_replication_master_link_up', label: '主复制链路状态' },
  { value: 'gopulse_redis_replication_sync_in_progress', label: '复制同步中' },
  { value: 'gopulse_redis_replica_offset', label: '副本复制偏移' },
  { value: 'gopulse_redis_replica_lag_seconds', label: '副本延迟' },
  { value: 'gopulse_redis_replica_link_up', label: '副本链路状态' },
  { value: 'gopulse_redis_rdb_changes_since_last_save', label: 'RDB 保存后变更数' },
  { value: 'gopulse_redis_rdb_bgsave_in_progress', label: 'RDB 后台保存中' },
  { value: 'gopulse_redis_rdb_last_save_timestamp_seconds', label: '最近 RDB 保存时间' },
  { value: 'gopulse_redis_rdb_last_bgsave_success', label: '最近 RDB 保存成功' },
  { value: 'gopulse_redis_rdb_current_bgsave_duration_seconds', label: 'RDB 保存耗时' },
  { value: 'gopulse_redis_aof_enabled', label: 'AOF 已启用' },
  { value: 'gopulse_redis_aof_rewrite_in_progress', label: 'AOF 重写中' },
  { value: 'gopulse_redis_aof_rewrite_scheduled', label: 'AOF 已计划重写' },
  { value: 'gopulse_redis_aof_last_rewrite_success', label: '最近 AOF 重写成功' },
  { value: 'gopulse_redis_aof_current_rewrite_duration_seconds', label: 'AOF 重写耗时' },
  { value: 'gopulse_redis_aof_last_write_success', label: '最近 AOF 写入成功' },
  { value: 'gopulse_redis_aof_rewrites_total', label: 'AOF 重写次数' },
  { value: 'gopulse_redis_aof_rewrite_consecutive_failures', label: 'AOF 连续重写失败数' },
  { value: 'gopulse_redis_aof_base_size_bytes', label: 'AOF 基础大小' },
  { value: 'gopulse_redis_aof_current_size_bytes', label: 'AOF 当前大小' },
  { value: 'gopulse_redis_commands_qps', label: 'Redis 命令 QPS' },
  { value: 'gopulse_redis_command_qps', label: 'Redis 分命令 QPS' },
  { value: 'gopulse_redis_keyspace_hit_rate', label: 'Redis Keyspace 命中率' },
  { value: 'gopulse_redis_connection_usage_ratio', label: 'Redis 连接使用率' },
  { value: 'gopulse_redis_memory_usage_ratio', label: 'Redis 内存使用率' },
  { value: 'gopulse_redis_command_average_duration_seconds', label: 'Redis 分命令平均耗时' },
  { value: 'gopulse_mysql_up', label: 'mysql up' },
  { value: 'gopulse_mysql_uptime_seconds', label: 'mysql uptime_seconds' },
  { value: 'gopulse_mysql_connections', label: 'mysql connections' },
  { value: 'gopulse_mysql_max_connections', label: 'mysql max_connections' },
  { value: 'gopulse_mysql_threads_running', label: 'mysql threads_running' },
  { value: 'gopulse_mysql_queries_total', label: 'mysql queries_total' },
  { value: 'gopulse_mysql_slow_queries_total', label: 'mysql slow_queries_total' },
  { value: 'gopulse_mysql_transactions_total', label: 'mysql transactions_total' },
  { value: 'gopulse_mysql_buffer_pool_data_bytes', label: 'mysql buffer_pool_data_bytes' },
  { value: 'gopulse_mysql_buffer_pool_dirty_bytes', label: 'mysql buffer_pool_dirty_bytes' },
  { value: 'gopulse_mysql_deadlocks_total', label: 'MySQL 死锁次数' },
  { value: 'gopulse_mysql_undo_history_length', label: 'MySQL undo history 长度' },
  { value: 'gopulse_mysql_undo_slots_used', label: 'MySQL undo slot 使用数' },
  { value: 'gopulse_mysql_active_transactions', label: 'MySQL 活跃事务数' },
  { value: 'gopulse_mysql_transaction_oldest_age_seconds', label: 'MySQL 最老事务年龄' },
  { value: 'gopulse_mysql_transaction_rows_locked', label: 'MySQL 事务锁定行数' },
  { value: 'gopulse_mysql_transaction_rows_modified', label: 'MySQL 事务修改行数' },
  { value: 'gopulse_mysql_redo_lsn_current', label: 'MySQL 当前 redo LSN' },
  { value: 'gopulse_mysql_redo_lsn_last_checkpoint', label: 'MySQL 最近 checkpoint LSN' },
  { value: 'gopulse_mysql_redo_checkpoint_age_bytes', label: 'MySQL redo checkpoint age' },
  { value: 'gopulse_mysql_redo_log_capacity_bytes', label: 'MySQL redo 日志容量' },
  { value: 'gopulse_mysql_queries_qps', label: 'MySQL 查询 QPS' },
  { value: 'gopulse_mysql_statement_qps', label: 'MySQL 分类型语句 QPS' },
  { value: 'gopulse_mysql_transaction_qps', label: 'MySQL 事务 QPS' },
  { value: 'gopulse_mysql_connection_usage_ratio', label: 'MySQL 连接使用率' },
  { value: 'gopulse_rabbitmq_up', label: 'rabbitmq up' },
  { value: 'gopulse_rabbitmq_connections', label: 'rabbitmq connections' },
  { value: 'gopulse_rabbitmq_channels', label: 'rabbitmq channels' },
  { value: 'gopulse_rabbitmq_queues', label: 'rabbitmq queues' },
  { value: 'gopulse_rabbitmq_consumers', label: 'rabbitmq consumers' },
  { value: 'gopulse_rabbitmq_messages', label: 'rabbitmq messages' },
  { value: 'gopulse_rabbitmq_published_total', label: 'rabbitmq published_total' },
  { value: 'gopulse_rabbitmq_delivered_total', label: 'rabbitmq delivered_total' },
  { value: 'gopulse_rabbitmq_acked_total', label: 'rabbitmq acked_total' },
  { value: 'gopulse_rabbitmq_connections_blocked', label: 'RabbitMQ 阻塞连接数' },
  { value: 'gopulse_rabbitmq_connection_received_bytes_total', label: 'RabbitMQ 接收字节数' },
  { value: 'gopulse_rabbitmq_connection_sent_bytes_total', label: 'RabbitMQ 发送字节数' },
  { value: 'gopulse_rabbitmq_queue_memory_bytes', label: 'RabbitMQ 队列内存' },
  { value: 'gopulse_rabbitmq_queue_message_bytes', label: 'RabbitMQ 队列消息字节' },
  { value: 'gopulse_rabbitmq_node_io_operations_total', label: 'RabbitMQ 节点持久化 I/O 次数' },
  { value: 'gopulse_rabbitmq_node_io_average_duration_seconds', label: 'RabbitMQ 节点持久化 I/O 平均耗时' },
  { value: 'gopulse_kafka_up', label: 'kafka up' },
  { value: 'gopulse_kafka_brokers', label: 'kafka brokers' },
  { value: 'gopulse_kafka_controller_available', label: 'kafka controller_available' },
  { value: 'gopulse_kafka_partitions', label: 'kafka partitions' },
  { value: 'gopulse_kafka_under_replicated_partitions', label: 'kafka under_replicated_partitions' },
  { value: 'gopulse_kafka_offline_partitions', label: 'kafka offline_partitions' },
  { value: 'gopulse_kafka_consumer_group_lag', label: 'kafka consumer_group_lag' },
  { value: 'gopulse_kafka_jmx_up', label: 'Kafka JMX 可用状态' },
  { value: 'gopulse_kafka_broker_bytes_in_total', label: 'Kafka broker 输入字节' },
  { value: 'gopulse_kafka_broker_bytes_out_total', label: 'Kafka broker 输出字节' },
  { value: 'gopulse_kafka_broker_messages_in_total', label: 'Kafka broker 输入消息' },
  { value: 'gopulse_kafka_request_total', label: 'Kafka 请求次数' },
  { value: 'gopulse_kafka_request_errors_total', label: 'Kafka 请求错误' },
  { value: 'gopulse_kafka_request_duration_seconds_total', label: 'Kafka 请求累计耗时' },
  { value: 'gopulse_kafka_request_queue_duration_seconds_total', label: 'Kafka 请求排队累计耗时' },
  { value: 'gopulse_kafka_request_local_duration_seconds_total', label: 'Kafka 请求本地累计耗时' },
  { value: 'gopulse_kafka_request_remote_duration_seconds_total', label: 'Kafka 请求远端累计耗时' },
  { value: 'gopulse_kafka_request_queue_size', label: 'Kafka 请求队列长度' },
  { value: 'gopulse_kafka_network_processor_idle_ratio', label: 'Kafka 网络处理器空闲比例' },
  { value: 'gopulse_kafka_request_handler_idle_ratio', label: 'Kafka 请求线程空闲比例' },
  { value: 'gopulse_kafka_broker_under_replicated_partitions', label: 'Kafka broker 未充分复制分区' },
  { value: 'gopulse_kafka_isr_shrinks_total', label: 'Kafka ISR 收缩次数' },
  { value: 'gopulse_kafka_isr_expands_total', label: 'Kafka ISR 扩张次数' },
  { value: 'gopulse_kafka_leader_elections_total', label: 'Kafka leader 选举次数' },
  { value: 'gopulse_kafka_unclean_leader_elections_total', label: 'Kafka 非正常 leader 选举' },
  { value: 'gopulse_kafka_jvm_heap_used_bytes', label: 'Kafka JVM heap 已用' },
  { value: 'gopulse_kafka_jvm_heap_max_bytes', label: 'Kafka JVM heap 上限' },
  { value: 'gopulse_kafka_jvm_threads', label: 'Kafka JVM 线程数' },
  { value: 'gopulse_kafka_jvm_gc_collections_total', label: 'Kafka JVM GC 次数' },
  { value: 'gopulse_kafka_jvm_gc_collection_time_seconds_total', label: 'Kafka JVM GC 累计耗时' },
  { value: 'gopulse_elasticsearch_up', label: 'elasticsearch up' },
  { value: 'gopulse_elasticsearch_cluster_health_status', label: 'elasticsearch cluster_health_status' },
  { value: 'gopulse_elasticsearch_nodes', label: 'elasticsearch nodes' },
  { value: 'gopulse_elasticsearch_data_nodes', label: 'elasticsearch data_nodes' },
  { value: 'gopulse_elasticsearch_active_primary_shards', label: 'elasticsearch active_primary_shards' },
  { value: 'gopulse_elasticsearch_active_shards', label: 'elasticsearch active_shards' },
  { value: 'gopulse_elasticsearch_relocating_shards', label: 'elasticsearch relocating_shards' },
  { value: 'gopulse_elasticsearch_initializing_shards', label: 'elasticsearch initializing_shards' },
  { value: 'gopulse_elasticsearch_unassigned_shards', label: 'elasticsearch unassigned_shards' },
  { value: 'gopulse_elasticsearch_pending_tasks', label: 'elasticsearch pending_tasks' },
  { value: 'gopulse_elasticsearch_documents', label: 'elasticsearch documents' },
  { value: 'gopulse_elasticsearch_store_size_bytes', label: 'elasticsearch store_size_bytes' },
  { value: 'gopulse_elasticsearch_cache_memory_bytes', label: 'Elasticsearch 缓存内存' },
  { value: 'gopulse_elasticsearch_cache_hits_total', label: 'Elasticsearch 缓存命中' },
  { value: 'gopulse_elasticsearch_cache_misses_total', label: 'Elasticsearch 缓存未命中' },
  { value: 'gopulse_elasticsearch_cache_evictions_total', label: 'Elasticsearch 缓存驱逐' },
  { value: 'gopulse_elasticsearch_merge_operations_total', label: 'Elasticsearch merge 次数' },
  { value: 'gopulse_elasticsearch_merge_duration_seconds_total', label: 'Elasticsearch merge 耗时' },
  { value: 'gopulse_elasticsearch_merge_in_flight', label: 'Elasticsearch merge 进行中' },
  { value: 'gopulse_elasticsearch_merge_current_size_bytes', label: 'Elasticsearch merge 当前字节' },
  { value: 'gopulse_elasticsearch_refresh_operations_total', label: 'Elasticsearch refresh 次数' },
  { value: 'gopulse_elasticsearch_refresh_duration_seconds_total', label: 'Elasticsearch refresh 耗时' },
  { value: 'gopulse_elasticsearch_refresh_listeners', label: 'Elasticsearch refresh listeners' },
  { value: 'gopulse_elasticsearch_flush_operations_total', label: 'Elasticsearch flush 次数' },
  { value: 'gopulse_elasticsearch_flush_duration_seconds_total', label: 'Elasticsearch flush 耗时' },
  { value: 'gopulse_elasticsearch_circuit_breaker_tripped_total', label: 'Elasticsearch 断路器触发' },
  { value: 'gopulse_elasticsearch_circuit_breaker_estimated_bytes', label: 'Elasticsearch 断路器估算字节' },
  { value: 'gopulse_elasticsearch_circuit_breaker_limit_bytes', label: 'Elasticsearch 断路器上限字节' },
  { value: 'gopulse_elasticsearch_fielddata_memory_bytes', label: 'Elasticsearch fielddata 内存' },
  { value: 'gopulse_elasticsearch_segments_count', label: 'Elasticsearch segments 数量' },
  { value: 'gopulse_elasticsearch_segments_memory_bytes', label: 'Elasticsearch segments 内存' },
  { value: 'gopulse_victoriametrics_up', label: 'VictoriaMetrics up' },
  { value: 'gopulse_victoriametrics_rows_inserted_total', label: 'VictoriaMetrics rows_inserted_total' },
  { value: 'gopulse_victoriametrics_query_requests_total', label: 'VictoriaMetrics query_requests_total' },
  { value: 'gopulse_victoriametrics_active_timeseries', label: 'VictoriaMetrics active_timeseries' },
  { value: 'gopulse_victoriametrics_storage_rows', label: 'VictoriaMetrics storage_rows' },
  { value: 'gopulse_victoriametrics_storage_size_bytes', label: 'VictoriaMetrics storage_size_bytes' },
  { value: 'gopulse_victoriametrics_free_disk_space_bytes', label: 'VictoriaMetrics free_disk_space_bytes' },
  { value: 'gopulse_victoriametrics_active_merges', label: 'VictoriaMetrics active_merges' },
  { value: 'gopulse_victoriametrics_storage_rows_deleted_total', label: 'VictoriaMetrics storage_rows_deleted_total' },
  { value: 'gopulse_victoriametrics_write_batches_total', label: 'VictoriaMetrics 写入批次数' },
  { value: 'gopulse_victoriametrics_write_batch_rows_total', label: 'VictoriaMetrics 写入批次行数' },
  { value: 'gopulse_victoriametrics_write_requests_total', label: 'VictoriaMetrics 写入请求数' },
  { value: 'gopulse_victoriametrics_write_errors_total', label: 'VictoriaMetrics 写入错误数' },
  { value: 'gopulse_victoriametrics_query_errors_total', label: 'VictoriaMetrics 查询错误数' },
  { value: 'gopulse_victoriametrics_query_duration_seconds_total', label: 'VictoriaMetrics 查询累计耗时' },
  { value: 'gopulse_victoriametrics_active_queries', label: 'VictoriaMetrics 活跃查询' },
  { value: 'gopulse_victoriametrics_query_capacity', label: 'VictoriaMetrics 查询并发上限' },
  { value: 'gopulse_victoriametrics_query_limit_reached_total', label: 'VictoriaMetrics 查询排队次数' },
  { value: 'gopulse_victoriametrics_query_limit_timeout_total', label: 'VictoriaMetrics 查询排队超时' },
  { value: 'gopulse_victoriametrics_process_cpu_seconds_total', label: 'VictoriaMetrics CPU 时间' },
  { value: 'gopulse_victoriametrics_process_resident_memory_bytes', label: 'VictoriaMetrics RSS' },
  { value: 'gopulse_victoriametrics_go_heap_alloc_bytes', label: 'VictoriaMetrics Go heap 已用' },
  { value: 'gopulse_victoriametrics_go_heap_sys_bytes', label: 'VictoriaMetrics Go heap 系统' },
  { value: 'gopulse_victoriametrics_go_gc_cycles_total', label: 'VictoriaMetrics GC 次数' },
  { value: 'gopulse_victoriametrics_go_gc_pause_seconds_total', label: 'VictoriaMetrics GC 暂停耗时' },
  { value: 'gopulse_victoriametrics_go_goroutines', label: 'VictoriaMetrics goroutine 数' },
  { value: 'gopulse_victoriametrics_go_threads', label: 'VictoriaMetrics thread 数' },
]
export const ranges: ReadonlyArray<{ value: QueryRange; label: string; milliseconds: number }> = [
  { value: '15m', label: '最近 15 分钟', milliseconds: 15 * 60_000 },
  { value: '1h', label: '最近 1 小时', milliseconds: 60 * 60_000 },
  { value: '6h', label: '最近 6 小时', milliseconds: 6 * 60 * 60_000 },
  { value: '24h', label: '最近 24 小时', milliseconds: 24 * 60 * 60_000 },
]
export const logCatalog: Readonly<Record<string, Readonly<Record<string, readonly string[]>>>> = {
  backend: {
    http: ['request id generation failed','http request completed','http panic recovered'],
    auth: ['user registered','user logged in','user logged out'],
    post: ['post created','post committed'], comment: ['comment created'], like: ['post liked','post unliked'], notification: ['notification marked read'],
    cache: ['post detail cache fill failed','post detail cache read failed','post detail cache invalidation failed'],
    outbox: ['outbox cleanup failed','outbox claim failed','outbox event invalid','outbox publish failed','outbox mark published failed','outbox event published','outbox release failed'],
    lifecycle: ['backend listening','backend stopped','backend server failed','backend shutdown started','backend shutdown failed','resource close failed'],
  },
  'business-worker': {
    lifecycle: ['business worker started','business worker stopped','business worker initialization failed','resource close failed'],
    worker: ['event ignored','event processed','message acknowledgement failed','retry publish failed','message requeue failed','event retry scheduled','dead letter publish failed','event dead lettered','connection unavailable','connection restored','session close failed','session interrupted','delivery stop failed','shutdown timeout'],
    notification: ['event ignored','event processed','message acknowledgement failed','retry publish failed','message requeue failed','event retry scheduled','dead letter publish failed','event dead lettered','connection unavailable','connection restored','session close failed','session interrupted','delivery stop failed','shutdown timeout'],
  },
  'search-indexer': {
    lifecycle: ['search indexer started','search indexer stopped','search indexer initialization failed','resource close failed'],
    worker: ['event ignored','event processed','message acknowledgement failed','retry publish failed','message requeue failed','event retry scheduled','dead letter publish failed','event dead lettered','connection unavailable','connection restored','session close failed','session interrupted','delivery stop failed','shutdown timeout'],
    search: ['event ignored','event processed','message acknowledgement failed','retry publish failed','message requeue failed','event retry scheduled','dead letter publish failed','event dead lettered','connection unavailable','connection restored','session close failed','session interrupted','delivery stop failed','shutdown timeout'],
  },
  'search-reindex': { search: ['search reindex arguments invalid','search reindex initialization failed','search reindex started','search reindex skipped','search reindex completed','search reindex failed','resource close failed'] },
}
export const eventSeverities: Readonly<Record<string, 'info'|'warn'|'error'>> = {
  exporter_plugin_installed:'info',exporter_plugin_started:'info',exporter_plugin_stopped:'info',exporter_plugin_updated:'info',
  exporter_plugin_failed:'error',exporter_plugin_exited:'error',metrics_collection_failed:'warn',metrics_collection_recovered:'info',metrics_target_unavailable:'warn',metrics_target_recovered:'info',
}
export const eventOperations: Readonly<Record<string, readonly string[]>> = {
  exporter_plugin_installed:['install'],exporter_plugin_started:['start'],exporter_plugin_stopped:['stop'],exporter_plugin_updated:['update'],
  exporter_plugin_failed:['start','stop','update','recover'],exporter_plugin_exited:['start'],metrics_collection_failed:['scrape','publish'],metrics_collection_recovered:['scrape'],metrics_target_unavailable:['scrape'],metrics_target_recovered:['scrape'],
}
export const eventErrorCodes = ['start_failed','stop_failed','update_failed','rollback_failed','recovery_failed','recovery_invalid','process_exited','scrape_timeout','network_failed','response_too_large','parse_failed','contract_invalid','content_invalid','http_invalid','scrape_failed','message_id_failed','publish_failed'] as const

export const eventNames = [
  'exporter_plugin_installed', 'exporter_plugin_started', 'exporter_plugin_stopped', 'exporter_plugin_updated',
  'exporter_plugin_failed', 'exporter_plugin_exited', 'metrics_collection_failed', 'metrics_collection_recovered',
  'metrics_target_unavailable', 'metrics_target_recovered',
] as const

const metricSources = ['redis','mysql','rabbitmq','kafka','elasticsearch','victoriametrics','backend','business-worker','search-indexer','monitor','marshaller'] as const
const metricUnits = new Set(['none','boolean','seconds','count','bytes','state','unix_seconds'])
function validMetricName(value: unknown): value is string {
 return typeof value === 'string' && value.length <= 128 && /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(value) && metricSources.some(source => value.startsWith(`gopulse_${source.replaceAll('-', '_')}_`) && value.length > `gopulse_${source.replaceAll('-', '_')}_`.length)
}
function validMetricLabels(labels: Record<string, unknown>): boolean {
 return Object.keys(labels).length <= 16 && Object.entries(labels).every(([key,value]) => /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(key) && key.length <= 64 && !key.startsWith('__') && !['source','target_id','producer_kind','producer_id','producer_version'].includes(key) && typeof value === 'string' && new TextEncoder().encode(value).length <= 256 && !value.includes('\0'))
}
const rangeNames = new Set(ranges.map((item) => item.value))
const logKeys = new Set(['timestamp','level','service','instance_id','module','message','request_id','trace_id','span_id','attempt_id','event_id','command_id','data_epoch','event_type','projection_generation','routing_contract_revision','consumer','user_id','post_id','content_revision','comment_id','notification_id','outbox_id','method','route','status','duration_ms','response_bytes','error_code','reason','operation','resource','stage','result','attempt','batch_size','document_count','panic_recovered','response_committed'])
const metadataKeys = new Set(['plugin_id','plugin_version','previous_plugin_version','operation','from_state','to_state','error_code','scrape_status'])
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function keysAllowed(value: Record<string, unknown>, allowed: Set<string>): boolean { return Object.keys(value).every((key) => allowed.has(key)) }
function timestamp(value: unknown): value is string { return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) && Number.isFinite(Date.parse(value)) }
function finite(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) }
function optionalString(value: unknown): boolean { return value === undefined || typeof value === 'string' }
function optionalInteger(value: unknown): boolean { return value === undefined || (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) }
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const identity = /^[A-Za-z0-9._:/-]{1,255}$/

export function isMetricResult(value: unknown): value is MetricResult {
  if (!record(value) || !keysAllowed(value, new Set(['from','kind','metric','range','series','step_seconds','to','unit','value_kind','display_unit','quality','sampled_at']))) return false
  if (!validMetricName(value.metric) || (value.kind !== 'gauge' && value.kind !== 'counter') || !metricUnits.has(String(value.unit)) || typeof value.range !== 'string' || !rangeNames.has(value.range as QueryRange) || !timestamp(value.from) || !timestamp(value.to) || !Number.isSafeInteger(value.step_seconds) || !Array.isArray(value.series)) return false
  if (value.value_kind !== undefined && !['raw','rate','ratio','average'].includes(String(value.value_kind))) return false
  if (value.display_unit !== undefined && (typeof value.display_unit !== 'string' || value.display_unit.length > 32)) return false
  if (value.quality !== undefined && !['complete','sampling','zero_denominator'].includes(String(value.quality))) return false
  if (value.sampled_at !== undefined && !timestamp(value.sampled_at)) return false
  if (value.series.length > 1024) return false
  const range = ranges.find((item) => item.value === value.range)
  const expectedSteps: Record<QueryRange, number> = { '15m':15, '1h':60, '6h':300, '24h':900 }
  const from = Date.parse(value.from); const to = Date.parse(value.to)
  if (!range || from >= to || to - from !== range.milliseconds || value.step_seconds !== expectedSteps[value.range as QueryRange]) return false
  let points = 0
  const seriesKeys = new Set<string>()
  return value.series.every((series) => {
    if (!record(series) || Object.keys(series).sort().join() !== 'labels,points' || !record(series.labels) || !Array.isArray(series.points)) return false
    if (!validMetricLabels(series.labels)) return false
    const seriesKey = JSON.stringify(Object.entries(series.labels).sort(([a], [b]) => a.localeCompare(b)))
    if (seriesKeys.has(seriesKey)) return false
    seriesKeys.add(seriesKey)
    let previous = Number.NEGATIVE_INFINITY
    return series.points.every((point) => {
      points++
      if (points > 1024 * 97 || !record(point) || Object.keys(point).sort().join() !== 'timestamp,value' || !timestamp(point.timestamp) || !finite(point.value)) return false
      if ((value.kind === 'counter' && point.value < 0) || (value.unit === 'state' && ![-1,0,1].includes(point.value)) || (value.unit === 'boolean' && ![0,1].includes(point.value))) return false
      const at = Date.parse(point.timestamp)
      if (at < from || at > to || at <= previous) return false
      previous = at
      return true
    })
  })
}

export function isLogEntry(value: unknown): value is LogEntry {
  if (!record(value) || !keysAllowed(value, logKeys) || !timestamp(value.timestamp) || !['info','warn','error'].includes(String(value.level)) || typeof value.service !== 'string' || typeof value.module !== 'string' || typeof value.message !== 'string') return false
  const service = logCatalog[value.service]; const messages = service?.[value.module]
  if (!messages || !messages.includes(value.message)) return false
  for (const key of ['instance_id','request_id','trace_id','span_id','attempt_id','event_id','command_id','data_epoch','event_type','projection_generation','routing_contract_revision','consumer','method','route','error_code','reason','operation','resource','stage','result']) if (!optionalString(value[key])) return false
  if (value.trace_id !== undefined && (!/^[0-9a-f]{32}$/.test(String(value.trace_id)) || /^0+$/.test(String(value.trace_id)))) return false
  if (value.span_id !== undefined && (!/^[0-9a-f]{16}$/.test(String(value.span_id)) || /^0+$/.test(String(value.span_id)))) return false
  if (value.command_id !== undefined && !uuid.test(String(value.command_id))) return false
  if (value.data_epoch !== undefined && !uuid.test(String(value.data_epoch))) return false
  if (value.projection_generation !== undefined && !identity.test(String(value.projection_generation))) return false
  if (value.routing_contract_revision !== undefined && value.routing_contract_revision !== 'post-created-search/1') return false
  if (value.consumer !== undefined && value.consumer !== 'business' && value.consumer !== 'search') return false
  for (const key of ['user_id','post_id','content_revision','comment_id','notification_id','outbox_id','status','duration_ms','response_bytes','attempt','batch_size','document_count']) if (!optionalInteger(value[key])) return false
  return (value.panic_recovered === undefined || typeof value.panic_recovered === 'boolean') && (value.response_committed === undefined || typeof value.response_committed === 'boolean')
}
const eventMessages: Readonly<Record<string, string>> = {
  exporter_plugin_installed:'exporter plugin installed', exporter_plugin_started:'exporter plugin started', exporter_plugin_stopped:'exporter plugin stopped', exporter_plugin_updated:'exporter plugin updated',
  exporter_plugin_failed:'exporter plugin operation failed', exporter_plugin_exited:'exporter plugin exited unexpectedly', metrics_collection_failed:'metrics collection failed', metrics_collection_recovered:'metrics collection recovered',
  metrics_target_unavailable:'metrics target unavailable', metrics_target_recovered:'metrics target recovered',
}
const semver = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/
const operationErrors: Readonly<Record<string, readonly string[]>> = {
  start:['start_failed','process_exited'], stop:['stop_failed'], update:['update_failed','rollback_failed'], recover:['recovery_failed','recovery_invalid'],
  scrape:['scrape_timeout','network_failed','response_too_large','parse_failed','contract_invalid','content_invalid','http_invalid','scrape_failed','message_id_failed'], publish:['publish_failed'],
}
function absent(metadata: Record<string, unknown>, ...keys: string[]): boolean { return keys.every((key) => metadata[key] === undefined) }
function operationError(metadata: Record<string, unknown>): boolean { return typeof metadata.operation === 'string' && typeof metadata.error_code === 'string' && operationErrors[metadata.operation]?.includes(metadata.error_code) === true }
export function isEventEntry(value: unknown): value is EventEntry {
  if (!record(value) || Object.keys(value).sort().join() !== ['event_name','message','metadata','severity','source','timestamp'].sort().join() || !timestamp(value.timestamp) || typeof value.event_name !== 'string' || !eventNames.includes(value.event_name as typeof eventNames[number]) || !record(value.metadata) || !keysAllowed(value.metadata, metadataKeys)) return false
  const name = value.event_name
  const m = value.metadata
  if (value.source !== 'monitor' || value.severity !== eventSeverities[name] || value.message !== eventMessages[name] || !['redis-exporter','mysql-exporter','rabbitmq-exporter','kafka-exporter','elasticsearch-exporter','victoriametrics-exporter'].includes(String(m.plugin_id)) || ![...metadataKeys].filter((key) => key !== 'plugin_id').every((key) => optionalString(m[key]))) return false
  const version = typeof m.plugin_version === 'string' && semver.test(m.plugin_version)
  const previous = typeof m.previous_plugin_version === 'string' && semver.test(m.previous_plugin_version)
  const noError = absent(m, 'error_code', 'scrape_status')
  switch (name) {
    case 'exporter_plugin_installed': return version && noError && absent(m,'previous_plugin_version') && m.operation === 'install' && m.from_state === 'not_installed' && m.to_state === 'running'
    case 'exporter_plugin_started': return version && noError && absent(m,'previous_plugin_version') && m.operation === 'start' && (m.from_state === 'stopped' || m.from_state === 'failed') && m.to_state === 'running'
    case 'exporter_plugin_stopped': return version && noError && absent(m,'previous_plugin_version') && m.operation === 'stop' && (m.from_state === 'running' || m.from_state === 'failed') && m.to_state === 'stopped'
    case 'exporter_plugin_updated': return version && previous && noError && m.operation === 'update' && (m.from_state === 'running' || m.from_state === 'stopped') && m.from_state === m.to_state
    case 'exporter_plugin_failed': return version && absent(m,'previous_plugin_version','from_state','scrape_status') && ['not_installed','stopped','running','failed'].includes(String(m.to_state)) && operationError(m) && m.error_code !== 'process_exited'
    case 'exporter_plugin_exited': return version && absent(m,'previous_plugin_version','from_state','scrape_status') && m.operation === 'start' && m.to_state === 'failed' && m.error_code === 'process_exited'
    case 'metrics_collection_failed': return absent(m,'plugin_version','previous_plugin_version','from_state','to_state','scrape_status') && operationError(m)
    case 'metrics_collection_recovered': case 'metrics_target_recovered': return absent(m,'plugin_version','previous_plugin_version','from_state','to_state','error_code') && m.operation === 'scrape' && m.scrape_status === 'success'
    case 'metrics_target_unavailable': return absent(m,'plugin_version','previous_plugin_version','from_state','to_state','error_code') && m.operation === 'scrape' && m.scrape_status === 'target_unavailable'
    default: return false
  }
}
function pageQuery(filters: Record<string,string>, cursor?: string): string {
  const params = new URLSearchParams()
  if (cursor) { params.set('cursor', cursor); return params.toString() }
  const range = ranges.find((item) => item.value === filters.range) ?? ranges[0]
  const to = new Date(); const from = new Date(to.getTime() - range.milliseconds)
  params.set('from', filters.from?.trim() || from.toISOString()); params.set('to', filters.to?.trim() || to.toISOString()); params.set('limit', '50')
  for (const [key, value] of Object.entries(filters)) if (key !== 'range' && key !== 'from' && key !== 'to' && typeof value === 'string' && value.trim()) params.set(key, value.trim())
  return params.toString()
}
export const observabilityApi = {
  metrics: (metric: MetricName, range: QueryRange, labelsOrSignal: Record<string, string> | AbortSignal = {}, signal?: AbortSignal) => {
    const isSignal = (value: Record<string, string> | AbortSignal): value is AbortSignal => typeof AbortSignal !== 'undefined' && value instanceof AbortSignal
    const labels = isSignal(labelsOrSignal) ? {} : labelsOrSignal
    const requestSignal = isSignal(labelsOrSignal) ? labelsOrSignal : signal
    const params = new URLSearchParams({ metric, range })
    for (const [key, value] of Object.entries(labels)) if (value.trim()) params.set(key, value.trim())
    return requestValidatedData<MetricResult>(`/observability/metrics?${params}`, isMetricResult, { signal: requestSignal })
  },
  logs: (filters: LogFilters, cursor?: string, signal?: AbortSignal): Promise<Page<LogEntry>> => requestValidatedPage(`/observability/logs?${pageQuery(filters as unknown as Record<string,string>, cursor)}`, isLogEntry, { signal }),
  events: (filters: EventFilters, cursor?: string, signal?: AbortSignal): Promise<Page<EventEntry>> => requestValidatedPage(`/observability/events?${pageQuery(filters as unknown as Record<string,string>, cursor)}`, isEventEntry, { signal }),
}

export function isMetricCatalog(value: unknown): value is MetricDescriptor[] {
 if (!Array.isArray(value) || value.length > 2048 || new Set(value.map(item => record(item) ? item.metric : undefined)).size !== value.length) return false
 return value.every(item => {
  if (!record(item) || !keysAllowed(item,new Set(['metric','kind','unit','source','target_id','producer_kind','producer_id','label_keys','value_kind','display_unit'])) || !validMetricName(item.metric) || (item.kind !== 'gauge' && item.kind !== 'counter') || !metricUnits.has(String(item.unit)) || typeof item.source !== 'string' || !metricSources.includes(item.source as typeof metricSources[number]) || !item.metric.startsWith(`gopulse_${item.source.replaceAll('-', '_')}_`)) return false
  if (item.value_kind !== undefined && !['raw','rate','ratio','average'].includes(String(item.value_kind))) return false
  if (item.display_unit !== undefined && (typeof item.display_unit !== 'string' || item.display_unit.length > 32)) return false
  if (item.label_keys !== undefined && (!Array.isArray(item.label_keys) || new Set(item.label_keys).size !== item.label_keys.length || !item.label_keys.every(key => typeof key === 'string') || !validMetricLabels(Object.fromEntries(item.label_keys.map(key => [key,'']))))) return false
  const component = ['backend','business-worker','search-indexer','monitor','marshaller'].includes(item.source)
  return item.producer_kind === (component ? 'component' : 'exporter_plugin') && item.producer_id === (component ? item.source : `${item.source}-exporter`) && item.target_id === (component ? `${item.source}-local` : `${item.source}-exporter-local`)
 })
}
export const loadMetricCatalog = (signal?: AbortSignal) => requestValidatedData('/observability/metrics/catalog', isMetricCatalog, { signal })
