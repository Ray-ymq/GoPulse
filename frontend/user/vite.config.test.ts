import { describe, expect, it } from 'vitest'
import { adminTarget, backendProxyConfig, backendTarget, frontendPort } from './vite.config'

describe('backendTarget', () => {
  it('uses the Backend default port', () => {
    expect(backendTarget({})).toBe('http://localhost:8080')
  })

  it('uses a non-default HTTP_PORT for every Backend proxy entry', () => {
    const proxy = backendProxyConfig({ HTTP_PORT: '18080' })

    expect(Object.keys(proxy)).toEqual(['/health', '/ready', '/api/v1'])
    for (const entry of Object.values(proxy)) {
      expect(entry).toEqual({
        target: 'http://localhost:18080',
        changeOrigin: false,
      })
    }
  })

  it.each(['zero', '0', '65536', '-1'])('rejects invalid HTTP_PORT %s', (port) => {
    expect(() => backendTarget({ HTTP_PORT: port })).toThrow(
      'HTTP_PORT must be an integer from 1 to 65535',
    )
  })
})

describe('native frontend ports and admin proxy', () => {
  it('accepts the isolated native frontend and admin ports', () => {
    expect(frontendPort({ FRONTEND_PORT: '15173' })).toBe(15173)
    expect(adminTarget({ ADMIN_FRONTEND_PORT: '15174' })).toBe('http://localhost:15174')
  })

  it.each(['zero', '0', '65536', '-1'])('rejects invalid native port %s', (port) => {
    expect(() => frontendPort({ FRONTEND_PORT: port })).toThrow(
      'FRONTEND_PORT must be an integer from 1 to 65535',
    )
    expect(() => adminTarget({ ADMIN_FRONTEND_PORT: port })).toThrow(
      'ADMIN_FRONTEND_PORT must be an integer from 1 to 65535',
    )
  })
})
