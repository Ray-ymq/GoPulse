import { createMemoryHistory, createRouter } from 'vue-router'
import { mount } from '@vue/test-utils'
import MetricsView from './ObservabilityMetricsView.vue'
import { loadMetricCatalog, observabilityApi } from '../services/observability'

vi.mock('../services/observability', async importOriginal => ({
  ...await importOriginal<typeof import('../services/observability')>(),
  loadMetricCatalog: vi.fn(),
  observabilityApi: { metrics: vi.fn() },
}))

async function mountMetrics() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/metrics', component: MetricsView }] })
  await router.push('/metrics')
  await router.isReady()
  return mount(MetricsView, { global: { plugins: [router] } })
}

beforeEach(() => {
  vi.mocked(loadMetricCatalog).mockResolvedValue([{
    metric: 'gopulse_redis_up', kind: 'gauge', unit: 'boolean', source: 'redis', target_id: 'redis-exporter-local',
    producer_kind: 'exporter_plugin', producer_id: 'redis-exporter',
  }])
  vi.mocked(observabilityApi.metrics).mockRejectedValue({ code: 'upstream_unavailable', status: 503 })
})

afterEach(() => vi.resetAllMocks())

it('shows the VictoriaMetrics notice when the proxy preserves only a 503 status', async () => {
  const wrapper = await mountMetrics()
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('指标存储或查询服务暂时不可用（VictoriaMetrics）'))
  } finally {
    wrapper.unmount()
  }
})

it('keeps the latest metrics failure visible after a request is superseded', async () => {
  let rejectFirst!: (reason: unknown) => void
  const first = new Promise<never>((_, reject) => { rejectFirst = reject })
  vi.mocked(observabilityApi.metrics).mockReturnValueOnce(first).mockRejectedValueOnce({ status: 503 })
  const wrapper = await mountMetrics()
  try {
    await vi.waitFor(() => expect(observabilityApi.metrics).toHaveBeenCalledTimes(1))
    await wrapper.get('form').trigger('submit')
    rejectFirst(new DOMException('aborted', 'AbortError'))
    await vi.waitFor(() => expect(wrapper.text()).toContain('指标存储或查询服务暂时不可用（VictoriaMetrics）'))
  } finally {
    wrapper.unmount()
  }
})
