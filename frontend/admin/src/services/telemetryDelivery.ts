import { requestValidatedData } from './http'

export type DeliveryQueue = {
  kind: 'metrics' | 'logs' | 'events'
  pending: number
  occupied_bytes: number
  capacity_bytes: number
  file_bytes: number
  enqueued: number
  sent: number
  failed: number
  evicted: number
  write_blocked: boolean
  reason_code: string
}

export type TelemetryDeliveryItem = {
  component: string
  instance: string
  status: 'healthy' | 'degraded' | 'unknown'
  observed_at: string | null
  reason_code: string
  queues: DeliveryQueue[]
  write_blocked: boolean
  discarded: number
}

export type TelemetryDelivery = {
  generated_at: string
  status: 'healthy' | 'degraded' | 'unknown'
  items: TelemetryDeliveryItem[]
}

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function exact(value: Record<string, unknown>, keys: string[]): boolean {
  const actual = Object.keys(value).sort()
  const expected = [...keys].sort()
  return actual.length === expected.length && actual.every((key, index) => key === expected[index])
}

function uint(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function queue(value: unknown): value is DeliveryQueue {
  if (!record(value) || !exact(value, ['kind', 'pending', 'occupied_bytes', 'capacity_bytes', 'file_bytes', 'enqueued', 'sent', 'failed', 'evicted', 'write_blocked', 'reason_code'])) return false
  return (value.kind === 'metrics' || value.kind === 'logs' || value.kind === 'events')
    && typeof value.pending === 'number' && Number.isSafeInteger(value.pending) && value.pending >= 0
    && uint(value.occupied_bytes) && uint(value.capacity_bytes) && uint(value.file_bytes)
    && uint(value.enqueued) && uint(value.sent) && uint(value.failed) && uint(value.evicted)
    && typeof value.write_blocked === 'boolean'
    && typeof value.reason_code === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value.reason_code)
}

function item(value: unknown): value is TelemetryDeliveryItem {
  if (!record(value) || !exact(value, ['component', 'instance', 'status', 'observed_at', 'reason_code', 'queues', 'write_blocked', 'discarded'])) return false
  return typeof value.component === 'string' && /^[a-z][a-z0-9-]{0,63}$/.test(value.component)
    && typeof value.instance === 'string' && /^[a-zA-Z0-9._-]{1,128}$/.test(value.instance)
    && (value.status === 'healthy' || value.status === 'degraded' || value.status === 'unknown')
    && (value.observed_at === null || typeof value.observed_at === 'string' && Number.isFinite(Date.parse(value.observed_at)))
    && typeof value.reason_code === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value.reason_code)
    && Array.isArray(value.queues) && value.queues.length <= 3 && value.queues.every(queue)
    && typeof value.write_blocked === 'boolean' && uint(value.discarded)
}

export function parseTelemetryDelivery(value: unknown): value is TelemetryDelivery {
  if (!record(value) || !exact(value, ['generated_at', 'status', 'items']) || typeof value.generated_at !== 'string' || !Number.isFinite(Date.parse(value.generated_at)) || !['healthy', 'degraded', 'unknown'].includes(String(value.status)) || !Array.isArray(value.items) || value.items.length > 16 || !value.items.every(item)) throw new Error('数据传输响应无效')
  return true
}

export function getTelemetryDelivery(signal?: AbortSignal): Promise<TelemetryDelivery> {
  return requestValidatedData('/admin/telemetry-delivery', parseTelemetryDelivery, { signal })
}
