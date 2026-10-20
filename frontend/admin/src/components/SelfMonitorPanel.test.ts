import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SelfMonitorPanel from './SelfMonitorPanel.vue'
import { getSelfMonitor, setSelfMonitor } from '../services/selfMonitor'
vi.mock('../services/selfMonitor', () => ({ getSelfMonitor: vi.fn(), setSelfMonitor: vi.fn() }))
const snapshot = (state: 'stopped' | 'starting' | 'running' | 'degraded' | 'unavailable') => ({ state, reason: state, grafana_port: 23000 as const, targets: [] })
afterEach(() => { vi.clearAllMocks(); vi.useRealTimers() })
describe('independent self monitoring', () => {
  it('starts once, polls real status, and never stops on unmount', async () => {
    vi.useFakeTimers()
    vi.mocked(getSelfMonitor).mockResolvedValueOnce(snapshot('stopped')).mockResolvedValue(snapshot('running'))
    vi.mocked(setSelfMonitor).mockResolvedValue(snapshot('starting'))
    const wrapper = mount(SelfMonitorPanel)
    await flushPromises()
    await wrapper.findAll('button')[0]!.trigger('click'); await flushPromises()
    expect(setSelfMonitor).toHaveBeenCalledTimes(1)
    expect(wrapper.findAll('button')[0]!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('启动中')
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(wrapper.find('a').attributes('href')).toBe(`http://${window.location.hostname}:23000/`)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(setSelfMonitor).toHaveBeenCalledTimes(1)
    expect(getSelfMonitor).toHaveBeenCalledTimes(2)
  })
  it('shows partial failure and permits stop without claiming zero samples', async () => {
    vi.mocked(getSelfMonitor).mockResolvedValue(snapshot('degraded'))
    const wrapper = mount(SelfMonitorPanel); await flushPromises()
    expect(wrapper.text()).toContain('部分不可达')
    expect(wrapper.findAll('button')[1]!.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it('clears stale running status after a failed refresh and supports retry', async () => {
    vi.mocked(getSelfMonitor).mockResolvedValueOnce(snapshot('running')).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(snapshot('stopped'))
    const wrapper = mount(SelfMonitorPanel); await flushPromises()
    await wrapper.findAll('button')[2]!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('状态读取失败')
    expect(wrapper.find('a').exists()).toBe(false)
    await wrapper.findAll('button')[2]!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('已停止')
    wrapper.unmount()
  })
})
