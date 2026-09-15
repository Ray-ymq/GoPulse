import { flushPromises, mount } from '@vue/test-utils'
import DashboardView from './DashboardView.vue'
import { getOverview, sectionNames, type Overview } from '../services/overview'
vi.mock('../services/overview', async importOriginal => ({
  ...await importOriginal<typeof import('../services/overview')>(), getOverview: vi.fn(),
}))
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
