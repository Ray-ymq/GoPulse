import { expect, test } from '@playwright/test'
import path from 'node:path'
import { existsSync } from 'node:fs'
test('management center real DTO layouts, navigation and responsive controls', async ({ page }) => {
  test.setTimeout(180_000)
  const origin = new URL(process.env.GOPULSE_BASE_URL!).origin
  page.on('request', request => expect(new URL(request.url()).origin).toBe(origin))
  await page.goto('/login')
  await page.getByLabel('用户名').fill(process.env.GOPULSE_ADMIN_USERNAME!)
  await page.getByLabel('密码').fill(process.env.GOPULSE_ACCEPTANCE_PASSWORD!)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/$/)
  const changedPages = process.env.GOPULSE_PRESENTATION_ROUTES?.split(',')
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : width === 768 ? 1024 : 1000 })
    for (const route of changedPages ?? ['', 'metrics', 'logs', 'plugins', 'events', 'alerts', 'users', 'audit']) {
      await page.goto('/admin/' + route)
      await expect(page.locator('.admin-content')).toBeVisible()
      if (route === '') await expect(page.locator('[data-section]')).toHaveCount(6)
      if (route === 'metrics') await expect(page.locator('.metric-value').first()).toBeVisible()
      if (route === 'logs') {
        await expect(page.locator('.record-card').first()).toBeVisible()
        await page.locator('.row-select').first().click()
        await expect(page.getByRole('complementary', { name: '日志详情' })).toBeFocused()
      }
      if (route === 'alerts') {
        await page.getByRole('button', { name: 'history', exact: true }).click()
        await expect(page.locator('.row-select').first()).toBeVisible()
        await page.locator('.row-select').first().click()
        await expect(page.getByRole('complementary', { name: '告警详情' })).toBeFocused()
      }
      if (route === 'plugins') {
        await expect(page.locator('[aria-label="已安装插件状态摘要"] strong').first()).toHaveText('1')
        await expect(page.locator('[aria-label="官方插件目录"]>button')).toHaveCount(6)
        for (const button of await page.locator('[aria-label="官方插件目录"]>button').all()) {
          await button.click()
          await expect(button).toHaveAttribute('aria-pressed', 'true')
        }
        await page.locator('[aria-label="官方插件目录"]>button').first().click()
        await expect(page.locator('.exporter-configuration')).toBeVisible()
      }
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      if (width === 390) {
        await page.getByRole('button', { name: '管理导航' }).click()
        await expect(page.getByRole('navigation', { name: '可观测导航' })).toBeVisible()
      }
      await expect(page.locator('.admin-nav [aria-current="page"]')).toHaveCount(1)
      if (width === 390) await page.getByRole('button', { name: '管理导航' }).click()
      const screenshot = path.join(path.resolve('..', process.env.GOPULSE_SCREENSHOT_DIR!), `${route || 'dashboard'}-${width}.png`)
      if (['', 'metrics', 'logs', 'alerts', 'plugins'].includes(route) && !existsSync(screenshot)) await page.screenshot({ path: screenshot, fullPage: true })
      if (route === 'logs' || route === 'alerts') {
        await page.getByRole('button', { name: '关闭详情', exact: true }).click()
        await expect(page.locator('.row-select').first()).toBeFocused()
        await expect(page.locator('.detail-panel')).toHaveCount(0)
      }
    }
  }
  if (changedPages) {
    await page.goto('/admin/metrics')
    const heading = await page.locator('.panel h3').first().textContent()
    await page.getByRole('combobox', { name: '指标', exact: true }).selectOption('gopulse_redis_connected_clients')
    await expect(page.locator('.panel h3').first()).toHaveText(heading!)
    await page.getByRole('button', { name: '应用', exact: true }).click()
    await expect(page.locator('.panel h3').first()).toHaveText('gopulse_redis_connected_clients')
    return
  }
  // Actual filters, exact identifiers and empty state without intercepted APIs.
  await page.goto('/admin/logs')
  await page.getByLabel('Request ID', { exact: true }).fill('f'.repeat(32))
  await page.getByRole('button', { name: '应用筛选' }).click()
  await expect(page.locator('.record-card')).toHaveCount(0)
  await expect(page.getByRole('status')).toContainText(/暂无/)
  await page.getByLabel('Request ID', { exact: true }).fill('')
  await page.getByRole('button', { name: '应用筛选' }).click()
  await expect(page.locator('.record-card').first()).toBeVisible()
  const more = page.getByRole('button', { name: '加载更多', exact: true })
  if (await more.count()) { const count = await page.locator('.record-card').count(); await more.click(); await expect.poll(() => page.locator('.record-card').count()).toBeGreaterThan(count) }
  await page.goto('/admin/metrics')
  await page.getByRole('combobox', { name: '指标', exact: true }).selectOption('gopulse_redis_connected_clients')
  await page.getByRole('combobox', { name: '范围', exact: true }).selectOption('1h')
  await page.getByRole('button', { name: '应用', exact: true }).click()
  await expect(page.locator('.metric-chart').first()).toBeVisible()
  await page.goto('/admin/')
  await page.keyboard.press('Tab')
  await expect(page.getByRole('link', { name: '跳至主要内容' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.locator('#admin-main')).toBeFocused()
  await page.getByRole('link', { name: '返回社交' }).click()
  await expect(page).toHaveURL(/\/posts$/)
  await page.goto('/admin/')
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/login$/)
})
