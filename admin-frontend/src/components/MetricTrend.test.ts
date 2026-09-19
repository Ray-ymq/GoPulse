import { mount } from '@vue/test-utils'
import MetricTrend from './MetricTrend.vue'
it('plots actual time and value proportions, including negative values', () => {
  const wrapper = mount(MetricTrend, { props: { unit: 'count', points: [
    { timestamp: '2026-09-15T00:00:00Z', value: -10 },
    { timestamp: '2026-09-15T00:00:10Z', value: 0 },
    { timestamp: '2026-09-15T00:00:40Z', value: 30 },
  ] } })
  expect(wrapper.get('polyline').attributes('points')).toBe('10,220 255,170 990,20')
  expect(wrapper.get('svg').attributes('aria-label')).toContain('最小 -10，最大 30')
})
it('does not invent a sequence for an empty response', () => {
  const wrapper = mount(MetricTrend, { props: { unit: 'count', points: [] } })
  expect(wrapper.find('svg').exists()).toBe(false)
  expect(wrapper.get('[role="status"]').text()).toBe('暂无趋势采样')
})
