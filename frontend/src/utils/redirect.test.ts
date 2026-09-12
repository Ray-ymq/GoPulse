import { describe, expect, it } from 'vitest'
import { loginDestination } from './redirect'
describe('cross-application login destination', () => {
  it('defaults by role and restores role-matched deep links', () => {
    expect(loginDestination(undefined, 'user')).toBe('/posts')
    expect(loginDestination(undefined, 'super_admin')).toBe('/admin/')
    expect(loginDestination('/admin/logs?service=backend', 'super_admin')).toBe('/admin/logs?service=backend')
    expect(loginDestination('/admin', 'super_admin')).toBe('/admin/')
    expect(loginDestination('/admin/logs', 'user')).toBe('/posts')
  })
  it('rejects external, encoded, control-character and normalization bypasses', () => {
    for (const path of ['https://evil.test', '//evil.test', '/\\evil.test', '/%2f%2fevil.test', '/posts/../admin/logs', '/\n/admin', '/login']) {
      expect(loginDestination(path, 'user')).toBe('/posts')
    }
  })
})
