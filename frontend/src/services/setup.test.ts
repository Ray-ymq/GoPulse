import { afterEach, expect, it, vi } from 'vitest'
import { authApi } from './api'
afterEach(() => vi.unstubAllGlobals())
it('accepts the read-only setup hint and rejects non-boolean data', async () => {
  const data = { id: 1, username: 'ordinary', role: 'user', created_at: '2026-09-12T00:00:00Z' }
  vi.stubGlobal('fetch', vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ data: { ...data, management_setup_available: false } }), { status: 200 }))
    .mockResolvedValueOnce(new Response(JSON.stringify({ data: { ...data, management_setup_available: 'false' } }), { status: 200 })))
  expect((await authApi.me()).management_setup_available).toBe(false)
  await expect(authApi.me()).rejects.toThrow()
})
