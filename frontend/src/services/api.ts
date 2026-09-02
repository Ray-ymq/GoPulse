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
import { requestData, requestPage, requestValidatedPage, requestVoid } from './http'

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

function isPost(value: unknown): value is Post {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'title', 'content', 'created_at', 'updated_at', 'author', 'comment_count', 'like_count', 'liked_by_me'])) return false
  if (!isRecord(value.author) || !hasExactKeys(value.author, ['id', 'username'])) return false
  return isPositiveID(value.id)
    && typeof value.title === 'string'
    && typeof value.content === 'string'
    && isTimestamp(value.created_at)
    && isTimestamp(value.updated_at)
    && isPositiveID(value.author.id)
    && typeof value.author.username === 'string'
    && Number.isSafeInteger(value.comment_count)
    && typeof value.comment_count === 'number'
    && value.comment_count >= 0
    && Number.isSafeInteger(value.like_count)
    && typeof value.like_count === 'number'
    && value.like_count >= 0
    && typeof value.liked_by_me === 'boolean'
}

function isNotification(value: unknown): value is Notification {
  if (!isRecord(value) || !hasExactKeys(value, ['id', 'type', 'created_at', 'read_at', 'actor', 'post_id', 'comment_id'])) return false
  if (!isRecord(value.actor) || !hasExactKeys(value.actor, ['id', 'username'])) return false
  const validType = value.type === 'comment.created' || value.type === 'post.liked'
  const validComment = value.type === 'comment.created'
    ? isPositiveID(value.comment_id)
    : value.comment_id === null
  return isPositiveID(value.id)
    && validType
    && isTimestamp(value.created_at)
    && (value.read_at === null || isTimestamp(value.read_at))
    && isPositiveID(value.actor.id)
    && typeof value.actor.username === 'string'
    && value.actor.username.length > 0
    && isPositiveID(value.post_id)
    && validComment
}

export const authApi = {
  register: (credentials: Credentials) =>
    requestData<PublicUser>('/auth/register', {
      method: 'POST',
      body: JSON.stringify(credentials),
    }),
  login: (credentials: Credentials) =>
    requestData<PublicUser>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(credentials),
    }),
  logout: () => requestVoid('/auth/logout', { method: 'POST' }),
  me: () => requestData<PublicUser>('/users/me'),
}

export const postApi = {
  list: (cursor?: string, limit = 20): Promise<Page<Post>> =>
    requestPage<Post>(`/posts?limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`),
  detail: (postId: number) => requestData<Post>(`/posts/${postId}`),
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
    requestValidatedPage<Post>(
      `/search/posts?q=${encodeURIComponent(query)}&limit=${limit}${cursor ? `&cursor=${encodeCursor(cursor)}` : ''}`,
      isPost,
    ),
}
