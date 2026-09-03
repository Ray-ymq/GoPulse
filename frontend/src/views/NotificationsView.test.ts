import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests } from '../composables/useAuth'
import { createAppRouter } from '../router'
import NotificationsView from './NotificationsView.vue'

const user = { id: 1, username: 'alice', role: 'user', created_at: '2026-09-02T00:00:00Z' }
const first = {
  id: 9,
  type: 'comment.created',
  created_at: '2026-09-02T08:00:00Z',
  read_at: null,
  actor: { id: 2, username: 'bob' },
  post_id: 31,
  comment_id: 41,
}
const second = {
  id: 8,
  type: 'post.liked',
  created_at: '2026-09-02T07:00:00Z',
  read_at: '2026-09-02T07:30:00Z',
  actor: { id: 3, username: 'carol' },
  post_id: 30,
  comment_id: null,
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function pathOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.pathname + input.search : input.url
}

async function mountView() {
  const router = createAppRouter(createMemoryHistory())
  await router.push('/notifications')
  const wrapper = mount(NotificationsView, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  resetAuthForTests()
  vi.unstubAllGlobals()
})

describe('NotificationsView', () => {
  it('refreshes, paginates, links to posts, and prevents duplicate mark-read requests', async () => {
    const requests: string[] = []
    let initialLoads = 0
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = pathOf(input)
      const method = init?.method ?? 'GET'
      requests.push(`${method} ${path}`)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      if (path.endsWith('/notifications/9/read')) return Promise.resolve(jsonResponse(null, 204))
      if (path.includes('cursor=next')) return Promise.resolve(jsonResponse({ data: [second], meta: { next_cursor: null } }))
      initialLoads += 1
      return Promise.resolve(jsonResponse({ data: [first], meta: { next_cursor: 'next' } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('@bob')
    expect(wrapper.text()).toContain('评论了你的帖子')
    expect(wrapper.get('a[href="/posts/31"]').exists()).toBe(true)

    const markButton = wrapper.get('.notification-card button')
    await Promise.all([markButton.trigger('click'), markButton.trigger('click')])
    await flushPromises()
    expect(requests.filter((request) => request === 'PATCH /api/v1/notifications/9/read')).toHaveLength(1)
    expect(wrapper.text()).toContain('已读')

    await wrapper.get('.load-more button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('@carol')
    expect(wrapper.text()).toContain('已无更多通知')

    await wrapper.get('.page-heading button').trigger('click')
    await flushPromises()
    expect(initialLoads).toBe(2)
    expect(wrapper.text()).not.toContain('@carol')
  })

  it('shows a retryable initial loading failure', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      return Promise.resolve(jsonResponse({ error: { code: 'internal_error', message: 'an internal error occurred' } }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = await mountView()
    expect(wrapper.text()).toContain('an internal error occurred')
    expect(wrapper.get('.state-card button').text()).toBe('重试')
  })

  it('keeps an unread notification actionable after a mark-read failure', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      if (path.endsWith('/notifications/9/read')) {
        return Promise.resolve(jsonResponse({ error: { code: 'notification_not_found', message: 'notification not found' } }, 404))
      }
      return Promise.resolve(jsonResponse({ data: [first], meta: { next_cursor: null } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = await mountView()
    const button = wrapper.get('.notification-card button')
    await button.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('notification not found')
    expect(button.attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('未读')
  })
})
