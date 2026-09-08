import { expect, test, type Page } from '@playwright/test'

test('bookmark 收藏 stays private across feed, Following, search, profile, detail and saved list', async ({ browser }) => {
  test.setTimeout(120_000)
  const token = Date.now().toString(16)
  const password = `bookmark-${token}-password`
  const ownerName = `ba_${token}`; const viewerName = `bb_${token}`
  const title = `bookmarkstory${token}`
  const ownerContext = await browser.newContext(); const viewerContext = await browser.newContext()
  const owner = await ownerContext.newPage(); const viewer = await viewerContext.newPage()
  async function register(page: Page, username: string) {
    await page.goto('/register'); await page.getByLabel('用户名').fill(username); await page.getByLabel('密码').fill(password)
    await page.getByRole('button', { name: '注册并登录' }).click(); await expect(page).toHaveURL(/\/posts$/)
    return (await (await page.request.get('/api/v1/users/me')).json()).data.id as number
  }
  try {
    const a = await register(owner, ownerName)
    await owner.goto('/posts/new'); await owner.getByLabel('标题').fill(title); await owner.getByLabel('正文').fill('Private saved content acceptance.')
    await owner.getByRole('button', { name: '发布', exact: true }).click(); await expect(owner).toHaveURL(/\/posts\/\d+$/)
    const postPath = new URL(owner.url()).pathname
    const id = Number(postPath.split('/').pop())
    await owner.reload() // warm shared projection with the other viewer first
    await register(viewer, viewerName)
    await viewer.getByRole('link', { name: '收藏', exact: true }).click()
    await expect(viewer.getByText('还没有收藏的帖子。')).toBeVisible()
    await viewer.goto('/posts')
    const card = viewer.locator('.post-card').filter({ hasText: title })
    await card.getByRole('button', { name: '收藏帖子', exact: true }).click()
    await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'true')
    for (const response of await Promise.all([viewer.request.put(`/api/v1/posts/${id}/bookmark`), viewer.request.put(`/api/v1/posts/${id}/bookmark`)])) expect(response.status()).toBe(204)
    for (const path of [`/posts/${id}`, '/posts', `/users/${ownerName}/posts`]) {
      const response = await viewer.request.get(`/api/v1${path}`); expect(response.status()).toBe(200)
      const data = (await response.json()).data
      expect((Array.isArray(data) ? data.find(p => p.id === id) : data).bookmarked_by_me).toBe(true)
    }
    const privateState = (await (await owner.request.get(`/api/v1/posts/${id}`)).json()).data
    expect(privateState.bookmarked_by_me).toBe(false); expect(privateState.like_count).toBe(0)
    expect((await (await owner.request.get('/api/v1/bookmarks')).json()).data).toEqual([])
    // Bookmark alone produces no notification; following below is a different event.
    expect((await (await owner.request.get('/api/v1/notifications')).json()).data).toEqual([])
    for (const query of [`user_id=${a}`, `username=${ownerName}`, 'cursor=bad']) expect((await viewer.request.get(`/api/v1/bookmarks?${query}`)).status()).toBe(400)
    for (const method of ['put', 'delete'] as const) expect((await viewer.request[method]('/api/v1/posts/999999999/bookmark')).status()).toBe(404)
    expect((await viewer.request.put(`/api/v1/users/${a}/follow`)).status()).toBe(200)
    await viewer.goto('/posts'); await viewer.getByRole('tab', { name: 'Following', exact: true }).click()
    await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await viewer.goto(`/users/${ownerName}`)
    await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect.poll(async () => {
      const response = await viewer.request.get(`/api/v1/search/posts?q=${title}`)
      if (response.status() !== 200) return false
      return (await response.json()).data.some((p: { id: number; bookmarked_by_me: boolean }) => p.id === id && p.bookmarked_by_me)
    }, { timeout: 30_000 }).toBe(true)
    await viewer.goto('/search'); await viewer.getByLabel('搜索词', { exact: true }).fill(title); await viewer.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await viewer.goto(postPath)
    const button = viewer.getByRole('button', { name: '收藏帖子', exact: true })
    await expect(button).toHaveAttribute('aria-pressed', 'true')
    await viewer.route(`**/api/v1/posts/${id}/bookmark`, route => route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'internal_error', message: 'temporary failure' } }) }))
    await button.click(); await expect(viewer.getByRole('alert')).toContainText('已恢复原状态'); await expect(button).toHaveAttribute('aria-pressed', 'true')
    await viewer.unroute(`**/api/v1/posts/${id}/bookmark`)
    await viewer.getByRole('link', { name: '收藏', exact: true }).click()
    await expect(card).toBeVisible(); await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await viewer.reload(); await expect(card).toBeVisible()
    await card.getByRole('button', { name: '收藏帖子', exact: true }).click()
    await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'false')
    await expect(card.getByRole('button', { name: '收藏帖子', exact: true })).toBeEnabled()
    expect((await viewer.request.delete(`/api/v1/posts/${id}/bookmark`)).status()).toBe(204)
    await viewer.reload(); await expect(viewer.getByText('还没有收藏的帖子。')).toBeVisible()
    expect((await (await viewer.request.get(`/api/v1/posts/${id}`)).json()).data.bookmarked_by_me).toBe(false)
    await owner.reload(); await expect(owner.getByRole('button', { name: '收藏帖子', exact: true })).toHaveAttribute('aria-pressed', 'false')
    await viewer.getByRole('button', { name: '退出' }).click(); await viewer.goto('/bookmarks'); await expect(viewer).toHaveURL(/\/login\?redirect=/)
    expect((await viewer.request.get('/api/v1/bookmarks')).status()).toBe(401)
  } finally { await ownerContext.close(); await viewerContext.close() }
})
