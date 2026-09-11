import { expect, test, type Page } from '@playwright/test'

const scenario = process.env.GOPULSE_ACCEPTANCE_SCENARIO ?? 'admin'
const adminUsername = process.env.GOPULSE_OBSERVABILITY_ADMIN_USERNAME ?? ''
const userUsername = process.env.GOPULSE_OBSERVABILITY_USER_USERNAME ?? ''
const password = process.env.GOPULSE_OBSERVABILITY_PASSWORD ?? ''
const redisPassword = process.env.GOPULSE_REDIS_PASSWORD ?? ''

async function register(page: Page, username: string): Promise<void> {
  await page.goto('/register')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '注册并登录' }).click()
  await expect(page).toHaveURL(/\/posts$/)
}

async function login(page: Page, username: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/posts$/)
}

async function createSocialPost(page: Page, marker: string): Promise<void> {
  await page.goto('/posts/new')
  await page.getByLabel('标题').fill(`Compose observability ${marker}`)
  await page.getByLabel('正文').fill('This post proves social reads and writes remain available during observability operations.')
  await page.getByRole('button', { name: '发布', exact: true }).click()
  await expect(page).toHaveURL(/\/posts\/\d+$/)
  await page.getByPlaceholder('写下你的评论…').fill(`Comment ${marker}`)
  await page.getByRole('button', { name: '发布评论' }).click()
  await expect(page.getByText(`Comment ${marker}`)).toBeVisible()
  await page.getByRole('button', { name: '点赞', exact: true }).click()
  await expect(page.getByRole('button', { name: '取消点赞' })).toBeVisible()
}

async function waitForMetric(page: Page): Promise<void> {
  await page.goto('/admin/observability/metrics')
  await expect(page.getByRole('heading', { name: 'Plugin Metrics' })).toBeVisible()
  await expect.poll(async () => {
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await page.waitForTimeout(500)
    return page.locator('.metric-value').count()
  }, { timeout: 45_000 }).toBeGreaterThan(0)
}

async function waitForLogs(page: Page): Promise<void> {
  await page.goto('/admin/observability/logs')
  await expect(page.getByRole('heading', { name: '应用日志' })).toBeVisible()
  await expect.poll(async () => {
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await page.waitForTimeout(500)
    return page.locator('.record-card').count()
  }, { timeout: 45_000 }).toBeGreaterThan(0)
}

async function waitForEvents(page: Page): Promise<void> {
  await page.goto('/admin/observability/events')
  await expect(page.getByRole('heading', { name: '运行事件' })).toBeVisible()
  await expect.poll(async () => {
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await page.waitForTimeout(750)
    return page.locator('.record-card').count()
  }, { timeout: 60_000 }).toBeGreaterThan(0)
}

function trackUnexpectedOrigins(page: Page): string[] {
  const unexpected: string[] = []
  page.on('request', request => {
    const url = new URL(request.url())
    if ((url.protocol === 'http:' || url.protocol === 'https:') && url.origin !== 'http://frontend:8080') unexpected.push(request.url())
  })
  return unexpected
}

