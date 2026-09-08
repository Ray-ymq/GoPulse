import { flushPromises, mount } from '@vue/test-utils'
import { useAuth, resetAuthForTests } from '../composables/useAuth'
import { clearBookmarks, bookmarkReadBarrier } from '../composables/useBookmarks'
import BookmarkButton from './BookmarkButton.vue'
import type { Post } from '../types/api'
afterEach(() => { resetAuthForTests(); clearBookmarks(); vi.unstubAllGlobals() })
it('bookmark controls share state, roll back failures and clear private state on logout', async () => {
  let fail = false
  vi.stubGlobal('fetch', vi.fn(async (input: string) => input.endsWith('/users/me')
    ? new Response(JSON.stringify({ data: { id: 1, username: 'alice', role: 'user', created_at: '2026-09-07T00:00:00Z' } }), { headers: { 'Content-Type': 'application/json' } })
    : fail ? new Response(JSON.stringify({ error: { code: 'internal_error', message: 'failed' } }), { status: 500, headers: { 'Content-Type': 'application/json' } }) : new Response(null, { status: 204 })))
  await useAuth().initialize()
  const post: Post = { id: 2, title: 'post', content: '', created_at: '', updated_at: '', author: { id: 3, username: 'bob', display_name: 'Bob' }, like_count: 0, comment_count: 0, liked_by_me: false, bookmarked_by_me: false }
  const first = mount(BookmarkButton, { props: { post } }); const second = mount(BookmarkButton, { props: { post } })
  const staleRead = bookmarkReadBarrier()
  await first.get('button').trigger('click'); await flushPromises()
  staleRead([post]); await flushPromises()
  expect(second.get('button').attributes('aria-pressed')).toBe('true')
  fail = true
  await second.get('button').trigger('click'); await flushPromises()
  expect(first.get('button').attributes('aria-pressed')).toBe('true')
  expect(second.get('[role="alert"]').text()).toContain('已恢复原状态')
  bookmarkReadBarrier()([post]); await flushPromises()
  expect(first.get('button').attributes('aria-pressed')).toBe('false')
  fail = false
  await first.get('button').trigger('click'); await flushPromises()
  await useAuth().logout(); await flushPromises()
  expect(first.get('button').attributes('aria-pressed')).toBe('false')
  first.unmount(); second.unmount()
})
