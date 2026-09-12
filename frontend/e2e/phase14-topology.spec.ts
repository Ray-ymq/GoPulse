import { expect, test } from '@playwright/test'

test('Kafka and Elasticsearch administrator lifecycle with optional secrets', async ({ page }) => {
  test.skip(!process.env.GOPULSE_P14_ADMIN, 'run through owned source-focused acceptance')
  test.setTimeout(150_000)
  await page.goto('/login')
  await page.getByLabel('用户名').fill(process.env.GOPULSE_P14_ADMIN!)
  await page.getByLabel('密码').fill(process.env.GOPULSE_P14_PASSWORD!)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/metrics$/)
  for (const source of ['kafka', 'elasticsearch']) {
    await page.goto('/admin/plugins')
    await expect(page.getByRole('button').filter({ hasText: '未交付' })).toHaveCount(1)
    await page.getByRole('button').filter({ hasText: `GoPulse ${source} Exporter` }).click()
    await page.getByLabel('host', { exact: true }).fill(source)
    if (source === 'kafka') {
      await page.getByLabel('topic', { exact: true }).fill('gopulse-observability-v1')
      await page.getByLabel('consumer_group', { exact: true }).fill('gopulse-marshaller-metrics-v1')
    }
    await page.getByRole('button', { name: '连接测试', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('连接测试成功')
    await page.getByRole('button', { name: '安装并启动', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('running')
    page.once('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: '替换配置', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('配置已验证并保存')
    if (source === 'elasticsearch') await expect(page.getByLabel('password', { exact: true })).toHaveValue('')
    else await expect(page.getByLabel('password', { exact: true })).toHaveCount(0)
    page.once('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: '停止', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('stopped')
    await page.getByRole('button', { name: '启动', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('running')
    await page.getByRole('link', { name: '查询插件指标' }).click()
    await expect(page.getByRole('heading', { name: 'Plugin & Component Metrics' })).toBeVisible()
    await expect(page.locator('.metric-value').first()).toBeVisible()
    if (source === 'elasticsearch') {
      await page.locator('form.filter-bar select').first().selectOption('gopulse_elasticsearch_cluster_health_status')
      await page.getByRole('button', { name: '应用', exact: true }).click()
      await expect(page.locator('.metric-value')).toHaveCount(3)
    }
  }
})
