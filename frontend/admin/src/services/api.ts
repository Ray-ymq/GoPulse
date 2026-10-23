import { requestValidatedData } from './http'
import type { PublicUser } from '../types/api'
function isPublicUser(value: unknown): value is PublicUser {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const user = value as Record<string, unknown>
  return Object.keys(user).sort().join(',') === ('management_setup_available' in user ? 'created_at,id,management_setup_available,role,username' : 'created_at,id,role,username')
    && (user.management_setup_available === undefined || typeof user.management_setup_available === 'boolean')
    && Number.isSafeInteger(user.id) && Number(user.id) > 0
    && typeof user.username === 'string' && user.username.length > 0
    && (user.role === 'user' || user.role === 'super_admin')
    && typeof user.created_at === 'string' && Number.isFinite(Date.parse(user.created_at))
}
export const authApi = { me: () => requestValidatedData('/users/me', isPublicUser) }
