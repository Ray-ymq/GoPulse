import { afterEach, describe, expect, it, vi } from 'vitest'
import { businessStagesApi, isBusinessStageResponse } from './businessStages'

const epoch = '123e4567-e89b-12d3-a456-426614174000'
const command = '123e4567-e89b-12d3-a456-426614174001'
const event = '123e4567-e89b-12d3-a456-426614174002'

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function stage() {
  const source = (name: string, quality = 'known') => ({ source: name, quality, read_at: '2026-10-12T10:00:00Z', data_epoch: epoch })
  return {
    requested_epoch: epoch,
    current_epoch: epoch,
    resolved: { command_id: command, event_id: event, post_id: 7 },
    routing_contract_revision: 'post-created-search/1',
    evidence: {
      command: { ...source('command'), command_id: command, event_id: event, status: 'committed' },
      broker: { ...source('broker'), event_id: event, status: 'published', result: 'published' },
      consumers: [{ consumer: 'search', routing_contract_revision: 'post-created-search/1', quality: 'known', status: 'completed', result: 'applied', projection_generation: 'gopulse-post-search-v1' }],
      visibility: { ...source('search_visibility'), matched: true, projection_generation: 'gopulse-post-search-v1', actual_version: 1 },
      logs: { ...source('logs'), entries: [{ timestamp: '2026-10-12T09:59:00Z', service: 'backend', module: 'post', message: 'post committed', command_id: command, data_epoch: epoch, post_id: 7 }] },
      trace: { ...source('trace'), trace_ids: ['0102030405060708090a0b0c0d0e0f10'] },
    },
    budget: { current_open_requirements: 1, current_open_limit: 10000, retained_groups: 1, retained_groups_limit: 100000, retained_bytes: 1024, retained_bytes_limit: 1073741824, at_eighty_percent: false },
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('business stage DTO boundary', () => {
  it('accepts the bounded evidence response and rejects unknown sensitive fields', () => {
    expect(isBusinessStageResponse(stage())).toBe(true)
    expect(isBusinessStageResponse({ ...stage(), evidence: { ...stage().evidence, command: { ...stage().evidence.command, request_digest: 'secret' } } })).toBe(false)
  })

  it('accepts optional retention growth and horizon while rejecting invalid estimates', () => {
    const current = stage()
    const budget = { ...current.budget, recent_groups_per_second: 1.5, recent_bytes_per_second: 1024, estimated_seconds_until_full: 3600 }
    expect(isBusinessStageResponse({ ...current, budget })).toBe(true)
    expect(isBusinessStageResponse({ ...current, budget: { ...budget, estimated_seconds_until_full: null } })).toBe(true)
    expect(isBusinessStageResponse({ ...current, budget: { ...budget, recent_groups_per_second: -1 } })).toBe(false)
    expect(isBusinessStageResponse({ ...current, budget: { ...budget, estimated_seconds_until_full: Infinity } })).toBe(false)
  })

  it('encodes exactly one typed selector and a bounded evidence window', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ data: stage() }))
    vi.stubGlobal('fetch', fetchMock)
    await businessStagesApi.query({ epoch, selector: { command_id: command }, check_visibility: true, include_logs: true, include_trace: true, from: '2026-10-12T09:45:00Z', to: '2026-10-12T10:00:00Z' })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/observability/business-stages?epoch=123e4567-e89b-12d3-a456-426614174000&check_visibility=true&command_id=123e4567-e89b-12d3-a456-426614174001&include=logs%2Ctrace&from=2026-10-12T09%3A45%3A00Z&to=2026-10-12T10%3A00%3A00Z')
  })

  it('does not allow a multi-selector query or an overlong window', async () => {
    expect(() => businessStagesApi.query({ epoch, selector: { command_id: command, event_id: event } as never, check_visibility: true, include_logs: false, include_trace: false })).toThrow('exactly one')
    expect(() => businessStagesApi.query({ epoch, selector: { post_id: 7 }, check_visibility: true, include_logs: true, include_trace: false, from: '2026-10-10T09:00:00Z', to: '2026-10-12T10:00:00Z' })).toThrow('evidence window')
  })
})
