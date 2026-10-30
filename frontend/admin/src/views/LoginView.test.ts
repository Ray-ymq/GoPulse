import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import LoginView from './LoginView.vue'
import { authApi } from '../services/api'
import { ApiError } from '../services/http'

vi.mock('../services/api', () => ({ authApi: { login: vi.fn(), register: vi.fn() } }))
afterEach(() => vi.clearAllMocks())

it('registers an account without granting management access', async () => {
  vi.mocked(authApi.register).mockResolvedValue({ id: 9, username: 'reader', role: 'user', created_at: '2026-10-30T00:00:00Z' })
  const wrapper = mount(LoginView)
  await wrapper.find('button[type="button"]').trigger('click')
  await wrapper.find('input[name="username"]').setValue('reader')
  await wrapper.find('input[name="password"]').setValue('example-password')
  await wrapper.find('form').trigger('submit')
  await flushPromises()
  expect(authApi.register).toHaveBeenCalledWith('reader', 'example-password')
  expect(wrapper.text()).toContain('请联系管理员授予管理权限')
  expect(wrapper.find('input[name="password"]').element).toHaveProperty('value', '')
  wrapper.unmount()
})

it('shows authentication failure and allows retry', async () => {
  vi.mocked(authApi.login).mockRejectedValue(new ApiError('invalid_credentials', '用户名或密码错误。', 401))
  const wrapper = mount(LoginView)
  await wrapper.find('input[name="username"]').setValue('admin')
  await wrapper.find('input[name="password"]').setValue('example-password')
  await wrapper.find('form').trigger('submit')
  await flushPromises()
  expect(wrapper.text()).toContain('用户名或密码错误。')
  expect(wrapper.find('button[type="submit"]').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
