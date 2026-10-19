import { requestValidatedData } from './http'
export const selfMonitorStates = ['stopped', 'starting', 'running', 'stopping', 'degraded', 'failed', 'unavailable'] as const
export type SelfMonitorState = typeof selfMonitorStates[number]
export interface SelfMonitorStatus {
  state: SelfMonitorState
  reason: string
  grafana_url: '/grafana/'
  targets: { job: string; instance: string; up: boolean }[]
}
function valid(value: unknown): value is SelfMonitorStatus {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const v = value as Record<string, unknown>
  return selfMonitorStates.some(state => state === v.state)
    && typeof v.reason === 'string' && v.reason.length <= 80
    && v.grafana_url === '/grafana/' && Array.isArray(v.targets) && v.targets.length <= 64
    && v.targets.every(target => target && typeof target === 'object' && typeof target.job === 'string' && typeof target.instance === 'string' && typeof target.up === 'boolean')
}
export function getSelfMonitor(signal?: AbortSignal) {
  return requestValidatedData('/admin/self-monitor', valid, { signal })
}
export function setSelfMonitor(action: 'start' | 'stop', signal?: AbortSignal) {
  return requestValidatedData(`/admin/self-monitor/${action}`, valid, { method: 'POST', signal })
}
