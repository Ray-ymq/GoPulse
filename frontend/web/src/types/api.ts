export interface User { id: number; username: string; role: 'user' | 'super_admin'; created_at: string }
export interface Profile { id: number; username: string; display_name: string; bio: string; created_at: string; following: boolean; is_self: boolean }
export interface Author { id: number; username: string; display_name: string; following: boolean }
export interface Post { id: number; title: string; content: string; created_at: string; updated_at: string; edited_at: string | null; content_revision: number; bookmarked_by_me: boolean; liked_by_me: boolean; comment_count: number; like_count: number; author: Author }
export interface Comment { id: number; post_id: number; content: string; created_at: string; author: { id: number; username: string } }
export interface Notification { id: number; type: string; created_at: string; read_at: string | null; actor: { id: number; username: string; display_name: string }; post_id: number | null; comment_id: number | null; resource_deleted: boolean }
export interface Page<T> { data: T[]; meta: { next_cursor: string | null } }
export interface Data<T> { data: T }
export interface PostInput { title: string; content: string }
export interface CommandResult { data: Post | null; meta?: { resource_state: 'available' | 'unavailable'; post_id: number; replayed: boolean } }
export interface PendingPost { owner: number; key: string; epoch: string; input: PostInput }

export interface ManagedUser extends User { is_bootstrap_super_admin: boolean }
export interface RoleChange { user: ManagedUser; changed: boolean }
