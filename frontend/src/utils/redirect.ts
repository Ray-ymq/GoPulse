/** Deliberately reject encoded paths rather than normalizing ambiguous redirects. */
export function loginDestination(value: unknown, role: string | undefined): string {
  const fallback = role === 'super_admin' ? '/admin/' : '/posts'
  if (typeof value !== 'string' || !value.startsWith('/') || /[\\%\s\u0000-\u001f\u007f]/.test(value) || value.startsWith('//')) return fallback
  const path = value.split(/[?#]/)[0]!
  if (path.split('/').some(part => part === '.' || part === '..') || ['/login', '/register', '/auth-recovery'].includes(path)) return fallback
  if (role !== 'super_admin' && (path === '/admin' || path.startsWith('/admin/'))) return fallback
  const allowed = /^\/(?:posts(?:\/[^/]+(?:\/edit)?)?|bookmarks|search|notifications|me\/(?:following|followers)|users\/[^/]+|forbidden)$/.test(path) || (role === 'super_admin' && /^\/admin(?:\/(?:metrics|logs|events|alerts|users|audit|plugins)?)?$/.test(path))
  if (!allowed) return fallback
  return path === '/admin' ? '/admin/' + value.slice(path.length) : value
}
