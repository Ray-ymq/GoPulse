import { expect, test } from '@playwright/test'

const adminUsername = process.env.GOPULSE_OBSERVABILITY_ADMIN_USERNAME
const userUsername = process.env.GOPULSE_OBSERVABILITY_USER_USERNAME
const password = process.env.GOPULSE_OBSERVABILITY_PASSWORD

test('ordinary user is isolated from observability routes and APIs', async ({ page }) => {
  test.skip(!userUsername || !password, 'observability user credentials are required')
  const observed: string[] = []
  page.on('request', (request) => { if (request.url().includes('/api/v1/observability/')) observed.push(request.url()) })
  await page.goto('/login'); await page.getByLabel('用户名').fill(userUsername!); await page.getByLabel('密码').fill(password!); await page.getByRole('button', { name:'登录', exact:true }).click()
  await expect(page).toHaveURL(/\/posts$/)
  await expect(page.getByRole('link', { name:'可观测' })).toHaveCount(0)
  await page.goto('/admin/observability/metrics'); await expect(page).toHaveURL(/\/forbidden$/); await expect(page.getByRole('heading', { name:'无权访问管理区域' })).toBeVisible()
  expect(observed).toEqual([])
  for (const endpoint of ['metrics?metric=gopulse_redis_up','logs','events']) {
    const response = await page.request.get(`/api/v1/observability/${endpoint}`)
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
})

test('administrator completes the three-query browser loop', async ({ page }) => {
  test.skip(!adminUsername || !password, 'observability admin credentials are required')
  await page.goto('/login'); await page.getByLabel('用户名').fill(adminUsername!); await page.getByLabel('密码').fill(password!); await page.getByRole('button', { name:'登录', exact:true }).click()
  await expect(page).toHaveURL(/\/posts$/)
  await page.getByRole('link', { name:'可观测' }).click(); await expect(page).toHaveURL(/\/admin\/observability\/metrics$/)
  await expect(page.getByRole('heading', { name:'Redis Metrics' })).toBeVisible(); await expect(page.locator('.series-card').first()).toBeVisible({ timeout:15_000 })
  await page.getByRole('link', { name:'Logs', exact:true }).click(); await expect(page.locator('.record-card').first()).toBeVisible({ timeout:15_000 })
  await page.getByRole('link', { name:'Events', exact:true }).click(); await expect(page.getByText(/best-effort/)).toBeVisible(); await expect(page.locator('.record-card').first()).toBeVisible({ timeout:15_000 })
  await page.getByRole('link', { name:'返回社交' }).click(); await expect(page).toHaveURL(/\/posts$/)
})
