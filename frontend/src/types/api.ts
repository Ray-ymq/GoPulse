export interface PublicUser {
  id: number
  username: string
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
  | 'invalid_credentials'
  | 'username_conflict'
  | 'post_not_found'
  | 'internal_error'
  | 'network_error'
  | 'invalid_response'
