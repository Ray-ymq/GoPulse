import { expect, test, type Page } from '@playwright/test'
const admin = process.env.GOPULSE_ADMIN_USERNAME!
const user = process.env.GOPULSE_USER_USERNAME!
const demoted = process.env.GOPULSE_DEMOTION_USERNAME!
const password = process.env.GOPULSE_ACCEPTANCE_PASSWORD!
async function signIn(page: Page, username: string, destination: RegExp) {
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(destination)
}
test.beforeEach(async ({ page }) => {
  expect(admin && user && password).toBeTruthy()
  const origin = new URL(process.env.GOPULSE_BASE_URL!).origin
  page.on('request', request => expect(new URL(request.url()).origin).toBe(origin))
})
test('same-origin paths, role defaults, safe restoration, cookie and 401', async ({ page, context }) => {
  await page.goto('/admin/logs')
  await expect(page).toHaveURL(/\/login\?redirect=/)
  expect(new URL(page.url()).searchParams.get('redirect')).toBe('/admin/logs')
  await signIn(page, admin, /\/admin\/logs$/)
  for (const [old, target] of [['/admin', '/admin/'], ['/admin/', '/admin/'],
    ['/admin/observability', '/admin/metrics'], ['/admin/observability/metrics', '/admin/metrics'],
    ['/admin/observability/logs', '/admin/logs'], ['/admin/observability/events', '/admin/events'], ['/admin/observability/exporters', '/admin/plugins']]) {
    await page.goto(old!)
    await expect(page).toHaveURL(new RegExp(target!+'$'))
    await expect(page.getByRole('heading', { name: '可观测中心', exact: true })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('heading', { name: '可观测中心', exact: true })).toBeVisible()
  }
  const cookies = await context.cookies()
  expect(cookies.length).toBeGreaterThan(0)
  expect(cookies.every(cookie => cookie.httpOnly)).toBe(true)
  expect(await page.evaluate(() => document.cookie)).toBe('')
  expect(await page.evaluate(() => Object.keys(localStorage))).toEqual([])
  await context.clearCookies()
  await page.getByRole('button', { name: '刷新状态' }).click()
  await expect(page).toHaveURL(/\/login\?redirect=/)
  await page.goto('/login?redirect=https://evil.invalid')
  await signIn(page, user, /\/posts$/)
  await page.goto('/posts/new')
  await expect(page.getByLabel('标题')).toBeVisible()
  await context.clearCookies()
  await page.goto('/posts')
  await expect(page).toHaveURL(/\/login/)
})
test('ordinary user never mounts management or sends its API requests', async ({ page }) => {
  await page.goto('/login?redirect=/admin/events')
  await signIn(page, user, /\/posts$/)
  const calls: string[] = []
  page.on('request', request => { if (/\/api\/v1\/(observability|exporter-plugins)/.test(request.url())) calls.push(request.url()) })
  for (const path of ['metrics', 'logs', 'events', 'plugins']) {
    await page.goto('/admin/'+path)
    await expect(page).toHaveURL(/\/posts$/)
  }
  expect(calls).toEqual([])
  for (const endpoint of ['observability/metrics?metric=gopulse_redis_up', 'observability/logs', 'observability/events', 'exporter-plugins']) {
    expect((await page.request.get('/api/v1/'+endpoint)).status()).toBe(403)
  }
})
test('existing real metrics logs events and six plugin lifecycle', async ({ page }) => {
  test.setTimeout(120_000)
  await page.goto('/login')
  await signIn(page, admin, /\/admin\/$/)
  await page.goto('/admin/plugins')
  await expect(page.locator('[aria-label="官方插件目录"]')).toBeVisible()
  await expect(page.locator('[aria-label="官方插件目录"] > button')).toHaveCount(6)
  await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 30_000 })
  page.once('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(page.locator('.state-pill')).toHaveText('stopped', { timeout: 30_000 })
  await page.getByRole('button', { name: '启动', exact: true }).click()
  await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 30_000 })
  for (const kind of ['metrics', 'logs', 'events']) {
    const response = page.waitForResponse(r => r.url().includes('/api/v1/observability/'+kind+'?') && r.status() === 200)
    await page.goto('/admin/'+kind)
    const body = await (await response).json()
    expect(body.data).toBeTruthy()
    const records = page.locator(kind === 'metrics' ? '.metric-value' : '.record-card')
    await expect.poll(async () => {
      if (await records.count()) return records.count()
      await page.getByRole('button', { name: '刷新', exact: true }).click()
      return records.count()
    }, { timeout: 30_000 }).toBeGreaterThan(0)
    await expect(page.locator('.admin-content')).toBeVisible()
    await expect(page.getByRole('alert')).toHaveCount(0)
  }
})
test('database demotion erases candidate secrets and DOM but retains social session', async ({ page }) => {
  await page.goto('/login')
  await signIn(page, demoted, /\/admin\/$/)
  await page.goto('/admin/plugins')
  await expect(page.locator('.state-pill')).toBeVisible()
  await page.getByLabel('password', { exact: true }).fill('candidate-secret-must-disappear')
  const me = await (await page.request.get('/api/v1/users/me')).json()
  expect((await page.request.put(`/api/v1/admin/users/${me.data.id}/role`, { data: { role: 'user' } })).status()).toBe(200)
  await page.getByRole('button', { name: '刷新状态' }).click()
  await expect(page).toHaveURL(/\/posts$/)
  await expect(page.locator('.admin-shell')).toHaveCount(0)
  expect(await page.content()).not.toContain('candidate-secret-must-disappear')
  expect((await page.request.get('/api/v1/users/me')).status()).toBe(200)
  await expect(page.getByRole('link', { name: '管理中心' })).toHaveCount(0)
})
