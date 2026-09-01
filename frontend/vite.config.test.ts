import { describe, expect, it } from 'vitest'
import { backendProxyConfig, backendTarget } from './vite.config'

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
        changeOrigin: true,
      })
    }
  })

  it.each(['zero', '0', '65536', '-1'])('rejects invalid HTTP_PORT %s', (port) => {
    expect(() => backendTarget({ HTTP_PORT: port })).toThrow(
      'HTTP_PORT must be an integer from 1 to 65535',
    )
  })
})
