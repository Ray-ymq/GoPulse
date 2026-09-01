import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests } from '../composables/useAuth'
import { createAppRouter } from '../router'
import NewPostView from './NewPostView.vue'
import PostDetailView from './PostDetailView.vue'
import PostsView from './PostsView.vue'

const user = { id: 1, username: 'alice', created_at: '2026-09-01T00:00:00Z' }
const post = {
  id: 9,
  title: 'Phase 1 closed',
  content: 'Frontend business flow',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  author: { id: 1, username: 'alice' },
  comment_count: 1,
  like_count: 0,
  liked_by_me: false,
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(body === null ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function pathOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.pathname + input.search : input.url
}

async function authenticatedRouter(path: string) {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  return router
}

afterEach(() => {
  resetAuthForTests()
  vi.unstubAllGlobals()
})

describe('business views', () => {
  it('renders the empty post state and loads another page without duplicate requests', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      return Promise.resolve(jsonResponse({ data: [], meta: { next_cursor: null } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = await authenticatedRouter('/posts')
    const wrapper = mount(PostsView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('还没有帖子')
    expect(fetchMock.mock.calls.filter(([input]) => pathOf(input).includes('/posts?')).length).toBe(1)
  })

  it('validates post fields before sending and navigates after creation', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      return Promise.resolve(jsonResponse({ data: post }, 201))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = await authenticatedRouter('/posts/new')
    const wrapper = mount(NewPostView, { global: { plugins: [router] } })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('标题需为')
    await wrapper.get('input[name="title"]').setValue(post.title)
    await wrapper.get('textarea[name="content"]').setValue(post.content)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/posts/9')
  })

  it('publishes comments, refreshes server state, and performs explicit like requests', async () => {
    let liked = false
    const methods: string[] = []
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = pathOf(input)
      const method = init?.method ?? 'GET'
      methods.push(`${method} ${path}`)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      if (path.endsWith('/posts/9/like')) {
        liked = method === 'PUT'
        return Promise.resolve(jsonResponse(null, 204))
      }
      if (path.includes('/posts/9/comments') && method === 'POST') {
        return Promise.resolve(jsonResponse({ data: { id: 3, post_id: 9, content: 'Nice', created_at: post.created_at, author: post.author } }, 201))
      }
      if (path.includes('/posts/9/comments')) return Promise.resolve(jsonResponse({ data: [], meta: { next_cursor: null } }))
      return Promise.resolve(jsonResponse({ data: { ...post, liked_by_me: liked, like_count: liked ? 1 : 0 } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = await authenticatedRouter('/posts/9')
    const wrapper = mount(PostDetailView, { global: { plugins: [router] } })
    await flushPromises()

    await wrapper.get('.detail-actions button').trigger('click')
    await flushPromises()
    expect(methods).toContain('PUT /api/v1/posts/9/like')
    expect(wrapper.text()).toContain('取消点赞')

    await wrapper.get('.comment-form textarea').setValue('Nice')
    await wrapper.get('.comment-form').trigger('submit')
    await flushPromises()
    expect(methods).toContain('POST /api/v1/posts/9/comments')
    expect((wrapper.get('.comment-form textarea').element as HTMLTextAreaElement).value).toBe('')
  })
})
