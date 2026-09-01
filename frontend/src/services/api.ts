import type {
  Comment,
  CreateCommentInput,
  CreatePostInput,
  Credentials,
  Page,
  Post,
  PublicUser,
} from '../types/api'
import { requestData, requestPage, requestVoid } from './http'

const encodeCursor = (cursor: string) => encodeURIComponent(cursor)

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
