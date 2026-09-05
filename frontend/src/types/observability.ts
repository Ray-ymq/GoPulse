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
export type QueryRange = '15m' | '1h' | '6h' | '24h'
export interface MetricPoint { timestamp: string; value: number }
export interface MetricSeries { labels: { mode?: 'user' | 'system'; db?: string }; points: MetricPoint[] }
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
