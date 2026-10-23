import { describe, expect, it } from 'vitest'
import { frontendPort } from './vite.config'

describe('native admin frontend port', () => {
  it('uses the isolated admin Vite port', () => {
    expect(frontendPort({ FRONTEND_PORT: '15174' })).toBe(15174)
  })

  it.each(['zero', '0', '65536', '-1'])('rejects invalid native port %s', (port) => {
    expect(() => frontendPort({ FRONTEND_PORT: port })).toThrow(
      'FRONTEND_PORT must be an integer from 1 to 65535',
    )
  })
})
