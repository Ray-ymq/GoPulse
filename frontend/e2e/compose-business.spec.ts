import { expect, test, type Page } from '@playwright/test'

const token = (process.env.GOPULSE_ACCEPTANCE_TOKEN ?? Date.now().toString(16)).replace(/[^a-z0-9]/gi, '').slice(0, 16)
const scenario = process.env.GOPULSE_ACCEPTANCE_SCENARIO ?? 'business'
const ownerUsername = `owner_${token}`.slice(0, 32)
const actorUsername = `actor_${token}`.slice(0, 32)
const password = `acceptance-${token}-password`
const primaryTitle = `Compose business ${token}`
const workerTitle = `Worker recovery ${token}`
const indexerTitle = `Indexer recovery ${token}`

async function register(page: Page, username: string) {
  await page.goto('/register')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '注册并登录' }).click()
  await expect(page).toHaveURL(/\/posts$/)
}

async function login(page: Page, username: string) {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/posts$/)
}

async function createPost(page: Page, title: string, body: string): Promise<string> {
  await page.goto('/posts/new')
  await page.getByLabel('标题').fill(title)
  await page.getByLabel('正文').fill(body)
  await page.getByRole('button', { name: '发布', exact: true }).click()
  await expect(page).toHaveURL(/\/posts\/\d+$/)
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
  return new URL(page.url()).pathname
}

async function expectSearch(page: Page, title: string) {
  await page.goto('/search')
  await page.getByLabel('搜索词').fill(title)
  await expect.poll(async () => {
    await page.getByRole('button', { name: '搜索', exact: true }).click()
    await page.waitForTimeout(300)
    return page.getByRole('link', { name: title }).count()
  }, { timeout: 30_000 }).toBe(1)
  await page.getByRole('link', { name: title }).click()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
}

async function openPostByTitle(page: Page, title: string) {
  await page.goto('/posts')
  await page.getByRole('link', { name: title }).click()
  await expect(page.getByRole('heading', { name: title })).toBeVisible()
}

test(`runs Compose business scenario: ${scenario}`, async ({ browser, page }) => {
  test.setTimeout(90_000)

  if (scenario === 'business') {
    await register(page, ownerUsername)
    const postPath = await createPost(page, primaryTitle, 'Created by the container-only browser acceptance flow.')
    await page.reload()
    await expect(page.getByRole('heading', { name: primaryTitle })).toBeVisible()

    const actorContext = await browser.newContext()
    const actor = await actorContext.newPage()
    try {
      await register(actor, actorUsername)
      await actor.goto(postPath)
      await actor.getByPlaceholder('写下你的评论…').fill(`Compose comment ${token}`)
      await actor.getByRole('button', { name: '发布评论' }).click()
      await expect(actor.getByText(`Compose comment ${token}`)).toBeVisible()
      await actor.getByRole('button', { name: '点赞', exact: true }).click()
      await expect(actor.getByRole('button', { name: '取消点赞' })).toBeVisible()
    } finally {
      await actorContext.close()
    }

    await page.goto('/notifications')
    await expect.poll(async () => {
      await page.getByRole('button', { name: '刷新', exact: true }).click()
      await page.waitForTimeout(300)
      return page.locator('.notification-card').count()
    }, { timeout: 30_000 }).toBe(2)
    await expect(page.getByText(`@${actorUsername}`)).toHaveCount(2)
    await expectSearch(page, primaryTitle)

    await page.getByRole('link', { name: '帖子', exact: true }).click()
    await page.getByRole('button', { name: '退出' }).click()
    await expect(page).toHaveURL(/\/login$/)
    await login(page, ownerUsername)
    return
  }

  if (scenario === 'persistence') {
    await login(page, ownerUsername)
    await openPostByTitle(page, primaryTitle)
    await expect(page.getByText(`Compose comment ${token}`)).toBeVisible()
    await expectSearch(page, primaryTitle)
    await page.goto('/notifications')
    await expect(page.locator('.notification-card')).toHaveCount(4)
    return
  }

  if (scenario === 'redis-fallback') {
    await login(page, ownerUsername)
    await openPostByTitle(page, primaryTitle)
    await createPost(page, `Redis fallback ${token}`, 'MySQL remains authoritative while Redis is unavailable.')
    return
  }

  if (scenario === 'worker-seed') {
    await login(page, ownerUsername)
    const postPath = await createPost(page, workerTitle, 'Worker is paused while durable events are created.')
    await page.getByRole('button', { name: '退出' }).click()
    await login(page, actorUsername)
    await page.goto(postPath)
    await page.getByPlaceholder('写下你的评论…').fill(`Worker recovery comment ${token}`)
    await page.getByRole('button', { name: '发布评论' }).click()
    await page.getByRole('button', { name: '点赞', exact: true }).click()
    return
  }

  if (scenario === 'worker-verify') {
    await login(page, ownerUsername)
    await page.goto('/notifications')
    await expect.poll(async () => {
      await page.getByRole('button', { name: '刷新', exact: true }).click()
      await page.waitForTimeout(300)
      return page.locator('.notification-card').count()
    }, { timeout: 30_000 }).toBe(4)
    return
  }

  if (scenario === 'indexer-seed') {
    await login(page, actorUsername)
    await createPost(page, indexerTitle, 'Indexer is paused while the Outbox retains this event.')
    return
  }

  if (scenario === 'indexer-verify') {
    await login(page, actorUsername)
    await expectSearch(page, indexerTitle)
    return
  }

  throw new Error(`unknown GOPULSE_ACCEPTANCE_SCENARIO: ${scenario}`)
})
