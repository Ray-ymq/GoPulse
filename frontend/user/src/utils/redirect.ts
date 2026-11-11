/** Deliberately reject encoded paths rather than normalizing ambiguous redirects. */
export function loginDestination(value: unknown, _role: string | undefined): string {
  const fallback = '/posts'
  if (typeof value !== 'string' || !value.startsWith('/') || /[\\%\s\u0000-\u001f\u007f]/.test(value) || value.startsWith('//')) return fallback
  const path = value.split(/[?#]/)[0]!
  if (path.split('/').some(part => part === '.' || part === '..') || ['/login', '/register', '/auth-recovery'].includes(path)) return fallback
  const allowed = /^\/(?:posts(?:\/[^/]+(?:\/edit)?)?|bookmarks|search|notifications|me\/(?:following|followers)|users\/[^/]+|forbidden)$/.test(path)
  if (!allowed) return fallback
  return value
}
