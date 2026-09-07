import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from '../router'
import { resetAuthForTests } from '../composables/useAuth'
import ProfileView from './ProfileView.vue'
const profile = { id: 1, username: 'alice', display_name: 'Alice', bio: 'Hello', created_at: '2026-09-01T00:00:00Z', is_self: true }
afterEach(() => { resetAuthForTests(); vi.unstubAllGlobals() })
it('profile editing validates fields and recovers from a server failure', async () => {
  let fail = true
  const fetcher = vi.fn(async (input: string, init?: RequestInit) => {
    let body: unknown = { data: profile }; let status = 200
    if (input.endsWith('/users/me')) body = { data: { ...profile, role: 'user' } }
    if (input.includes('/posts?')) body = { data: [], meta: { next_cursor: null } }
    if (init?.method === 'PATCH') {
      if (fail) { body = { error: { code: 'internal_error', message: '保存失败' } }; status = 500 }
      else body = { data: { ...profile, display_name: '新名称' } }
    }
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
  })
  vi.stubGlobal('fetch', fetcher)
  const router = createAppRouter(createMemoryHistory()); await router.push('/users/alice')
  const wrapper = mount(ProfileView, { global: { plugins: [router] } }); await flushPromises()
  await wrapper.get('button').trigger('click')
  expect(wrapper.get('input').element.value).toBe('Alice')
  await wrapper.get('input').setValue(' ')
  expect(wrapper.text()).toContain('显示名称需要 1–64')
  await wrapper.get('input').setValue('新名称')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.text()).toContain('保存失败'); expect(wrapper.find('form').exists()).toBe(true)
  fail = false
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.text()).toContain('资料已更新'); expect(wrapper.find('form').exists()).toBe(false)
})
