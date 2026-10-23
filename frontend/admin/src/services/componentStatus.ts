import { requestValidatedData } from './http'

export type ComponentStatus = {
  component: string
  instance: string
  status: 'healthy' | 'unavailable' | 'unknown'
  observed_at: string | null
  reason_code: string
}

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function exact(value: Record<string, unknown>, keys: string[]): boolean {
  const actual = Object.keys(value).sort()
  const expected = [...keys].sort()
  return actual.length === expected.length && actual.every((key, index) => key === expected[index])
}

function status(value: unknown): value is ComponentStatus['status'] {
  return value === 'healthy' || value === 'unavailable' || value === 'unknown'
}

function item(value: unknown): value is ComponentStatus {
  if (!record(value) || !exact(value, ['component', 'instance', 'status', 'observed_at', 'reason_code'])) return false
  return typeof value.component === 'string' && /^[a-z][a-z0-9-]{0,63}$/.test(value.component)
    && typeof value.instance === 'string' && /^[a-zA-Z0-9._-]{1,128}$/.test(value.instance)
    && status(value.status)
    && (value.observed_at === null || typeof value.observed_at === 'string' && Number.isFinite(Date.parse(value.observed_at)))
    && typeof value.reason_code === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value.reason_code)
}

export function parseComponentStatus(value: unknown): value is ComponentStatus[] {
  if (!Array.isArray(value) || value.length > 128 || !value.every(item)) throw new Error('组件状态响应无效')
  return true
}

export function getComponentStatus(signal?: AbortSignal): Promise<ComponentStatus[]> {
  return requestValidatedData('/admin/component-status', parseComponentStatus, { signal })
}
