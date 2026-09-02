import { expect, test } from '@playwright/test'

test('completes the browser registration, post, comment, like, logout, and login flow', async ({ page }) => {
  const token = process.env.GOPULSE_ACCEPTANCE_TOKEN ?? Date.now().toString(16)
  const username = `web_${token}`.slice(0, 32)
  const password = `acceptance-${token}-password`
  const title = `Browser acceptance ${token}`

  await page.goto('/register')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '注册并登录' }).click()
  await expect(page).toHaveURL(/\/posts$/)
  await expect(page.getByText(`@${username}`)).toBeVisible()

  await page.reload()
  await expect(page).toHaveURL(/\/posts$/)
  await expect(page.getByText(`@${username}`)).toBeVisible()

  await page.locator('.app-header').getByRole('link', { name: '发布' }).click()
  await page.getByLabel('标题').fill(title)
  await page.getByLabel('正文').fill('This post verifies the real browser business flow.')
  await page.getByRole('button', { name: '发布', exact: true }).click()
  await expect(page).toHaveURL(/\/posts\/\d+$/)
  await expect(page.getByRole('heading', { name: title })).toBeVisible()

  await page.getByPlaceholder('写下你的评论…').fill('Browser comment')
  await page.getByRole('button', { name: '发布评论' }).click()
  await expect(page.getByText('Browser comment')).toBeVisible()

  await page.getByRole('button', { name: '点赞', exact: true }).click()
  await expect(page.getByRole('button', { name: '取消点赞' })).toBeVisible()
  await page.getByRole('button', { name: '取消点赞' }).click()
  await expect(page.getByRole('button', { name: '点赞', exact: true })).toBeVisible()

  await page.getByRole('link', { name: '帖子', exact: true }).click()
  await expect(page.getByRole('link', { name: title })).toBeVisible()
  await page.getByRole('button', { name: '退出' }).click()
  await expect(page).toHaveURL(/\/login$/)

  await page.goto('/posts')
  await expect(page).toHaveURL(/\/login$/)
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/posts$/)
})

test('notifications close the real two-user comment and like loop', async ({ browser }) => {
  test.setTimeout(60_000)
  const token = `${process.env.GOPULSE_ACCEPTANCE_TOKEN ?? ''}${Date.now().toString(16)}`
  const recipientUsername = `notify_owner_${token}`.slice(0, 32)
  const actorUsername = `notify_actor_${token}`.slice(0, 32)
  const password = `acceptance-${token}-password`
  const title = `Notification acceptance ${token}`
  const recipientContext = await browser.newContext()
  const actorContext = await browser.newContext()
  const recipient = await recipientContext.newPage()
  const actor = await actorContext.newPage()

  try {
    await recipient.goto('/register')
    await recipient.getByLabel('用户名').fill(recipientUsername)
    await recipient.getByLabel('密码').fill(password)
    await recipient.getByRole('button', { name: '注册并登录' }).click()
    await expect(recipient).toHaveURL(/\/posts$/)
    await recipient.goto('/posts/new')
    await recipient.getByLabel('标题').fill(title)
    await recipient.getByLabel('正文').fill('This post receives asynchronous notifications.')
    await recipient.getByRole('button', { name: '发布', exact: true }).click()
    await expect(recipient).toHaveURL(/\/posts\/\d+$/)
    const postURL = recipient.url()
    const postPath = new URL(postURL).pathname

    await actor.goto('/register')
    await actor.getByLabel('用户名').fill(actorUsername)
    await actor.getByLabel('密码').fill(password)
    await actor.getByRole('button', { name: '注册并登录' }).click()
    await expect(actor).toHaveURL(/\/posts$/)
    await actor.goto(postURL)
    await actor.getByPlaceholder('写下你的评论…').fill('A notification-producing comment')
    await actor.getByRole('button', { name: '发布评论' }).click()
    await expect(actor.getByText('A notification-producing comment')).toBeVisible()
    await actor.getByRole('button', { name: '点赞', exact: true }).click()
    await expect(actor.getByRole('button', { name: '取消点赞' })).toBeVisible()
    const duplicateLike = await actorContext.request.put(`/api/v1${postPath}/like`)
    expect(duplicateLike.status()).toBe(204)

    await actor.goto('/notifications')
    await expect(actor.getByText('暂时没有通知。')).toBeVisible()

    await recipient.goto('/notifications')
    await expect.poll(async () => {
      await recipient.getByRole('button', { name: '刷新', exact: true }).click()
      await recipient.waitForTimeout(250)
      return recipient.locator('.notification-card').count()
    }, { timeout: 15_000 }).toBe(2)
    await expect(recipient.getByText(`@${actorUsername}`)).toHaveCount(2)
    await expect(recipient.getByText('评论了你的帖子')).toBeVisible()
    await expect(recipient.getByText('赞了你的帖子')).toBeVisible()
    await expect(recipient.locator(`a[href="${postPath}"]`)).toHaveCount(2)

    await recipient.locator('.notification-card').first().getByRole('button', { name: '标记已读' }).click()
    await expect(recipient.locator('.notification-card').first().getByText('已读', { exact: true })).toBeVisible()
    await recipient.reload()
    await expect(recipient.locator('.notification-status').filter({ hasText: /^已读$/ })).toHaveCount(1)
    await expect(recipient.locator('.notification-card')).toHaveCount(2)

    await recipient.locator(`a[href="${postPath}"]`).first().click()
    await expect(recipient.getByRole('heading', { name: title })).toBeVisible()
  } finally {
    await recipientContext.close()
    await actorContext.close()
  }
})
