import type { ExporterErrorCode, ExporterSafeError, ExporterStatus } from '../types/exporter'
import { requestValidatedData } from './http'

export const MAX_EXPORTER_PACKAGE_BYTES = 64 * 1024 * 1024
const observedStates = new Set(['installing', 'starting', 'running', 'stopping', 'stopped', 'updating', 'failed'])
const safeMessages: Record<ExporterErrorCode, readonly string[]> = {
  start_failed: ['plugin failed to start'],
  stop_failed: ['plugin failed to stop', 'plugin process ownership could not be verified'],
  update_failed: ['plugin update failed and was rolled back'],
  rollback_failed: ['plugin update rollback requires repair', 'plugin update rollback could not restart the previous version'],
  recovery_invalid: ['plugin installation requires repair'],
  recovery_failed: ['plugin failed to recover'],
  process_exited: ['plugin process exited unexpectedly'],
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function exactKeys(value: Record<string, unknown>, required: string[], optional: string[] = []): boolean {
  const keys = Object.keys(value)
  return required.every((key) => key in value) && keys.every((key) => required.includes(key) || optional.includes(key))
}
function isUTC(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) && Number.isFinite(Date.parse(value))
}
function isOptionalUTC(value: unknown): value is string | null { return value === null || isUTC(value) }
function isSafeText(value: unknown, max: number): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= max && value.trim() === value && !/[\u0000-\u001f\u007f]/.test(value)
}
function isSafeError(value: unknown): value is ExporterSafeError {
  if (!isRecord(value) || !exactKeys(value, ['code', 'message', 'at']) || typeof value.code !== 'string' || !(value.code in safeMessages)) return false
  return typeof value.message === 'string' && safeMessages[value.code as ExporterErrorCode].includes(value.message) && isUTC(value.at)
}
export function isExporterStatus(value: unknown): value is ExporterStatus {
  const required = ['id','name','version','kind','source','desired_state','observed_state','installed_at','updated_at','started_at','last_scrape_at','last_success_at']
  if (!isRecord(value) || !exactKeys(value, required, ['last_error'])) return false
  if (value.id !== 'redis-exporter' || !isSafeText(value.name, 80) || typeof value.version !== 'string' || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(value.version) || value.kind !== 'metrics-exporter' || value.source !== 'redis') return false
  if ((value.desired_state !== 'running' && value.desired_state !== 'stopped') || typeof value.observed_state !== 'string' || !observedStates.has(value.observed_state)) return false
  if (!isUTC(value.installed_at) || !isUTC(value.updated_at) || !isOptionalUTC(value.started_at) || !isOptionalUTC(value.last_scrape_at) || !isOptionalUTC(value.last_success_at)) return false
  if ('last_error' in value && !isSafeError(value.last_error)) return false
  const installed = Date.parse(value.installed_at); const updated = Date.parse(value.updated_at)
  if (updated < installed) return false
  for (const stamp of [value.started_at, value.last_scrape_at, value.last_success_at]) if (stamp !== null && Date.parse(stamp) < installed) return false
  if (value.last_success_at !== null && (value.last_scrape_at === null || Date.parse(value.last_success_at) > Date.parse(value.last_scrape_at))) return false
  if ((value.observed_state === 'running' || value.observed_state === 'starting') && value.desired_state !== 'running') return false
  if (value.observed_state === 'stopping' && value.desired_state !== 'stopped') return false
  if (value.observed_state === 'running' && value.started_at === null) return false
  return !('last_error' in value) || Date.parse(value.last_error!.at) >= installed
}
function isExporterList(value: unknown): value is ExporterStatus[] { return Array.isArray(value) && value.length <= 1 && value.every(isExporterStatus) }
function packageBody(file: File): FormData { const form = new FormData(); form.append('package', file); return form }
export function validateExporterPackage(file: File | null): string {
  if (!file) return '请选择一个 Exporter 安装包。'
  if (!file.name.endsWith('.tar.gz')) return '安装包必须使用 .tar.gz 扩展名。'
  if (file.size <= 0) return '安装包不能为空。'
  if (file.size > MAX_EXPORTER_PACKAGE_BYTES) return '安装包不能超过 64 MiB。'
  return ''
}
export const exporterApi = {
  list: (signal?: AbortSignal) => requestValidatedData<ExporterStatus[]>('/exporter-plugins', isExporterList, { signal }),
  get: (signal?: AbortSignal) => requestValidatedData<ExporterStatus>('/exporter-plugins/redis-exporter', isExporterStatus, { signal }),
  start: () => requestValidatedData<ExporterStatus>('/exporter-plugins/redis-exporter/start', isExporterStatus, { method: 'POST' }),
  stop: () => requestValidatedData<ExporterStatus>('/exporter-plugins/redis-exporter/stop', isExporterStatus, { method: 'POST' }),
  install: (file: File) => requestValidatedData<ExporterStatus>('/exporter-plugins/install', isExporterStatus, { method: 'POST', body: packageBody(file) }),
  update: (file: File) => requestValidatedData<ExporterStatus>('/exporter-plugins/redis-exporter/update', isExporterStatus, { method: 'POST', body: packageBody(file) }),
}
