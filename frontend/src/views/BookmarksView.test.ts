import { flushPromises, mount } from '@vue/test-utils'
import BookmarksView from './BookmarksView.vue'
import { postApi } from '../services/api'
import type { Post } from '../types/api'
vi.mock('../services/api', () => ({ postApi: { bookmarks: vi.fn() } }))
afterEach(() => vi.resetAllMocks())
it('bookmark list recovers, loads the next cursor and explains empty/deleted content', async () => {
  const record: Post = { id: 1, title: 'saved', content: '', created_at: '', updated_at: '', author: { id: 2, username: 'bob', display_name: 'Bob' }, comment_count: 0, like_count: 0, liked_by_me: false, edited_at: null, content_revision: 1, bookmarked_by_me: true }
  vi.mocked(postApi.bookmarks).mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValueOnce({ data: [record], nextCursor: 'next' })
    .mockResolvedValueOnce({ data: [{ ...record, id: 2 }], nextCursor: null })
    .mockResolvedValueOnce({ data: [], nextCursor: null })
  const options = { global: { stubs: { PostCard: { props: ['post'], template: '<article>{{ post.title }}</article>' } } } }
  const wrapper = mount(BookmarksView, options)
  await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('加载失败')
  await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
  await wrapper.get('button').trigger('click'); await flushPromises()
  expect(postApi.bookmarks).toHaveBeenLastCalledWith('next')
  expect(wrapper.findAll('article')).toHaveLength(2)
  expect(wrapper.text()).toContain('已删除的帖子会自动跳过')
  wrapper.unmount()
  const empty = mount(BookmarksView, options); await flushPromises()
  expect(empty.text()).toContain('还没有收藏的帖子'); empty.unmount()
})
