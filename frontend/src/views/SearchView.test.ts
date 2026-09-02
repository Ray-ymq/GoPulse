import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests } from '../composables/useAuth'
import { createAppRouter } from '../router'
import SearchView from './SearchView.vue'

const user = { id: 1, username: 'alice', created_at: '2026-09-02T00:00:00Z' }
const post = {
  id: 8,
  title: 'Elasticsearch rebuild',
  content: 'MySQL hydration remains authoritative',
  created_at: '2026-09-02T00:00:00Z',
  updated_at: '2026-09-02T00:00:00Z',
  author: { id: 1, username: 'alice' },
  comment_count: 2,
  like_count: 3,
  liked_by_me: true,
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function pathOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.pathname + input.search : input.url
}

afterEach(() => {
  resetAuthForTests()
  vi.unstubAllGlobals()
})

describe('SearchView', () => {
  it('restores the URL query, renders validated posts, and loads the next page', async () => {
    let searchCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      searchCalls += 1
      return Promise.resolve(jsonResponse(searchCalls === 1
        ? { data: [post], meta: { next_cursor: 'next-token' } }
        : { data: [{ ...post, id: 7, title: 'Second result' }], meta: { next_cursor: null } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = createAppRouter(createMemoryHistory())
    await router.push('/search?q=Elasticsearch')
    const wrapper = mount(SearchView, { global: { plugins: [router] } })
    await flushPromises()

    expect((wrapper.get('input[name="q"]').element as HTMLInputElement).value).toBe('Elasticsearch')
    expect(wrapper.text()).toContain('Elasticsearch rebuild')
    await wrapper.get('.load-more button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Second result')
    expect(fetchMock.mock.calls.some(([input]) => pathOf(input).includes('cursor=next-token'))).toBe(true)
  })

  it('shows empty and unavailable states and keeps searches on Backend relative paths', async () => {
    let unavailable = false
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      if (unavailable) return Promise.resolve(jsonResponse({ error: { code: 'search_unavailable', message: 'hidden' } }, 503))
      return Promise.resolve(jsonResponse({ data: [], meta: { next_cursor: null } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = createAppRouter(createMemoryHistory())
    await router.push('/search?q=missing')
    const wrapper = mount(SearchView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('没有找到相关帖子')

    unavailable = true
    await wrapper.get('input[name="q"]').setValue('retry')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('搜索服务暂时不可用')
    const searchPath = pathOf(fetchMock.mock.calls.at(-1)?.[0] as RequestInfo)
    expect(searchPath.startsWith('/api/v1/search/posts?')).toBe(true)
    expect(searchPath).not.toContain('9200')
  })
})

// Pagination retries must preserve the failed request, while an invalid PIT cursor
// deliberately restarts from the first page.
describe('SearchView pagination recovery', () => {
  it('retries a temporary load-more failure with the same cursor and appends results', async () => {
    let pageCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      pageCalls += 1
      if (pageCalls === 1) return Promise.resolve(jsonResponse({ data: [post], meta: { next_cursor: 'retry-token' } }))
      if (pageCalls === 2) return Promise.resolve(jsonResponse({ error: { code: 'search_unavailable', message: 'hidden' } }, 503))
      return Promise.resolve(jsonResponse({ data: [{ ...post, id: 7, title: 'Recovered result' }], meta: { next_cursor: null } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = createAppRouter(createMemoryHistory())
    await router.push('/search?q=Elasticsearch')
    const wrapper = mount(SearchView, { global: { plugins: [router] } })
    await flushPromises()

    await wrapper.get('.load-more button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('搜索服务暂时不可用')
    expect(wrapper.text()).toContain('Elasticsearch rebuild')

    await wrapper.get('.inline-action').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Elasticsearch rebuild')
    expect(wrapper.text()).toContain('Recovered result')
    const searchPaths = fetchMock.mock.calls
      .map(([input]) => pathOf(input))
      .filter((path) => path.startsWith('/api/v1/search/posts?'))
    expect(searchPaths.slice(-2).every((path) => path.includes('cursor=retry-token'))).toBe(true)
  })

  it('clears an invalid pagination snapshot and restarts once without the stale cursor', async () => {
    let pageCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = pathOf(input)
      if (path.endsWith('/users/me')) return Promise.resolve(jsonResponse({ data: user }))
      pageCalls += 1
      if (pageCalls === 1) return Promise.resolve(jsonResponse({ data: [post], meta: { next_cursor: 'expired-pit-token' } }))
      if (pageCalls === 2) return Promise.resolve(jsonResponse({ error: { code: 'validation_failed', message: 'cursor is invalid' } }, 400))
      return Promise.resolve(jsonResponse({ data: [{ ...post, id: 9, title: 'Fresh snapshot' }], meta: { next_cursor: null } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const router = createAppRouter(createMemoryHistory())
    await router.push('/search?q=Elasticsearch')
    const wrapper = mount(SearchView, { global: { plugins: [router] } })
    await flushPromises()

    await wrapper.get('.load-more button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('搜索结果已更新')
    expect(wrapper.text()).not.toContain('Elasticsearch rebuild')

    await wrapper.get('.inline-action').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Fresh snapshot')
    const searchPaths = fetchMock.mock.calls
      .map(([input]) => pathOf(input))
      .filter((path) => path.startsWith('/api/v1/search/posts?'))
    expect(searchPaths.at(-1)).not.toContain('cursor=')
  })
})
