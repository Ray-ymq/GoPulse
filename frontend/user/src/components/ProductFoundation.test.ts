import { mount } from '@vue/test-utils'
import ProductIcon from '../../../shared/ProductIcon.vue'
import ProductState from '../../../shared/ProductState.vue'
import ProductStatus from '../../../shared/ProductStatus.vue'
import { productStatusLabel, productStatusTone } from '../../../shared/status'

it('renders shared icons as decorative or named accessible graphics', () => {
  const decorative = mount(ProductIcon, { props: { name: 'home' } })
  expect(decorative.get('svg').attributes('aria-hidden')).toBe('true')

  const named = mount(ProductIcon, { props: { name: 'close', label: '关闭详情' } })
  expect(named.get('svg').attributes('aria-label')).toBe('关闭详情')
  expect(named.get('title').text()).toBe('关闭详情')
})

it('keeps shared state feedback actionable and live for assistive technology', async () => {
  const wrapper = mount(ProductState, { props: { state: 'unavailable', message: '查询失败。', actionLabel: '重试' } })
  expect(wrapper.get('[role="alert"]').text()).toContain('查询失败。')
  await wrapper.get('button').trigger('click')
  expect(wrapper.emitted('retry')).toHaveLength(1)
})

it('maps internal status values to product language and semantic tone', () => {
  expect(productStatusLabel('running')).toBe('运行中')
  expect(productStatusTone('failed')).toBe('danger')
  expect(mount(ProductStatus, { props: { status: 'stale' } }).text()).toBe('数据已过期')
})
