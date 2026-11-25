import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, request } from '../src/services/http'
afterEach(() => vi.unstubAllGlobals())
describe('HTTP contract', () => {
  it('sends session cookies and handles empty mutation responses', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 })); vi.stubGlobal('fetch', fetcher)
    await expect(request('/posts/3/like', { method: 'PUT' })).resolves.toBeUndefined()
    expect(fetcher).toHaveBeenCalledWith('/api/v1/posts/3/like', expect.objectContaining({ credentials: 'include', method: 'PUT' }))
  })
  it('preserves backend error identity and notifies expired sessions', async () => {
    const dispatch = vi.fn(); vi.stubGlobal('window', { dispatchEvent: dispatch })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'authentication_required', request_id: 'r-7' } }), { status: 401 })))
    await expect(request('/users/me')).rejects.toMatchObject({ status: 401, code: 'authentication_required', requestId: 'r-7' })
    expect(dispatch).toHaveBeenCalledOnce()
  })
  it('does not treat an HTML gateway response as successful data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('<html>gateway</html>', { status: 200 })))
    await expect(request('/posts')).rejects.toMatchObject({ code: 'invalid_response' })
  })
  it('represents network uncertainty separately from backend rejection', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    await expect(request('/posts')).rejects.toBeInstanceOf(ApiError)
    await expect(request('/posts')).rejects.toMatchObject({ status: 0, code: 'network_error' })
  })
})
