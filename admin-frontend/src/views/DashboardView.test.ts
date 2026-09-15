import { flushPromises, mount } from '@vue/test-utils'
import DashboardView from './DashboardView.vue'
import { observabilityApi } from '../services/observability'
import { getOverview, sectionNames, type Overview } from '../services/overview'
vi.mock('../services/overview', async importOriginal => ({
  ...await importOriginal<typeof import('../services/overview')>(), getOverview: vi.fn(),
}))
vi.mock('../services/observability', () => ({ observabilityApi: { metrics: vi.fn() } }))
beforeEach(() => vi.mocked(observabilityApi.metrics).mockRejectedValue(new Error('unavailable')))
afterEach(() => vi.resetAllMocks())
it.each(['healthy', 'unavailable'])('derives summaries without treating %s missing data as zero', async status => {
  const sections = Object.fromEntries(sectionNames.map(name => [name, {
    status, reason_code: status === 'healthy' ? 'ok' : 'upstream_unavailable', observed_at: null,
    items: name === 'components' ? [{ id: 'backend', status: 'healthy' }] : [],
  }]))
  vi.mocked(getOverview).mockResolvedValue({ generated_at: '2026-09-15T00:00:00Z', status, sections } as Overview)
  const wrapper = mount(DashboardView, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  expect(wrapper.findAll('[data-section]')).toHaveLength(6)
  const values = wrapper.findAll('.summary-grid strong').map(node => node.text())
  expect(values).toEqual(status === 'healthy' ? ['1 / 1', '未知', '未知', '未知'] : ['未知', '未知', '未知', '未知'])
  wrapper.unmount()
})

it('renders real trend samples while preserving a failed independent chart as unavailable', async () => {
  const sections = Object.fromEntries(sectionNames.map(name => [name, { status:'healthy', reason_code:'ok', observed_at:null, items:[] }]))
  vi.mocked(getOverview).mockResolvedValue({ generated_at:'2026-09-15T00:00:00Z',status:'healthy',sections } as Overview)
  vi.mocked(observabilityApi.metrics).mockResolvedValueOnce({ metric:'gopulse_backend_outbox_pending',kind:'gauge',unit:'count',range:'15m',from:'2026-09-15T00:00:00Z',to:'2026-09-15T00:15:00Z',step_seconds:15,series:[{labels:{},points:[{timestamp:'2026-09-15T00:00:00Z',value:7},{timestamp:'2026-09-15T00:00:15Z',value:9}]}] })
  const wrapper=mount(DashboardView,{global:{stubs:{RouterLink:true}}})
  await flushPromises()
  expect(wrapper.findAll('.metric-chart')).toHaveLength(1)
  expect(wrapper.find('.metric-chart').attributes('aria-label')).toContain('2 个采样点')
  expect(wrapper.text()).toContain('趋势暂不可用')
  expect(wrapper.findAll('[data-section]')).toHaveLength(6)
  wrapper.unmount()
})
