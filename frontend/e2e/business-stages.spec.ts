import { expect, test, type Page } from '@playwright/test'

const adminUsername = process.env.GOPULSE_OBSERVABILITY_ADMIN_USERNAME ?? ''
const userUsername = process.env.GOPULSE_OBSERVABILITY_USER_USERNAME ?? ''
const password = process.env.GOPULSE_OBSERVABILITY_PASSWORD ?? ''

type StageIdentity = {
  command_id: string
  event_id: string
  data_epoch: string
  post_id: number
}

async function login(page: Page, username: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(username === adminUsername ? /\/admin\/$/ : /\/posts$/)
}

async function createPost(page: Page): Promise<StageIdentity> {
  const createResponse = page.waitForResponse(response => response.url().includes('/api/v1/posts') && response.request().method() === 'POST')
  await page.goto('/posts/new')
  await page.getByLabel('标题').fill(`Business stage evidence ${Date.now()}`)
  await page.getByLabel('正文').fill('Stage evidence browser contract does not expose post content in the management page.')
  await page.getByRole('button', { name: '发布', exact: true }).click()
  const response = await createResponse
  expect(response.status()).toBe(201)
  const body = await response.json() as { meta?: StageIdentity }
  expect(body.meta?.command_id).toMatch(/^[0-9a-f-]{36}$/)
  expect(body.meta?.event_id).toMatch(/^[0-9a-f-]{36}$/)
  expect(body.meta?.data_epoch).toMatch(/^[0-9a-f-]{36}$/)
  expect(body.meta?.post_id).toBeGreaterThan(0)
  await expect(page).toHaveURL(/\/posts\/\d+$/)
  return body.meta as StageIdentity
}

async function writeCaseEvidence(identity: StageIdentity): Promise<void> {
  const path = process.env.GOPULSE_V1_CASE_EVIDENCE
  if (!path) return
  const { writeFile } = await import('node:fs/promises')
  await writeFile(path, JSON.stringify({
    case_id: process.env.GOPULSE_V1_CASE_ID ?? 'V01',
    identity,
    checks: { created: true, stage_page: true, precise_logs_link: true },
    captured_at: new Date().toISOString(),
  }) + '\n', { mode: 0o600 })
}

test('creates a post, checks its stage evidence, and preserves ordinary-user denial', async ({ browser, page }) => {
  test.setTimeout(120_000)
  expect(adminUsername).not.toBe('')
  expect(userUsername).not.toBe('')
  expect(password).not.toBe('')

  await login(page, adminUsername)
  const identity = await createPost(page)
  await page.goto('/admin/business-stages')
  await expect(page.getByRole('heading', { name: '业务阶段查证', exact: true })).toBeVisible()
  await expect(page.getByLabel('Data epoch')).toHaveValue(identity.data_epoch)
  await page.getByLabel('Command ID').fill(identity.command_id)

  const stageResponse = page.waitForResponse(response => response.url().includes('/api/v1/observability/business-stages?') && response.request().method() === 'GET')
  await page.getByRole('button', { name: '查证阶段', exact: true }).click()
  const response = await stageResponse
  expect(response.status()).toBe(200)
  const stage = await response.json() as { data?: { resolved?: StageIdentity, evidence?: { command?: { quality?: string } } } }
  expect(stage.data?.resolved?.command_id).toBe(identity.command_id)
  expect(stage.data?.resolved?.event_id).toBe(identity.event_id)
  expect(stage.data?.resolved?.post_id).toBe(identity.post_id)
  expect(stage.data?.evidence?.command?.quality).toBe('known')
  const commandCard = page.locator('article.stage-evidence-card').filter({ hasText: '命令提交' })
  await expect(commandCard).toContainText('命令提交')
  await expect(commandCard).toContainText('已知依据')
  const logsLink = page.getByRole('link', { name: '在 Logs 中查看精确关联' })
  await expect(logsLink).toHaveAttribute('href', new RegExp(`data_epoch=${identity.data_epoch}`))
  await expect(logsLink).toHaveAttribute('href', new RegExp(`command_id=${identity.command_id}`))
  await writeCaseEvidence(identity)

  const userContext = await browser.newContext()
  try {
    const userPage = await userContext.newPage()
    await login(userPage, userUsername)
    const deniedRequests: string[] = []
    userPage.on('request', request => {
      if (request.url().includes('/api/v1/observability/business-stages')) deniedRequests.push(request.url())
    })
    await userPage.goto('/admin/business-stages')
    await expect(userPage).toHaveURL(/\/posts$/)
    expect(deniedRequests).toEqual([])
  } finally {
    await userContext.close()
  }
})
