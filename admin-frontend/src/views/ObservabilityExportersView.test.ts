import { flushPromises, mount } from '@vue/test-utils'
import ObservabilityExportersView from './ObservabilityExportersView.vue'
import { exporterApi, pluginConfigApi } from '../services/exporters'
import type { PluginCatalogItem } from '../services/exporters'
import type { ExporterStatus } from '../types/exporter'

vi.mock('../services/exporters', async importOriginal => ({
  ...await importOriginal<typeof import('../services/exporters')>(),
  exporterApi: { list: vi.fn(), update: vi.fn() },
  pluginConfigApi: { catalog: vi.fn() },
}))
afterEach(() => { vi.resetAllMocks(); vi.restoreAllMocks() })

it.each([false, true])('refreshes the catalog after v2 update (refresh failure: %s)', async refreshFails => {
  const old: ExporterStatus = {
    id: 'redis-exporter', name: 'GoPulse redis Exporter', version: '1.10.6',
    kind: 'metrics-exporter', source: 'redis', desired_state: 'running', observed_state: 'running',
    installed_at: '2026-09-12T00:00:00Z', updated_at: '2026-09-12T00:00:00Z',
    started_at: null, last_scrape_at: null, last_success_at: null,
  }
  const descriptor: PluginCatalogItem = {
    id: old.id, name: old.name, source: old.source, available: true, configured: true,
    secret_configured: true, revision: 'a'.repeat(32), summary: 'upgrade_required',
    schema: { schema_version: 1, plugin_id: old.id, fields: [] },
  }
  vi.mocked(exporterApi.list).mockResolvedValue([old])
  vi.mocked(exporterApi.update).mockResolvedValue({ ...old, version: '1.11.6' })
  const catalog = vi.mocked(pluginConfigApi.catalog).mockResolvedValueOnce([descriptor])
  if (refreshFails) catalog.mockRejectedValueOnce(new Error('offline'))
  else catalog.mockResolvedValueOnce([{ ...descriptor, revision: 'b'.repeat(32), summary: 'configured' }])
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  const wrapper = mount(ObservabilityExportersView, { global: { stubs: { RouterLink: true } } })
  try {
    await flushPromises()
    const save = () => wrapper.findAll('button').find(button => button.text() === '替换配置')!
    expect(save().attributes('disabled')).toBeDefined()
    const input = wrapper.get('input[type="file"]')
    const file = new File(['package'], 'redis.tar.gz', { type: 'application/gzip' })
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await wrapper.findAll('button').find(button => button.text() === '确认更新')!.trigger('click')
    await flushPromises()
    expect(exporterApi.update).toHaveBeenCalledExactlyOnceWith(file, old.id)
    expect(pluginConfigApi.catalog).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('v1.11.6')
    if (refreshFails) {
      expect(wrapper.get('[role="status"]').text()).toContain('更新已成功，但插件目录刷新失败')
      expect(save().attributes('disabled')).toBeDefined()
    } else {
      expect(save().attributes('disabled')).toBeUndefined()
      expect(wrapper.text()).not.toContain('旧版包保持原状态')
    }
    expect((input.element as HTMLInputElement).value).toBe('')
  } finally { wrapper.unmount() }
})
