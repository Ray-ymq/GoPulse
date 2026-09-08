import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import PostsView from './PostsView.vue'
import { postApi } from '../services/api'
import type { Post } from '../types/api'

vi.mock('../services/api', () => ({ postApi: { list: vi.fn(), following: vi.fn() } }))
afterEach(() => vi.resetAllMocks())
it('Following recovers from a failed load, paginates and shows the empty state', async () => {
  const record = { id: 1, title: 'followed', content: '', created_at: '2026-09-07T00:00:00Z', updated_at: '2026-09-07T00:00:00Z', author: { id: 2, username: 'bob', display_name: 'Bob', following: true }, comment_count: 0, like_count: 0, liked_by_me: false, edited_at: null, content_revision: 1, bookmarked_by_me: false } satisfies Post
  vi.mocked(postApi.list).mockResolvedValue({ data: [], nextCursor: null })
  vi.mocked(postApi.following).mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValueOnce({ data: [record], nextCursor: 'next' })
    .mockResolvedValueOnce({ data: [{ ...record, id: 2 }], nextCursor: null })
    .mockResolvedValueOnce({ data: [], nextCursor: null })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/posts', component: PostsView }] })
  await router.push('/posts')
  const wrapper = mount(PostsView, { global: { plugins: [router], stubs: { PostCard: { props: ['post'], template: '<article>{{ post.title }}</article>' } } } })
  await flushPromises()
  await wrapper.findAll('[role="tab"]')[1]!.trigger('click'); await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('加载失败')
  await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
  expect(wrapper.findAll('article')).toHaveLength(1)
  await wrapper.get('.load-more button').trigger('click'); await flushPromises()
  expect(postApi.following).toHaveBeenLastCalledWith('next')
  expect(wrapper.findAll('article')).toHaveLength(2)
  await wrapper.findAll('[role="tab"]')[0]!.trigger('click'); await flushPromises()
  await wrapper.findAll('[role="tab"]')[1]!.trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('关注的人还没有帖子')
  wrapper.unmount()
})
