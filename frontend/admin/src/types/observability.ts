export type MetricName = string
export type QueryRange = '15m' | '1h' | '6h' | '24h'
export interface MetricDescriptor {
  metric: MetricName
  kind: 'gauge' | 'counter'
  unit: string
  source: string
  target_id: string
  producer_kind: 'exporter_plugin' | 'component'
  producer_id: string
  label_keys?: string[]
  value_kind?: 'raw' | 'rate' | 'ratio' | 'average'
  display_unit?: string
}
export interface MetricPoint { timestamp: string; value: number }
export interface MetricSeries { labels: Record<string, string>; points: MetricPoint[] }
export interface MetricResult {
  metric: MetricName
  kind: 'gauge' | 'counter'
  unit: 'none' | 'boolean' | 'seconds' | 'count' | 'bytes' | 'state' | 'unix_seconds'
  value_kind?: 'raw' | 'rate' | 'ratio' | 'average'
  display_unit?: string
  quality?: 'complete' | 'sampling' | 'zero_denominator'
  sampled_at?: string
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
  instance_id?: string
  module: string
  message: string
  request_id?: string
  trace_id?: string
  span_id?: string
  attempt_id?: string
  event_id?: string
  command_id?: string
  data_epoch?: string
  event_type?: string
  projection_generation?: string
  routing_contract_revision?: string
  consumer?: string
  user_id?: number
  post_id?: number
  content_revision?: number
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
  trace_id: string
  span_id: string
  event_id: string
  error_code: string
  command_id: string
  data_epoch: string
  post_id: string
  from?: string
  to?: string
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

export type BusinessStageQuality =
  | 'known'
  | 'no_evidence'
  | 'legacy_unknown'
  | 'stale_epoch'
  | 'expired'
  | 'not_requested'
  | 'not_configured'
  | 'backend_error'
  | 'sampling'

export interface BusinessStageLogEntry {
  timestamp: string
  level?: string
  service?: string
  instance_id?: string
  module?: string
  message?: string
  trace_id?: string
  span_id?: string
  attempt_id?: string
  event_id?: string
  command_id?: string
  data_epoch?: string
  post_id?: number
}

export interface BusinessStageFailure {
  attempt_count?: number
  retry_count?: number
  dead_count?: number
  last_reason?: string
  retry_summary?: string
  dead_summary?: string
}

export interface BusinessStageConsumer {
  consumer: string
  routing_contract_revision: string
  applicable_object_version?: number
  quality: BusinessStageQuality
  status?: string
  result?: string
  projection_generation?: string
  actual_version?: number
  completed_at?: string
  retain_until?: string
  failure?: BusinessStageFailure
}

export interface BusinessStageSource {
  source: string
  quality: BusinessStageQuality
  read_at?: string
  data_epoch?: string
  command_id?: string
  event_id?: string
  consumer?: string
  routing_contract_revision?: string
  projection_generation?: string
  status?: string
  result?: string
  reason?: string
  actual_version?: number
  matched?: boolean
  entries?: BusinessStageLogEntry[]
  truncated?: boolean
  trace_ids?: string[]
  sampling?: string
}

export interface BusinessStageEvidence {
  command: BusinessStageSource
  broker: BusinessStageSource
  consumers?: BusinessStageConsumer[]
  visibility: BusinessStageSource
  logs: BusinessStageSource
  trace: BusinessStageSource
}

export interface BusinessStageBudget {
  current_open_requirements: number
  current_open_limit: number
  retained_groups: number
  retained_groups_limit: number
  retained_bytes: number
  retained_bytes_limit: number
  at_eighty_percent: boolean
}

export interface BusinessStageResponse {
  requested_epoch: string
  current_epoch: string
  resolved: {
    command_id?: string
    event_id?: string
    post_id?: number
  }
  routing_contract_revision?: string
  evidence: BusinessStageEvidence
  budget: BusinessStageBudget
}
