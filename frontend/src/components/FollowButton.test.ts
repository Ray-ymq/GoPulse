import { flushPromises, mount } from '@vue/test-utils'
import { useAuth, resetAuthForTests } from '../composables/useAuth'
import { clearFollowing } from '../composables/useFollowing'
import FollowButton from './FollowButton.vue'

afterEach(() => { resetAuthForTests(); clearFollowing(); vi.unstubAllGlobals() })
it('follow controls share optimistic state and roll back a failed unfollow', async () => {
  let fail = false
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    const me = input.endsWith('/users/me')
    return new Response(JSON.stringify(me ? { data: { id: 1, username: 'alice', role: 'user', created_at: '2026-09-07T00:00:00Z' } } : fail ? { error: { code: 'internal_error', message: 'failed' } } : { data: { following: true } }), { status: !me && fail ? 500 : 200, headers: { 'Content-Type': 'application/json' } })
  }))
  await useAuth().initialize()
  const props = { target: { id: 2, username: 'bob', following: false } }
  const first = mount(FollowButton, { props }); const second = mount(FollowButton, { props })
  await first.get('button').trigger('click'); await flushPromises()
  expect(second.get('button').attributes('aria-pressed')).toBe('true')
  fail = true
  await second.get('button').trigger('click'); await flushPromises()
  expect(first.get('button').attributes('aria-pressed')).toBe('true')
  expect(second.get('[role="alert"]').text()).toContain('已恢复原状态')
  const self = mount(FollowButton, { props: { target: { id: 1, username: 'alice' } } })
  expect(self.find('button').exists()).toBe(false)
  first.unmount(); second.unmount(); self.unmount()
})
