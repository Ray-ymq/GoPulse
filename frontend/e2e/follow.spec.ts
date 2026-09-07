import { expect, test, type Page } from '@playwright/test'

test('follow, Following and notification close the private social loop', async ({ browser }) => {
  test.setTimeout(120_000)
  const token = Date.now().toString(16)
  const password = `following-${token}-password`
  const ownerName = `fa_${token}`; const actorName = `fb_${token}`
  const ownerContext = await browser.newContext(); const actorContext = await browser.newContext()
  const owner = await ownerContext.newPage(); const actor = await actorContext.newPage()
  async function register(page: Page, username: string) {
    await page.goto('/register'); await page.getByLabel('用户名').fill(username); await page.getByLabel('密码').fill(password)
    await page.getByRole('button', { name: '注册并登录' }).click(); await expect(page).toHaveURL(/\/posts$/)
    return (await (await page.request.get('/api/v1/users/me')).json()).data.id as number
  }
  try {
    const a = await register(owner, ownerName)
    await owner.goto('/posts/new'); await owner.getByLabel('标题').fill(`Following ${token}`); await owner.getByLabel('正文').fill('Only followed authors belong in this feed.')
    await owner.getByRole('button', { name: '发布', exact: true }).click(); await expect(owner).toHaveURL(/\/posts\/\d+$/)
    const postPath = new URL(owner.url()).pathname
    const b = await register(actor, actorName)
    await actor.getByRole('tab', { name: 'Following', exact: true }).click()
    await expect(actor.getByText('关注的人还没有帖子，去搜索用户并关注吧。')).toBeVisible()
    expect((await actor.request.put(`/api/v1/users/${b}/follow`)).status()).toBe(400)
    expect((await actor.request.put('/api/v1/users/999999999/follow')).status()).toBe(404)
    for (const path of [`/users/${a}/following`, `/users/${ownerName}/followers`]) expect((await actor.request.get(`/api/v1${path}`)).status()).toBe(404)
    for (const path of ['/posts/following?author_id=1', '/posts/following?cursor=bad', '/users/me/followers?limit=0', '/users/me/following?viewer=1']) expect((await actor.request.get(`/api/v1${path}`)).status()).toBe(400)
    await actor.goto(`/search?tab=users&q=${ownerName}`)
    // Use the actual tab so URL spelling is not a hidden test dependency.
    await actor.getByRole('tab', { name: '用户', exact: true }).click()
    await actor.getByLabel('搜索词', { exact: true }).fill(ownerName); await actor.getByRole('button', { name: '搜索', exact: true }).click()
    const result = actor.locator('.user-result').filter({ hasText: ownerName })
    await result.getByRole('button', { name: `关注 @${ownerName}`, exact: true }).click()
    await expect(result.getByRole('button', { name: `取消关注 @${ownerName}`, exact: true })).toBeVisible()
    const repeated = await Promise.all([actor.request.put(`/api/v1/users/${a}/follow`), actor.request.put(`/api/v1/users/${a}/follow`)])
    for (const response of repeated) expect(response.status()).toBe(200)
    await result.getByRole('link').click()
    await expect(actor.getByRole('button', { name: `取消关注 @${ownerName}`, exact: true }).first()).toBeVisible()
    await expect(actor.getByRole('link', { name: '我的关注', exact: true })).toHaveCount(0)
    await actor.goto(postPath)
    await expect(actor.getByRole('button', { name: `取消关注 @${ownerName}`, exact: true })).toBeVisible()
    await actor.goto('/me/following'); await actor.reload()
    await expect(actor.locator('.user-result').filter({ hasText: ownerName })).toBeVisible()
    await owner.goto('/me/followers'); await expect(owner.locator('.user-result').filter({ hasText: actorName })).toBeVisible()
    await actor.goto('/posts'); await actor.getByRole('tab', { name: 'Following', exact: true }).click()
    await expect(actor.locator('.post-card')).toHaveCount(1)
    await expect(actor.getByRole('link', { name: `Following ${token}`, exact: true })).toBeVisible()
    await expect.poll(async () => {
      const data = await (await owner.request.get('/api/v1/notifications')).json()
      return data.data.filter((n: { type: string; actor: { id: number } }) => n.type === 'user.followed' && n.actor.id === b).length
    }, { timeout: 30_000 }).toBe(1)
    await owner.goto('/notifications')
    const notice = owner.locator('.notification-card').filter({ hasText: '关注了你' })
    await expect(notice).toHaveCount(1); await expect(notice.getByRole('link', { name: '查看帖子' })).toHaveCount(0)
    await notice.getByRole('button', { name: '标记已读' }).click(); await expect(notice.getByText('已读', { exact: true })).toBeVisible()
    // Failure must roll back the shared button state rather than navigating away.
    await actor.route(`**/api/v1/users/${a}/follow`, route => route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'internal_error', message: 'temporary failure' } }) }))
    await actor.getByRole('button', { name: `取消关注 @${ownerName}`, exact: true }).click()
    await expect(actor.getByRole('alert')).toContainText('已恢复原状态')
    await expect(actor.getByRole('button', { name: `取消关注 @${ownerName}`, exact: true })).toBeVisible()
    await actor.unroute(`**/api/v1/users/${a}/follow`)
    await actor.getByRole('button', { name: `取消关注 @${ownerName}`, exact: true }).click()
    await expect(actor.getByRole('button', { name: `关注 @${ownerName}`, exact: true })).toBeVisible()
    expect((await actor.request.delete(`/api/v1/users/${a}/follow`)).status()).toBe(200)
    await actor.reload(); await actor.getByRole('tab', { name: 'Following', exact: true }).click()
    await expect(actor.locator('.post-card')).toHaveCount(0)
    await actor.goto('/me/following'); await expect(actor.getByText('暂无关系。')).toBeVisible()
    const notices = (await (await owner.request.get('/api/v1/notifications')).json()).data
    expect(notices.filter((n: { type: string }) => n.type === 'user.followed')).toHaveLength(1)
    await actor.getByRole('button', { name: '退出' }).click(); await actor.goto('/me/followers'); await expect(actor).toHaveURL(/\/login\?redirect=/)
    expect((await actor.request.get('/api/v1/users/me/followers')).status()).toBe(401)
  } finally { await ownerContext.close(); await actorContext.close() }
})
