import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests, useAuth } from './useAuth'

const currentUser = { id: 1, username: 'alice', role: 'user', created_at: '2026-09-01T00:00:00Z' }

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
    resolve(jsonResponse({ data: currentUser }))
    await Promise.all([first, second])

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(auth.status.value).toBe('authenticated')
    expect(auth.user.value?.username).toBe('alice')
  })

  it.each([
    ['missing role', { id: 1, username: 'alice', created_at: '2026-09-01T00:00:00Z' }],
    ['unknown role', { ...currentUser, role: 'owner' }],
  ])('rejects a current-user response with %s', async (_name, responseUser) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: responseUser })))
    const auth = useAuth()

    await expect(auth.initialize()).rejects.toMatchObject({ code: 'invalid_response' })
    expect(auth.status.value).toBe('error')
    expect(auth.user.value).toBeNull()
  })

  it('treats an authentication-required response as an anonymous state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: { code: 'authentication_required', message: 'required' } }, 401)))
    const auth = useAuth()
    await expect(auth.initialize()).resolves.toBeUndefined()
    expect(auth.status.value).toBe('anonymous')
    expect(auth.user.value).toBeNull()
  })

  it('keeps a network failure retryable and recovers without logging in again', async () => {
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new TypeError('connection refused'))
      .mockResolvedValueOnce(jsonResponse({ data: currentUser }))
    vi.stubGlobal('fetch', fetchMock)
    const auth = useAuth()

    await expect(auth.initialize()).rejects.toMatchObject({ code: 'network_error' })
    expect(auth.status.value).toBe('error')
    expect(auth.user.value).toBeNull()

    await expect(auth.initialize()).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(auth.status.value).toBe('authenticated')
    expect(auth.user.value?.username).toBe('alice')
  })

  it.each([
    ['server failure', jsonResponse({ error: { code: 'internal_error', message: 'failed' } }, 500)],
    ['invalid response', jsonResponse({ unexpected: true })],
  ])('does not convert a %s into an anonymous state', async (_name, response) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response))
    const auth = useAuth()

    await expect(auth.initialize()).rejects.toBeDefined()
    expect(auth.status.value).toBe('error')
    expect(auth.user.value).toBeNull()
  })
})
