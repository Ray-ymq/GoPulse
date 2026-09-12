import { expect, test } from '@playwright/test'

test('component catalog and scraped identity render through Backend only', async ({ page }) => {
  test.skip(!process.env.GOPULSE_P14_ADMIN, 'requires owned component acceptance')
  test.setTimeout(90_000)
  const requests: string[] = []
  page.on('request', request => requests.push(request.url()))
  await page.goto('/login')
  await page.getByLabel('用户名').fill(process.env.GOPULSE_P14_ADMIN!)
  await page.getByLabel('密码').fill(process.env.GOPULSE_P14_PASSWORD!)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/metrics$/)
  await page.goto('/admin/metrics?source=backend')
  await expect(page.locator('.metric-value').first()).toBeVisible()
  await page.locator('form.filter-bar select').first().selectOption('gopulse_monitor_last_scrape_success_timestamp_seconds')
  await page.getByRole('button', { name: '应用', exact: true }).click()
  await expect(page.locator('.series-card').filter({ hasText: 'scraped_target_id=monitor-local' })).toBeVisible()
  expect(requests.some(url => /:1910[1-6]\//.test(url))).toBe(false)
  expect(requests.some(url => url.includes('/api/v1/observability/metrics?'))).toBe(true)
})
