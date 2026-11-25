import { request } from './http'
import type { Comment, Data, ManagedUser, RoleChange, Notification, Page, Post, PostInput, Profile, User } from '../types/api'
export function pageQuery(cursor?: string | null, extra: Record<string, string> = {}) {
  const params = new URLSearchParams({ limit: '20', ...extra }); if (cursor) params.set('cursor', cursor)
  return `?${params}`
}
export const api = {
  managedUser: (username: string) => request<Data<ManagedUser>>(`/admin/users/by-username/${encodeURIComponent(username)}`),
  changeRole: (id: number, role: User['role']) => request<Data<RoleChange>>(`/admin/users/${id}/role`, { method: 'PATCH', body: { role } }),
  me: () => request<Data<User>>('/users/me'),
  login: (username: string, password: string, register = false) => request<Data<User>>(`/auth/${register ? 'register' : 'login'}`, { method: 'POST', body: { username, password } }),
  logout: () => request<void>('/auth/logout', { method: 'POST' }),
  profile: (name: string) => request<Data<Profile>>(`/users/${encodeURIComponent(name)}`),
  updateProfile: (display_name: string, bio: string) => request<Data<Profile>>('/users/me/profile', { method: 'PATCH', body: { display_name, bio } }),
  relations: (kind: 'following' | 'followers', cursor?: string | null) => request<Page<Profile>>(`/users/me/${kind}${pageQuery(cursor)}`),
  follow: (id: number, active: boolean) => request<Data<{ following: boolean }>>(`/users/${id}/follow`, { method: active ? 'PUT' : 'DELETE' }),
  posts: (path = '/posts', cursor?: string | null, extra?: Record<string, string>, signal?: AbortSignal) => request<Page<Post>>(`${path}${pageQuery(cursor, extra)}`, { signal }),
  users: (q: string, cursor?: string | null, signal?: AbortSignal) => request<Page<Profile>>(`/search/users${pageQuery(cursor, { q })}`, { signal }),
  post: (id: number) => request<Data<Post>>(`/posts/${id}`),
  editPost: (id: number, input: PostInput) => request<Data<Post>>(`/posts/${id}`, { method: 'PATCH', body: input }),
  deletePost: (id: number) => request<void>(`/posts/${id}`, { method: 'DELETE' }),
  like: (id: number, active: boolean) => request<void>(`/posts/${id}/like`, { method: active ? 'PUT' : 'DELETE' }),
  bookmark: (id: number, active: boolean) => request<void>(`/posts/${id}/bookmark`, { method: active ? 'PUT' : 'DELETE' }),
  comments: (id: number, cursor?: string | null) => request<Page<Comment>>(`/posts/${id}/comments${pageQuery(cursor)}`),
  comment: (id: number, content: string) => request<Data<Comment>>(`/posts/${id}/comments`, { method: 'POST', body: { content } }),
  notifications: (cursor?: string | null) => request<Page<Notification>>(`/notifications${pageQuery(cursor)}`),
  read: (id: number) => request<void>(`/notifications/${id}/read`, { method: 'PATCH' }),
}