test(`runs Compose observability scenario: ${scenario}`, async ({ browser, page }) => {
  test.setTimeout(180_000)
  expect(adminUsername).not.toBe('')
  expect(userUsername).not.toBe('')
  expect(password).not.toBe('')

  if (scenario === 'setup') {
    await register(page, adminUsername)
    const userContext = await browser.newContext()
    try {
      await register(await userContext.newPage(), userUsername)
    } finally {
      await userContext.close()
    }
    return
  }

  if (scenario === 'ordinary') {
    const observed: string[] = []
    page.on('request', request => {
      if (request.url().includes('/api/v1/observability/') || request.url().includes('/api/v1/exporter-plugins')) observed.push(request.url())
    })
    await login(page, userUsername)
    await expect(page.getByRole('link', { name: '可观测' })).toHaveCount(0)
    await page.goto('/admin/observability/metrics')
    await expect(page).toHaveURL(/\/forbidden$/)
    expect(observed).toEqual([])
    for (const endpoint of ['observability/metrics?metric=gopulse_redis_up&range=15m', 'observability/logs', 'observability/events', 'exporter-plugins']) {
      const response = await page.request.get(`/api/v1/${endpoint}`)
      expect(response.status()).toBe(403)
      expect(await response.json()).toMatchObject({ error: { code: 'permission_denied' } })
    }
    return
  }

  const unexpected = trackUnexpectedOrigins(page)
  await login(page, adminUsername)

  if (scenario === 'admin') {
    await createSocialPost(page, 'normal')
    await page.goto('/admin/observability/exporters')
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 30_000 })
    await expect.poll(async () => {
      await page.getByRole('button', { name: '刷新状态' }).click()
      await page.waitForTimeout(500)
      return page.locator('.exporter-details > div').filter({ hasText: '最近成功' }).locator('strong').textContent()
    }, { timeout: 30_000 }).not.toBe('—')
    page.once('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: '停止', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('stopped', { timeout: 20_000 })
    await page.getByRole('button', { name: '启动', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 20_000 })
    await waitForMetric(page)
    await waitForLogs(page)
    await waitForEvents(page)
    expect(unexpected).toEqual([])
    return
  }

  if (scenario === 'persistence') {
    await page.goto('/admin/observability/exporters')
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 30_000 })
    await waitForMetric(page)
    await waitForLogs(page)
    await waitForEvents(page)
    expect(unexpected).toEqual([])
    return
  }

  if (scenario === 'post-restart') {
    await createSocialPost(page, 'post-restart')
    await page.goto('/admin/observability/exporters')
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 30_000 })
    await waitForMetric(page)
    await waitForLogs(page)
    await waitForEvents(page)
    expect(unexpected).toEqual([])
    return
  }

  if (scenario === 'vm-down') {
    await createSocialPost(page, 'vm-down')
    await page.goto('/admin/observability/metrics')
    await expect(page.getByText(/指标存储或查询服务暂时不可用（VictoriaMetrics）/)).toBeVisible({ timeout: 20_000 })
    expect((await page.request.get('/ready')).status()).toBe(200)
    expect(unexpected).toEqual([])
    return
  }

  if (scenario === 'monitor-down') {
    await createSocialPost(page, 'monitor-down')
    await page.goto('/admin/observability/exporters')
    await expect(page.getByText(/Monitor 暂时不可用/)).toBeVisible({ timeout: 20_000 })
    expect((await page.request.get('/ready')).status()).toBe(200)
    await waitForMetric(page)
    expect(unexpected).toEqual([])
    return
  }

  if (scenario === 'transport-down') {
    await createSocialPost(page, 'transport-down')
    expect((await page.request.get('/ready')).status()).toBe(200)
    await page.goto('/admin/observability/exporters')
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 20_000 })
    expect(unexpected).toEqual([])
    return
  }

  if (scenario === 'manage') {
    expect(redisPassword).not.toBe('')
    await page.goto('/admin/observability/exporters')
    await expect(page.getByRole('heading', { name: 'GoPulse redis Exporter 目标配置', exact: true })).toBeVisible()
    await page.getByLabel('password').fill(redisPassword)
    await page.getByRole('button', { name: '安装并启动' }).click()
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 30_000 })
    page.once('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: '停止', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('stopped', { timeout: 20_000 })
    await page.getByRole('button', { name: '启动', exact: true }).click()
    await expect(page.locator('.state-pill')).toHaveText('running', { timeout: 20_000 })
    await waitForMetric(page)
    await waitForEvents(page)
    expect(unexpected).toEqual([])
    return
  }

  throw new Error(`unknown GOPULSE_ACCEPTANCE_SCENARIO: ${scenario}`)
})
