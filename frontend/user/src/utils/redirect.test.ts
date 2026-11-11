import { describe, expect, it } from 'vitest'
import { loginDestination } from './redirect'
describe('登录跳转', () => {
  it('所有角色均回到业务页面，拒绝已删除的管理页面', () => {
    expect(loginDestination(undefined, 'user')).toBe('/posts')
    expect(loginDestination(undefined, 'super_admin')).toBe('/posts')
    expect(loginDestination('/admin/logs?service=backend', 'super_admin')).toBe('/posts')
    expect(loginDestination('/admin/business-stages', 'super_admin')).toBe('/posts')
    expect(loginDestination('/admin', 'super_admin')).toBe('/posts')
    expect(loginDestination('/admin/logs', 'user')).toBe('/posts')
  })
  it('rejects external, encoded, control-character and normalization bypasses', () => {
    for (const path of ['https://evil.test', '//evil.test', '/\\evil.test', '/%2f%2fevil.test', '/posts/../admin/logs', '/\n/admin', '/login', '/api/v1/auth/logout', '/unknown', '/admin/unknown']) {
      expect(loginDestination(path, 'user')).toBe('/posts')
    }
  })
})
