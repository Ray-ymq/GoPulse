export type UserRole = 'user' | 'super_admin'

export interface PublicUser {
  management_setup_available?: boolean
  id: number
  username: string
  role: UserRole
  created_at: string
}

export interface AuthorSummary {
  following?: boolean
  id: number
  username: string
  display_name: string
}

export interface Post {
  edited_at: string | null
  content_revision: number
  id: number
  title: string
  content: string
  created_at: string
  updated_at: string
  author: AuthorSummary
  comment_count: number
  like_count: number
  liked_by_me: boolean
  bookmarked_by_me: boolean
}

export interface Comment {
  id: number
  post_id: number
  content: string
  created_at: string
  author: Omit<AuthorSummary, 'display_name'>
}

export interface Credentials {
  username: string
  password: string
}

export interface CreatePostInput {
  title: string
  content: string
}

export interface PostCommandMeta {
  command_id: string
  event_id: string
  data_epoch: string
  post_id: number
  committed_at: string
  replayed: boolean
  resource_state: 'available' | 'unavailable'
}

export interface PostCommandContext {
  data_epoch: string
  retry_window_seconds: number
  retry_window_until: string
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
  | 'user_not_found'
  | 'post_not_found'
  | 'data_epoch_mismatch'
  | 'idempotency_conflict'
  | 'command_result_unknown'
  | 'command_outcome_unknown'
  | 'business_async_backlog'
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

export type NotificationType = 'comment.created' | 'post.liked' | 'user.followed'

export interface Notification {
  resource_deleted?: boolean
  id: number
  type: NotificationType
  created_at: string
  read_at: string | null
  actor: AuthorSummary
  post_id: number | null
  comment_id: number | null
}

export interface UserProfile {
  following?: boolean
  id: number
  username: string
  display_name: string
  bio: string
  created_at: string
  is_self: boolean
}
