import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { resetAuthForTests, useAuth } from '../composables/useAuth'
import AuthRecoveryView from '../views/AuthRecoveryView.vue'
import { createAppRouter } from './index'

const currentUser = { id: 1, username: 'alice', role: 'user', created_at: '2026-09-01T00:00:00Z' }

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
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: currentUser })))
    const router = createAppRouter(createMemoryHistory())
    await router.push('/posts/9')
    expect(router.currentRoute.value.path).toBe('/posts/9')
    expect(useAuth().status.value).toBe('authenticated')
  })

  it('redirects authenticated users away from guest-only pages', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ data: currentUser })))
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

  it('shows a retryable recovery page instead of treating a network failure as logout', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('connection refused')))
    const router = createAppRouter(createMemoryHistory())

    await router.push('/posts/9')

    expect(router.currentRoute.value.path).toBe('/auth-recovery')
    expect(router.currentRoute.value.query.redirect).toBe('/posts/9')
    expect(useAuth().status.value).toBe('error')
  })

  it('lets the recovery page retry and return to the original protected route', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: { code: 'internal_error', message: 'failed' } }, 503))
      .mockResolvedValueOnce(jsonResponse({ data: currentUser }))
    vi.stubGlobal('fetch', fetchMock)
    const router = createAppRouter(createMemoryHistory())
    await router.push('/posts/9')
    const wrapper = mount(AuthRecoveryView, { global: { plugins: [router] } })

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(router.currentRoute.value.path).toBe('/posts/9')
    expect(useAuth().status.value).toBe('authenticated')
    wrapper.unmount()
  })
})
