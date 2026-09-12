export type UserRole = 'user' | 'super_admin'

export interface PublicUser {
  management_setup_available?: boolean
  id: number
  username: string
  role: UserRole
  created_at: string
}

export interface Page<T> {
  data: T[]
  nextCursor: string | null
}
