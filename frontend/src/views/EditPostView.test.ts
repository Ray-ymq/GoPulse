import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import { createAppRouter } from '../router'
import { resetAuthForTests } from '../composables/useAuth'
import NewPostView from './NewPostView.vue'
import PostEditMenu from '../components/PostEditMenu.vue'

const timestamp = '2026-09-08T00:00:00Z'
const post = { id: 9, title: 'Old title', content: 'Old content', author: { id: 1, username: 'alice', display_name: '' }, created_at: timestamp, updated_at: timestamp, edited_at: null, content_revision: 1, comment_count: 0, like_count: 0, liked_by_me: false, bookmarked_by_me: false }
afterEach(() => { resetAuthForTests(); vi.unstubAllGlobals() })
it('edit retains failed input, prevents duplicate saves, retries and navigates to current detail', async () => {
  let release: (value: Response) => void = () => {}
  let patches = 0
  vi.stubGlobal('fetch', vi.fn((url: string, options?: RequestInit) => {
    if (url.endsWith('/users/me')) return Promise.resolve(Response.json({ data: { id: 1, username: 'alice', role: 'user', created_at: timestamp } }))
    if (options?.method === 'PATCH') { patches++; return new Promise<Response>(resolve => { release = resolve }) }
    return Promise.resolve(Response.json({ data: post }))
  }))
  const router = createAppRouter(createMemoryHistory()); await router.push('/posts/9/edit')
  const wrapper = mount(NewPostView, { global: { plugins: [router] } }); await flushPromises()
  await wrapper.get('input').setValue('New title'); await wrapper.get('textarea').setValue('New content')
  await wrapper.get('form').trigger('submit'); await wrapper.get('form').trigger('submit')
  expect(patches).toBe(1); expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  release(Response.json({ error: { code: 'internal_error', message: 'Try again' } }, { status: 500 })); await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('Try again'); expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('New content')
  await wrapper.get('form').trigger('submit'); release(Response.json({ data: { ...post, title: 'New title', content: 'New content', edited_at: timestamp, content_revision: 2 } })); await flushPromises()
  expect(router.currentRoute.value.path).toBe('/posts/9')
  const authorMenu = mount(PostEditMenu, { props: { post }, global: { plugins: [router] } })
  expect(authorMenu.find('summary').exists()).toBe(true)
  await authorMenu.setProps({ post: { ...post, author: { ...post.author, id: 2 } } })
  expect(authorMenu.find('summary').exists()).toBe(false)
})
