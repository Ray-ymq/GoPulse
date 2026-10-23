import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import BusinessStagesView from './BusinessStagesView.vue'
import { businessStagesApi } from '../services/businessStages'
import type { BusinessStageResponse } from '../types/observability'

vi.mock('../services/businessStages', async original => ({
  ...await original<typeof import('../services/businessStages')>(),
  businessStagesApi: { context: vi.fn(), query: vi.fn() },
}))

const epoch = '123e4567-e89b-42d3-a456-426614174000'
const firstCommand = '123e4567-e89b-42d3-a456-426614174001'
const secondCommand = '123e4567-e89b-42d3-a456-426614174002'

function response(commandID: string): BusinessStageResponse {
  const source = (name: string, quality: 'known' | 'no_evidence' = 'known') => ({ source: name, quality, read_at: '2026-10-12T10:00:00Z', data_epoch: epoch })
  return {
    requested_epoch: epoch,
    current_epoch: epoch,
    resolved: { command_id: commandID, event_id: '123e4567-e89b-42d3-a456-426614174003', post_id: 7 },
    routing_contract_revision: 'post-created-search/1',
    evidence: {
      command: { ...source('command'), command_id: commandID, status: 'committed' },
      broker: { ...source('broker'), status: 'published' },
      consumers: [{ consumer: 'search', routing_contract_revision: 'post-created-search/1', quality: 'known', status: 'completed', projection_generation: 'gopulse-post-search-v1', result: 'applied' }],
      visibility: { ...source('search_visibility'), matched: true, projection_generation: 'gopulse-post-search-v1', actual_version: 1 },
      logs: { ...source('logs'), entries: [] },
      trace: { ...source('trace', 'no_evidence'), trace_ids: [] },
    },
    budget: { current_open_requirements: 1, current_open_limit: 100, retained_groups: 1, retained_groups_limit: 100, retained_bytes: 1, retained_bytes_limit: 1000, at_eighty_percent: false },
  }
}

afterEach(() => vi.resetAllMocks())

it('ignores a late response from an aborted query', async () => {
  vi.mocked(businessStagesApi.context).mockResolvedValue({ data_epoch: epoch, retry_window_seconds: 60, retry_window_until: '2026-10-12T10:01:00Z' })
  let resolveFirst: ((value: BusinessStageResponse) => void) | undefined
  vi.mocked(businessStagesApi.query)
    .mockImplementationOnce((_query, _signal) => new Promise(resolve => { resolveFirst = resolve }))
    .mockResolvedValueOnce(response(secondCommand))
  const wrapper = mount(BusinessStagesView, { global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } })
	await vi.waitFor(() => expect((wrapper.get('[aria-label="Data epoch"]').element as HTMLInputElement).value).toBe(epoch))
  await wrapper.get('[aria-label="Command ID"]').setValue(firstCommand)
  await wrapper.get('form').trigger('submit')
  await wrapper.get('[aria-label="Command ID"]').setValue(secondCommand)
  await wrapper.get('form').trigger('submit')
  await vi.waitFor(() => expect(wrapper.text()).toContain(secondCommand))
  resolveFirst?.(response(firstCommand))
  await new Promise(resolve => setTimeout(resolve, 0))
  expect(wrapper.text()).toContain(secondCommand)
  expect(wrapper.text()).not.toContain(`Command ID ${firstCommand}`)
  wrapper.unmount()
})
