import { expect, test } from '@playwright/test'

test('serves the production SPA and preserves Backend HTTP semantics', async ({ page, request }) => {
  const frontendHealth = await request.get('/frontend-health')
  expect(frontendHealth.status()).toBe(204)

  const health = await request.get('/health')
  expect(health.status()).toBe(200)
  expect(health.headers()['content-type']).toContain('application/json')

  const ready = await request.get('/ready')
  expect(ready.status()).toBe(200)
  expect(ready.headers()['content-type']).toContain('application/json')

  const protectedResponse = await request.get('/api/v1/posts')
  expect(protectedResponse.status()).toBe(401)
  expect(protectedResponse.headers()['content-type']).toContain('application/json')

  const unknownAPI = await request.get('/api/not-a-route')
  expect(unknownAPI.status()).toBe(404)
  expect(unknownAPI.headers()['content-type']).toContain('application/json')
  expect(await unknownAPI.text()).not.toContain('<!doctype html>')

  await page.goto('/posts/deep-link-probe')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
})
