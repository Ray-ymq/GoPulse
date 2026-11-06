import { afterEach, expect, it, vi } from 'vitest'
import { loadDeployment, standaloneObservability } from './deployment'
import { bindAuthNavigation } from './useAuth'
import { requestData } from '../services/http'

afterEach(() => { vi.unstubAllGlobals(); standaloneObservability.value = false })

it('loads standalone deployment and keeps authentication navigation in the admin application', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ standaloneObservability: true })))
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'authentication_required', message: '请先登录。' } }), { status: 401 })))
  await loadDeployment()
  expect(standaloneObservability.value).toBe(true)
  const navigate = vi.fn()
  bindAuthNavigation(navigate)
  await expect(requestData('/users/me')).rejects.toThrow()
  expect(navigate.mock.calls[0]?.[0]).toMatch(/^\/admin\/login\?redirect=/)
})

it('uses the existing deployment when development metadata is unavailable', async () => {
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
  await loadDeployment()
  expect(standaloneObservability.value).toBe(false)
})
