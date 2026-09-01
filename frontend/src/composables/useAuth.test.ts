import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests, useAuth } from './useAuth'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => {
  resetAuthForTests()
  vi.unstubAllGlobals()
})

describe('useAuth', () => {
  it('shares one current-user recovery request across concurrent initialization', async () => {
    let resolve!: (value: Response) => void
    const pending = new Promise<Response>((done) => { resolve = done })
    const fetchMock = vi.fn().mockReturnValue(pending)
    vi.stubGlobal('fetch', fetchMock)
    const auth = useAuth()

    const first = auth.initialize()
    const second = auth.initialize()
    resolve(jsonResponse({ data: { id: 1, username: 'alice', created_at: '2026-09-01T00:00:00Z' } }))
    await Promise.all([first, second])

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(auth.status.value).toBe('authenticated')
    expect(auth.user.value?.username).toBe('alice')
  })

  it('treats an authentication-required response as an anonymous state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: { code: 'authentication_required', message: 'required' } }, 401)))
    const auth = useAuth()
    await expect(auth.initialize()).resolves.toBeUndefined()
    expect(auth.status.value).toBe('anonymous')
    expect(auth.user.value).toBeNull()
  })
})
