import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import LogsView from './ObservabilityLogsView.vue'
import { observabilityApi } from '../services/observability'
vi.mock('../services/observability', async original => ({
  ...await original<typeof import('../services/observability')>(),
  observabilityApi: { logs: vi.fn() },
}))
afterEach(() => vi.resetAllMocks())
it('selects safe returned details and clears selection when a refresh fails', async () => {
  vi.mocked(observabilityApi.logs).mockResolvedValueOnce({ data: [{ timestamp:'2026-09-15T12:00:00Z', level:'error', service:'backend', module:'http', message:'request failed', request_id:'a'.repeat(32) }], nextCursor:null }).mockRejectedValueOnce(new Error('private upstream diagnostic'))
  const wrapper = mount(LogsView)
  await vi.waitFor(() => expect(wrapper.find('.row-select').exists()).toBe(true))
  await wrapper.get('.row-select').trigger('click')
  expect(wrapper.get('[aria-label="日志详情"]').text()).toContain('a'.repeat(32))
  await wrapper.get('form').trigger('submit')
  await vi.waitFor(() => {
    expect(observabilityApi.logs).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('查询失败')
    expect(wrapper.find('.detail-panel').exists()).toBe(false)
  })
  expect(wrapper.text()).not.toContain('private upstream diagnostic')
  expect(wrapper.findAll('.record-card')).toHaveLength(1)
  wrapper.unmount()
})
