import { requestValidatedData } from './http'
import type {
  BusinessStageBudget,
  BusinessStageConsumer,
  BusinessStageEvidence,
  BusinessStageFailure,
  BusinessStageLogEntry,
  BusinessStageQuality,
  BusinessStageResponse,
  BusinessStageSource,
} from '../types/observability'

export interface BusinessStageContext {
  data_epoch: string
  retry_window_seconds: number
  retry_window_until: string
}

export type BusinessStageSelector =
  | { command_id: string; event_id?: never; post_id?: never }
  | { event_id: string; command_id?: never; post_id?: never }
  | { post_id: number; command_id?: never; event_id?: never }

export interface BusinessStageQuery {
  epoch: string
  selector: BusinessStageSelector
  check_visibility: boolean
  include_logs: boolean
  include_trace: boolean
  from?: string
  to?: string
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const traceID = /^[0-9a-f]{32}$/
const spanID = /^[0-9a-f]{16}$/
const identity = /^[A-Za-z0-9._:/-]{1,255}$/
const qualities = new Set<BusinessStageQuality>([
  'known', 'no_evidence', 'legacy_unknown', 'stale_epoch', 'expired',
  'not_requested', 'not_configured', 'backend_error', 'sampling',
])

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function exact(value: Record<string, unknown>, allowed: string[], required: string[] = []): boolean {
  const keys = Object.keys(value).sort()
  const accepted = [...allowed].sort()
  if (keys.some((key) => !accepted.includes(key))) return false
  return required.every((key) => key in value)
}

function string(value: unknown, pattern?: RegExp): value is string {
  return typeof value === 'string' && (pattern === undefined || pattern.test(value))
}

function optionalString(value: Record<string, unknown>, key: string, pattern?: RegExp): boolean {
  return value[key] === undefined || string(value[key], pattern)
}

function optionalInteger(value: Record<string, unknown>, key: string): boolean {
  return value[key] === undefined || (typeof value[key] === 'number' && Number.isSafeInteger(value[key]) && value[key] >= 0)
}

function timestamp(value: unknown): value is string {
  return typeof value === 'string' && Number.isFinite(Date.parse(value))
}

function validQuality(value: unknown): value is BusinessStageQuality {
  return typeof value === 'string' && qualities.has(value as BusinessStageQuality)
}

function validFailure(value: unknown): value is BusinessStageFailure {
  if (!record(value) || !exact(value, ['attempt_count', 'retry_count', 'dead_count', 'last_reason', 'retry_summary', 'dead_summary'])) return false
  return ['attempt_count', 'retry_count', 'dead_count'].every((key) => optionalInteger(value, key))
    && ['last_reason', 'retry_summary', 'dead_summary'].every((key) => optionalString(value, key))
}

function validLogEntry(value: unknown): value is BusinessStageLogEntry {
  if (!record(value) || !exact(value, ['timestamp', 'level', 'service', 'instance_id', 'module', 'message', 'trace_id', 'span_id', 'attempt_id', 'event_id', 'command_id', 'data_epoch', 'post_id'], ['timestamp'])) return false
  if (!timestamp(value.timestamp) || !['level', 'service', 'instance_id', 'module', 'message', 'attempt_id'].every((key) => optionalString(value, key))) return false
  if (!optionalString(value, 'trace_id', traceID) || !optionalString(value, 'span_id', spanID)) return false
  if (!optionalString(value, 'event_id', uuid) || !optionalString(value, 'command_id', uuid) || !optionalString(value, 'data_epoch', uuid)) return false
  return optionalInteger(value, 'post_id')
}

function validSource(value: unknown, sourceName: string): value is BusinessStageSource {
  if (!record(value) || !exact(value, ['source', 'quality', 'read_at', 'data_epoch', 'command_id', 'event_id', 'consumer', 'routing_contract_revision', 'projection_generation', 'status', 'result', 'reason', 'actual_version', 'matched', 'entries', 'truncated', 'trace_ids', 'sampling'], ['source', 'quality'])) return false
  if (value.source !== sourceName || !validQuality(value.quality)) return false
  if (value.read_at !== undefined && !timestamp(value.read_at)) return false
  if (!optionalString(value, 'data_epoch', uuid) || !optionalString(value, 'command_id', uuid) || !optionalString(value, 'event_id', uuid)) return false
  if (!optionalString(value, 'consumer') || !optionalString(value, 'routing_contract_revision', identity) || !optionalString(value, 'projection_generation', identity)) return false
  if (!['status', 'result', 'reason', 'sampling'].every((key) => optionalString(value, key))) return false
  if (!optionalInteger(value, 'actual_version') || (value.matched !== undefined && typeof value.matched !== 'boolean') || (value.truncated !== undefined && typeof value.truncated !== 'boolean')) return false
  if (value.entries !== undefined && (!Array.isArray(value.entries) || value.entries.length > 100 || !value.entries.every(validLogEntry))) return false
  if (value.trace_ids !== undefined && (!Array.isArray(value.trace_ids) || value.trace_ids.length > 100 || !value.trace_ids.every((id) => string(id, traceID) && !/^0+$/.test(id)))) return false
  return true
}

function validConsumer(value: unknown): value is BusinessStageConsumer {
  if (!record(value) || !exact(value, ['consumer', 'routing_contract_revision', 'applicable_object_version', 'quality', 'status', 'result', 'projection_generation', 'actual_version', 'completed_at', 'retain_until', 'failure'], ['consumer', 'routing_contract_revision', 'quality'])) return false
  if (value.consumer !== 'search' || !string(value.routing_contract_revision, identity) || value.routing_contract_revision !== 'post-created-search/1' || !validQuality(value.quality)) return false
  if (!optionalInteger(value, 'applicable_object_version') || !optionalInteger(value, 'actual_version') || !optionalString(value, 'status') || !optionalString(value, 'result') || !optionalString(value, 'projection_generation', identity)) return false
  if ((value.completed_at !== undefined && !timestamp(value.completed_at)) || (value.retain_until !== undefined && !timestamp(value.retain_until))) return false
  return value.failure === undefined || validFailure(value.failure)
}

function validEvidence(value: unknown): value is BusinessStageEvidence {
  if (!record(value) || !exact(value, ['command', 'broker', 'consumers', 'visibility', 'logs', 'trace'], ['command', 'broker', 'visibility', 'logs', 'trace'])) return false
  if (!validSource(value.command, 'command') || !validSource(value.broker, 'broker') || !validSource(value.visibility, 'search_visibility') || !validSource(value.logs, 'logs') || !validSource(value.trace, 'trace')) return false
  return value.consumers === undefined || (Array.isArray(value.consumers) && value.consumers.length <= 20 && value.consumers.every(validConsumer))
}

function validBudget(value: unknown): value is BusinessStageBudget {
  if (!record(value) || !exact(value, ['current_open_requirements', 'current_open_limit', 'retained_groups', 'retained_groups_limit', 'retained_bytes', 'retained_bytes_limit', 'at_eighty_percent'], ['current_open_requirements', 'current_open_limit', 'retained_groups', 'retained_groups_limit', 'retained_bytes', 'retained_bytes_limit', 'at_eighty_percent'])) return false
  return ['current_open_requirements', 'current_open_limit', 'retained_groups', 'retained_groups_limit', 'retained_bytes', 'retained_bytes_limit'].every((key) => typeof value[key] === 'number' && Number.isSafeInteger(value[key]) && value[key] >= 0)
    && typeof value.at_eighty_percent === 'boolean'
}

export function isBusinessStageContext(value: unknown): value is BusinessStageContext {
  if (!record(value) || !exact(value, ['data_epoch', 'retry_window_seconds', 'retry_window_until'], ['data_epoch', 'retry_window_seconds', 'retry_window_until'])) return false
  return string(value.data_epoch, uuid) && typeof value.retry_window_seconds === 'number' && Number.isSafeInteger(value.retry_window_seconds) && value.retry_window_seconds > 0 && timestamp(value.retry_window_until)
}

export function isBusinessStageResponse(value: unknown): value is BusinessStageResponse {
  if (!record(value) || !exact(value, ['requested_epoch', 'current_epoch', 'resolved', 'routing_contract_revision', 'evidence', 'budget'], ['requested_epoch', 'current_epoch', 'resolved', 'evidence', 'budget'])) return false
  if (!string(value.requested_epoch, uuid) || !string(value.current_epoch, uuid) || !record(value.resolved) || !exact(value.resolved, ['command_id', 'event_id', 'post_id'])) return false
  if (!optionalString(value.resolved, 'command_id', uuid) || !optionalString(value.resolved, 'event_id', uuid) || !optionalInteger(value.resolved, 'post_id')) return false
  if (value.routing_contract_revision !== undefined && value.routing_contract_revision !== 'post-created-search/1') return false
  return validEvidence(value.evidence) && validBudget(value.budget)
}

export function validateBusinessStageQuery(query: BusinessStageQuery): void {
  if (!uuid.test(query.epoch)) throw new Error('epoch is invalid')
  const selector = query.selector
  const selectors = Object.keys(selector).filter((key) => selector[key as keyof BusinessStageSelector] !== undefined)
  if (selectors.length !== 1) throw new Error('exactly one business selector is required')
  if ('command_id' in selector) {
    const commandID = selector.command_id
    if (typeof commandID !== 'string' || !uuid.test(commandID)) throw new Error('command_id is invalid')
  }
  if ('event_id' in selector) {
    const eventID = selector.event_id
    if (typeof eventID !== 'string' || !uuid.test(eventID)) throw new Error('event_id is invalid')
  }
  if ('post_id' in selector) {
    const postID = selector.post_id
    if (typeof postID !== 'number' || !Number.isSafeInteger(postID) || postID <= 0) throw new Error('post_id is invalid')
  }
  if (query.include_logs || query.include_trace) {
    if (!query.from || !query.to || !timestamp(query.from) || !timestamp(query.to) || Date.parse(query.from) >= Date.parse(query.to) || Date.parse(query.to) - Date.parse(query.from) > 24 * 60 * 60 * 1000) throw new Error('evidence window is invalid')
  }
}

function queryPath(query: BusinessStageQuery): string {
  validateBusinessStageQuery(query)
  const values = new URLSearchParams({ epoch: query.epoch, check_visibility: String(query.check_visibility) })
  for (const [key, value] of Object.entries(query.selector)) if (value !== undefined) values.set(key, String(value))
  const includes = [query.include_logs ? 'logs' : '', query.include_trace ? 'trace' : ''].filter(Boolean)
  if (includes.length) {
    values.set('include', includes.join(','))
    values.set('from', query.from!)
    values.set('to', query.to!)
  }
  return `/observability/business-stages?${values.toString()}`
}

export const businessStagesApi = {
  context: (signal?: AbortSignal) => requestValidatedData<BusinessStageContext>('/post-commands/context', isBusinessStageContext, { signal }),
  query: (query: BusinessStageQuery, signal?: AbortSignal) => requestValidatedData<BusinessStageResponse>(queryPath(query), isBusinessStageResponse, { signal }),
}
