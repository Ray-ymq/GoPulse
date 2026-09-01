import { createMemoryHistory } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests, useAuth } from '../composables/useAuth'
import { createAppRouter } from './index'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => {
  resetAuthForTests()
  vi.unstubAllGlobals()
})

describe('business router guards', () => {
  it('waits for recovery and redirects anonymous users away from protected pages', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: { code: 'authentication_required', message: 'required' } }, 401)))
    const router = createAppRouter(createMemoryHistory())
    await router.push('/posts')
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('allows a recovered user into protected pages without a login flash', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: { id: 1, username: 'alice', created_at: '2026-09-01T00:00:00Z' } })))
    const router = createAppRouter(createMemoryHistory())
    await router.push('/posts/9')
    expect(router.currentRoute.value.path).toBe('/posts/9')
    expect(useAuth().status.value).toBe('authenticated')
  })

  it('redirects authenticated users away from guest-only pages', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: { id: 1, username: 'alice', created_at: '2026-09-01T00:00:00Z' } })))
    const router = createAppRouter(createMemoryHistory())
    await router.push('/register')
    expect(router.currentRoute.value.path).toBe('/posts')
  })

  it('keeps the diagnostic status page available without authentication recovery', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const router = createAppRouter(createMemoryHistory())
    await router.push('/dev/status')
    expect(router.currentRoute.value.path).toBe('/dev/status')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
