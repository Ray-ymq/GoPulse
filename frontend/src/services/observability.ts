import type { Page } from '../types/api'
import type { EventEntry, EventFilters, LogEntry, LogFilters, MetricName, MetricResult, QueryRange } from '../types/observability'
import { requestValidatedData, requestValidatedPage } from './http'

export const metricCatalog: ReadonlyArray<{ value: MetricName; label: string }> = [
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
    post: ['post created'], comment: ['comment created'], like: ['post liked','post unliked'], notification: ['notification marked read'],
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

const metricNames = new Set(metricCatalog.map((item) => item.value))
const metricContracts: Record<MetricName, { kind:'gauge'|'counter'; unit:'boolean'|'seconds'|'count'|'bytes'; label?:'mode'|'db' }> = {
  gopulse_redis_up:{kind:'gauge',unit:'boolean'},gopulse_redis_uptime_seconds:{kind:'gauge',unit:'seconds'},gopulse_redis_connected_clients:{kind:'gauge',unit:'count'},gopulse_redis_used_memory_bytes:{kind:'gauge',unit:'bytes'},
  gopulse_redis_commands_processed_total:{kind:'counter',unit:'count'},gopulse_redis_keyspace_hits_total:{kind:'counter',unit:'count'},gopulse_redis_keyspace_misses_total:{kind:'counter',unit:'count'},gopulse_redis_cpu_seconds_total:{kind:'counter',unit:'seconds',label:'mode'},
  gopulse_redis_db_keys:{kind:'gauge',unit:'count',label:'db'},gopulse_redis_db_expiring_keys:{kind:'gauge',unit:'count',label:'db'},
}
const rangeNames = new Set(ranges.map((item) => item.value))
const logKeys = new Set(['timestamp','level','service','module','message','request_id','event_id','event_type','user_id','post_id','comment_id','notification_id','outbox_id','method','route','status','duration_ms','response_bytes','error_code','reason','operation','resource','stage','result','attempt','batch_size','document_count','panic_recovered','response_committed'])
const metadataKeys = new Set(['plugin_id','plugin_version','previous_plugin_version','operation','from_state','to_state','error_code','scrape_status'])
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function keysAllowed(value: Record<string, unknown>, allowed: Set<string>): boolean { return Object.keys(value).every((key) => allowed.has(key)) }
function timestamp(value: unknown): value is string { return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) && Number.isFinite(Date.parse(value)) }
function finite(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) }
function optionalString(value: unknown): boolean { return value === undefined || typeof value === 'string' }
function optionalInteger(value: unknown): boolean { return value === undefined || (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) }

export function isMetricResult(value: unknown): value is MetricResult {
  if (!record(value) || Object.keys(value).sort().join() !== ['from','kind','metric','range','series','step_seconds','to','unit'].sort().join()) return false
  if (typeof value.metric !== 'string' || !metricNames.has(value.metric as MetricName) || (value.kind !== 'gauge' && value.kind !== 'counter') || !['boolean','seconds','count','bytes'].includes(String(value.unit)) || typeof value.range !== 'string' || !rangeNames.has(value.range as QueryRange) || !timestamp(value.from) || !timestamp(value.to) || !Number.isSafeInteger(value.step_seconds) || !Array.isArray(value.series) || value.series.length > 32) return false
  const contract = metricContracts[value.metric as MetricName]
  const range = ranges.find((item) => item.value === value.range)
  const expectedSteps: Record<QueryRange, number> = { '15m':15, '1h':60, '6h':300, '24h':900 }
  const from = Date.parse(value.from); const to = Date.parse(value.to)
  if (!range || from >= to || to - from !== range.milliseconds || value.step_seconds !== expectedSteps[value.range as QueryRange] || value.kind !== contract.kind || value.unit !== contract.unit) return false
  let points = 0
  const seriesKeys = new Set<string>()
  return value.series.every((series) => {
    if (!record(series) || Object.keys(series).sort().join() !== 'labels,points' || !record(series.labels) || !keysAllowed(series.labels, new Set(['mode','db'])) || !Array.isArray(series.points)) return false
    if (series.labels.mode !== undefined && series.labels.mode !== 'user' && series.labels.mode !== 'system') return false
    if (contract.label === undefined && Object.keys(series.labels).length !== 0) return false
    if (contract.label === 'mode' && (Object.keys(series.labels).length !== 1 || series.labels.mode === undefined)) return false
    if (contract.label === 'db' && (Object.keys(series.labels).length !== 1 || series.labels.db === undefined)) return false
    if (series.labels.db !== undefined && (typeof series.labels.db !== 'string' || !/^(0|[1-9][0-9]*)$/.test(series.labels.db))) return false
    const seriesKey = `${series.labels.mode ?? ''}|${series.labels.db ?? ''}`
    if (seriesKeys.has(seriesKey)) return false
    seriesKeys.add(seriesKey)
    let previous = Number.NEGATIVE_INFINITY
    return series.points.every((point) => {
      points++
      if (points > 4096 || !record(point) || Object.keys(point).sort().join() !== 'timestamp,value' || !timestamp(point.timestamp) || !finite(point.value)) return false
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
  for (const key of ['request_id','event_id','event_type','method','route','error_code','reason','operation','resource','stage','result']) if (!optionalString(value[key])) return false
  for (const key of ['user_id','post_id','comment_id','notification_id','outbox_id','status','duration_ms','response_bytes','attempt','batch_size','document_count']) if (!optionalInteger(value[key])) return false
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
  if (value.source !== 'monitor' || value.severity !== eventSeverities[name] || value.message !== eventMessages[name] || m.plugin_id !== 'redis-exporter' || ![...metadataKeys].filter((key) => key !== 'plugin_id').every((key) => optionalString(m[key]))) return false
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
  params.set('from', from.toISOString()); params.set('to', to.toISOString()); params.set('limit', '50')
  for (const [key, value] of Object.entries(filters)) if (key !== 'range' && value.trim()) params.set(key, value.trim())
  return params.toString()
}
export const observabilityApi = {
  metrics: (metric: MetricName, range: QueryRange, signal?: AbortSignal) => requestValidatedData<MetricResult>(`/observability/metrics?${new URLSearchParams({ metric, range })}`, isMetricResult, { signal }),
  logs: (filters: LogFilters, cursor?: string, signal?: AbortSignal): Promise<Page<LogEntry>> => requestValidatedPage(`/observability/logs?${pageQuery(filters as unknown as Record<string,string>, cursor)}`, isLogEntry, { signal }),
  events: (filters: EventFilters, cursor?: string, signal?: AbortSignal): Promise<Page<EventEntry>> => requestValidatedPage(`/observability/events?${pageQuery(filters as unknown as Record<string,string>, cursor)}`, isEventEntry, { signal }),
}
