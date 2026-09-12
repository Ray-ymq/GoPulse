import { expect, test, type Page } from '@playwright/test'

async function login(page: Page, admin = true) {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(process.env[admin ? 'GOPULSE_ADMIN_USERNAME' : 'GOPULSE_USER_USERNAME']!)
  await page.getByLabel('密码').fill(process.env.GOPULSE_ACCEPTANCE_PASSWORD!)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/(posts|admin\/)$/)
}

test('clean setup never implicitly promotes the first social account', async ({ page }) => {
  await login(page)
  await expect(page).toHaveURL(/\/posts$/)
  await page.goto('/admin/')
  await expect(page).toHaveURL(/\/posts$/)
  await expect(page.getByRole('status')).toContainText('管理尚未初始化')
  expect((await page.request.get('/api/v1/admin/overview?range=15m')).status()).toBe(403)
})

test('bootstrap login enters the independent management frontend', async ({ page }) => {
  await login(page)
  await expect(page).toHaveURL(/\/admin\/$/)
  await expect(page.locator('[data-section]')).toHaveCount(6)
  await page.reload()
  await expect(page.locator('[data-section]')).toHaveCount(6)
  await page.goto('/posts')
  await expect(page).toHaveURL(/\/posts$/)
})

test('create exact three-source rules from the management catalog', async ({ page }) => {
  await login(page)
  await page.goto('/admin/alerts')
  for (const source of ['metrics', 'logs', 'events']) {
    await page.getByRole('button', { name: '创建规则' }).click()
    await page.getByLabel('名称', { exact: true }).fill('closure-' + source)
    await page.getByLabel('来源', { exact: true }).selectOption(source)
    if (source === 'metrics') {
      await page.getByLabel('指标', { exact: true }).selectOption('gopulse_redis_connected_clients')
      await page.getByLabel('聚合', { exact: true }).selectOption('last')
    } else {
      const expected = source === 'logs'
        ? { service: 'backend', module: 'auth', message: 'user registered' }
        : { event_name: 'exporter_plugin_started', severity: 'info', operation: 'start' }
      const options = await page.getByLabel('固定选择器', { exact: true }).locator('option').allTextContents()
      const index = options.findIndex(text => {
        const tuple = JSON.parse(text) as Record<string, string>
        return Object.entries(expected).every(([k, v]) => tuple[k] === v)
      })
      expect(index).toBeGreaterThanOrEqual(0)
      await page.getByLabel('固定选择器', { exact: true }).selectOption(String(index))
      await page.getByLabel('聚合', { exact: true }).selectOption('count')
    }
    await page.getByLabel('运算', { exact: true }).selectOption('gt')
    await page.getByLabel('阈值', { exact: true }).fill(source === 'metrics' ? '10' : '0')
    await page.getByLabel('窗口', { exact: true }).selectOption('5m')
    await page.getByLabel('持续', { exact: true }).selectOption('0s')
    await page.getByRole('button', { name: '保存规则' }).click()
    await expect(page.locator('[data-rule-id]').filter({ hasText: 'closure-' + source })).toHaveCount(1)
  }
})
