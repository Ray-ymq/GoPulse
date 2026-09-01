import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, requestData, requestPage, requestVoid, setUnauthorizedHandler } from './http'

function response(body: unknown, status = 200): Response {
  return new Response(body === null ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  setUnauthorizedHandler(() => undefined)
})

describe('HTTP service', () => {
  it('uses same-origin credentials and unwraps data', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ data: { id: 7 } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(requestData<{ id: number }>('/users/me')).resolves.toEqual({ id: 7 })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/users/me', expect.objectContaining({ credentials: 'include' }))
  })

  it('does not parse a valid 204 response', async () => {
    const result = response(null, 204)
    const jsonSpy = vi.spyOn(result, 'json')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(result))

    await expect(requestVoid('/auth/logout', { method: 'POST' })).resolves.toBeUndefined()
    expect(jsonSpy).not.toHaveBeenCalled()
  })

  it('preserves pagination metadata', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response({ data: [{ id: 1 }], meta: { next_cursor: 'next' } })))
    await expect(requestPage<{ id: number }>('/posts')).resolves.toEqual({
      data: [{ id: 1 }],
      nextCursor: 'next',
    })
  })

  it('clears authentication before exposing a stable 401 error', async () => {
    const unauthorized = vi.fn()
    setUnauthorizedHandler(unauthorized)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        response({ error: { code: 'authentication_required', message: 'authentication is required' } }, 401),
      ),
    )

    await expect(requestData('/posts')).rejects.toMatchObject<ApiError>({
      code: 'authentication_required',
      status: 401,
    })
    expect(unauthorized).toHaveBeenCalledOnce()
  })

  it('does not expose messages from unknown server error codes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(response({ error: { code: 'unexpected_backend_error', message: 'SQL detail' } }, 500)),
    )
    await expect(requestData('/posts')).rejects.toMatchObject<ApiError>({
      code: 'unexpected_backend_error',
      message: '操作失败，请稍后重试。',
    })
  })

  it('maps fetch failures to a safe network error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('connection refused')))
    await expect(requestData('/posts')).rejects.toMatchObject<ApiError>({ code: 'network_error', status: null })
  })
})
