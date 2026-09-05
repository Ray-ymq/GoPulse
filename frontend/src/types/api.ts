export type UserRole = 'user' | 'admin'

export interface PublicUser {
  id: number
  username: string
  role: UserRole
  created_at: string
}

export interface AuthorSummary {
  id: number
  username: string
}

export interface Post {
  id: number
  title: string
  content: string
  created_at: string
  updated_at: string
  author: AuthorSummary
  comment_count: number
  like_count: number
  liked_by_me: boolean
}

export interface Comment {
  id: number
  post_id: number
  content: string
  created_at: string
  author: AuthorSummary
}

export interface Credentials {
  username: string
  password: string
}

export interface CreatePostInput {
  title: string
  content: string
}

export interface CreateCommentInput {
  content: string
}

export interface Page<T> {
  data: T[]
  nextCursor: string | null
}

export type ApiErrorCode =
  | 'validation_failed'
  | 'authentication_required'
  | 'permission_denied'
  | 'invalid_credentials'
  | 'username_conflict'
  | 'post_not_found'
  | 'search_unavailable'
  | 'metrics_unavailable'
  | 'logs_unavailable'
  | 'events_unavailable'
  | 'plugin_package_invalid'
  | 'plugin_not_found'
  | 'plugin_conflict'
  | 'plugin_operation_in_progress'
  | 'plugin_operation_failed'
  | 'monitor_unavailable'
  | 'internal_error'
  | 'network_error'
  | 'invalid_response'

export type NotificationType = 'comment.created' | 'post.liked'

export interface Notification {
  id: number
  type: NotificationType
  created_at: string
  read_at: string | null
  actor: AuthorSummary
  post_id: number
  comment_id: number | null
}
