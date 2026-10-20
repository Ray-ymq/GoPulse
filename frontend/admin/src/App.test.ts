import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import App from './App.vue'
import { bindAuthNavigation, useAuth } from './composables/useAuth'
import { requestData } from './services/http'
const user = { id: 1, username: 'admin', role: 'super_admin', created_at: '2026-09-12T00:00:00Z' }
afterEach(() => vi.unstubAllGlobals())
it('does not mount before bootstrap; any 403 unmounts data before social navigation', async () => {
  let resolve!: (response: Response) => void
  vi.stubGlobal('fetch', vi.fn().mockReturnValueOnce(new Promise<Response>(r => { resolve = r })).mockResolvedValueOnce(new Response('invalid', { status: 403 })))
  const navigate = vi.fn(() => expect(wrapper.text()).not.toContain('private data'))
  bindAuthNavigation(navigate)
  const wrapper = mount(App, { global: { stubs: { RouterView: { template: '<div>private data</div>' } } } })
  expect(wrapper.text()).not.toContain('private data')
  resolve(new Response(JSON.stringify({ data: user })))
  await flushPromises()
  expect(wrapper.text()).toContain('private data')
  await expect(requestData('/exporter-plugins')).rejects.toThrow()
  await nextTick()
  expect(useAuth().user.value?.role).toBe('user')
  expect(navigate).toHaveBeenCalledWith('/posts')
  wrapper.unmount()
})
