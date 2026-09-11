import { bookmarkReadBarrier } from '../composables/useBookmarks'
import type {
  Comment,
  CreateCommentInput,
  CreatePostInput,
  Credentials,
  Notification,
  Page,
  Post,
  PublicUser,
} from '../types/api'
import { requestData, requestPage, requestValidatedData, requestValidatedPage, requestVoid } from './http'

const encodeCursor = (cursor: string) => encodeURIComponent(cursor)

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasExactKeys(record: Record<string, unknown>, keys: string[]): boolean {
  const actual = Object.keys(record).sort()
  const expected = [...keys].sort()
  return actual.length === expected.length && actual.every((key, index) => key === expected[index])
}

function isPositiveID(value: unknown): value is number {
  return Number.isSafeInteger(value) && typeof value === 'number' && value > 0
}

function isTimestamp(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && Number.isFinite(Date.parse(value))
}

function isPublicUser(value: unknown): value is PublicUser {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'username', 'role', 'created_at'])) return false
  return isPositiveID(value.id)
    && typeof value.username === 'string'
    && value.username.length > 0
    && (value.role === 'user' || value.role === 'super_admin')
    && isTimestamp(value.created_at)
}

function isPost(value: unknown): value is Post {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'title', 'content', 'created_at', 'updated_at', 'edited_at', 'content_revision', 'author', 'comment_count', 'like_count', 'liked_by_me', 'bookmarked_by_me'])) return false
  if (!isRecord(value.author) || !hasExactKeys(value.author, ['id', 'username', 'display_name', ...(value.author.following === undefined ? [] : ['following'])])) return false
  return isPositiveID(value.id)
    && typeof value.title === 'string'
    && typeof value.content === 'string'
    && isTimestamp(value.created_at)
    && isTimestamp(value.updated_at)
    && (value.edited_at === null || isTimestamp(value.edited_at))
    && isPositiveID(value.content_revision)
    && isPositiveID(value.author.id)
    && typeof value.author.username === 'string'
    && typeof value.author.display_name === 'string'
    && Number.isSafeInteger(value.comment_count)
    && typeof value.comment_count === 'number'
    && value.comment_count >= 0
    && Number.isSafeInteger(value.like_count)
    && typeof value.like_count === 'number'
    && value.like_count >= 0
    && typeof value.liked_by_me === 'boolean'
    && typeof value.bookmarked_by_me === 'boolean'
}

function isNotification(value: unknown): value is Notification {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'type', 'created_at', 'read_at', 'actor', 'post_id', 'comment_id', ...(value.resource_deleted === undefined ? [] : ['resource_deleted'])])) return false
  if (!isRecord(value.actor) || !hasExactKeys(value.actor, ['id', 'username', ...(value.actor.display_name === undefined ? [] : ['display_name'])])) return false
  const validType = value.type === 'comment.created' || value.type === 'post.liked' || value.type === 'user.followed'
  const deleted = value.resource_deleted === true && value.type !== 'user.followed' && value.post_id === null && value.comment_id === null
  const validComment = value.type === 'comment.created' && !deleted
    ? isPositiveID(value.comment_id)
    : value.comment_id === null
  return isPositiveID(value.id)
    && validType
    && isTimestamp(value.created_at)
    && (value.read_at === null || isTimestamp(value.read_at))
    && isPositiveID(value.actor.id)
    && typeof value.actor.username === 'string'
    && value.actor.username.length > 0
    && (value.type === 'user.followed' || deleted ? value.post_id === null : isPositiveID(value.post_id))
    && validComment
}

export const authApi = {
  register: (credentials: Credentials) =>
    requestValidatedData<PublicUser>('/auth/register', isPublicUser, {
      method: 'POST',
      body: JSON.stringify(credentials),
    }),
  login: (credentials: Credentials) =>
    requestValidatedData<PublicUser>('/auth/login', isPublicUser, {
      method: 'POST',
      body: JSON.stringify(credentials),
    }),
  logout: () => requestVoid('/auth/logout', { method: 'POST' }),
  me: () => requestValidatedData<PublicUser>('/users/me', isPublicUser),
}

function readPosts<T extends Post | Page<Post>>(read: () => Promise<T>): Promise<T> {
  const reconcile = bookmarkReadBarrier()
  return read().then(result => { reconcile('data' in result ? result.data : [result]); return result })
}

export const postApi = {
  delete: (id: number) => requestVoid(`/posts/${id}`, { method: 'DELETE' }),
  bookmark: (postId: number, value: boolean) => requestVoid(`/posts/${postId}/bookmark`, { method: value ? 'PUT' : 'DELETE' }),
  bookmarks: (cursor?: string, limit = 20): Promise<Page<Post>> => readPosts(() => requestPage<Post>(`/bookmarks?limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`)),
  following: (cursor?: string, limit = 20): Promise<Page<Post>> => readPosts(() => requestPage<Post>(`/posts/following?limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`)),
  list: (cursor?: string, limit = 20): Promise<Page<Post>> =>
    readPosts(() => requestPage<Post>(`/posts?limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`)),
  detail: (postId: number) => readPosts(() => requestData<Post>(`/posts/${postId}`)),
  update: (id: number, input: CreatePostInput) =>
    requestValidatedData<Post>(`/posts/${id}`, isPost, { method: 'PATCH', body: JSON.stringify(input) }),
  create: (input: CreatePostInput) =>
    requestData<Post>('/posts', { method: 'POST', body: JSON.stringify(input) }),
  comments: (postId: number, cursor?: string, limit = 20): Promise<Page<Comment>> =>
    requestPage<Comment>(
      `/posts/${postId}/comments?limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`,
    ),
  createComment: (postId: number, input: CreateCommentInput) =>
    requestData<Comment>(`/posts/${postId}/comments`, {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  like: (postId: number) => requestVoid(`/posts/${postId}/like`, { method: 'PUT' }),
  unlike: (postId: number) => requestVoid(`/posts/${postId}/like`, { method: 'DELETE' }),
}

export const notificationApi = {
  list: (cursor?: string, limit = 20): Promise<Page<Notification>> =>
    requestValidatedPage<Notification>(
      `/notifications?limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`,
      isNotification,
    ),
  markRead: (notificationId: number) =>
    requestVoid(`/notifications/${notificationId}/read`, { method: 'PATCH' }),
}

export const searchApi = {
  posts: (query: string, cursor?: string, limit = 20): Promise<Page<Post>> =>
    readPosts(() => requestValidatedPage<Post>(
      `/search/posts?q=${encodeURIComponent(query)}&limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`,
      isPost,
    )),
}

export const userApi = {
  follow: (id: number, following: boolean) => requestData<{ following: boolean }>(`/users/${id}/follow`, { method: following ? 'PUT' : 'DELETE' }),
  relations: (kind: 'following' | 'followers', cursor?: string) => requestPage<import('../types/api').UserProfile>(`/users/me/${kind}?limit=20${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`),
  profile: (username: string) => requestData<import('../types/api').UserProfile>(`/users/${encodeURIComponent(username)}`),
  update: (display_name: string, bio: string) => requestData<import('../types/api').UserProfile>('/users/me/profile', { method: 'PATCH', body: JSON.stringify({ display_name, bio }) }),
  posts: (username: string, cursor?: string) => readPosts(() => requestPage<Post>(`/users/${encodeURIComponent(username)}/posts?limit=20${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`)),
  search: (query: string, cursor?: string) => requestPage<import('../types/api').UserProfile>(`/search/users?q=${encodeURIComponent(query)}&limit=20${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`),
}
