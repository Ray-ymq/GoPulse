import { expect, test, type Page } from '@playwright/test'

test('Phase 13 profile follow Following bookmark edit delete notification closure', async ({ browser }) => {
  test.setTimeout(120_000)
  const token = Date.now().toString(16)
  const a = await browser.newContext(); const b = await browser.newContext()
  const owner = await a.newPage(); const viewer = await b.newPage()
  async function register(page: Page, username: string) {
    await page.goto('/register'); await page.getByLabel('用户名').fill(username); await page.getByLabel('密码').fill(`delete-${token}-password`)
    await page.getByRole('button', { name: '注册并登录' }).click(); await expect(page).toHaveURL(/\/posts$/)
  }
  try {
    await register(owner, `da_${token}`); await register(viewer, `db_${token}`)
    expect((await owner.request.patch('/api/v1/users/me/profile', { data: { display_name: `Delete ${token}`, bio: 'Deletion acceptance' } })).status()).toBe(200)
    const made = await owner.request.post('/api/v1/posts', { data: { title: `delete${token}`, content: `delete${token}` } })
    expect(made.status()).toBe(201)
    const post = (await made.json()).data; const api = `/api/v1/posts/${post.id}`
    expect((await viewer.request.put(`/api/v1/users/${post.author.id}/follow`)).status()).toBe(200)
    expect((await viewer.request.put(`${api}/bookmark`)).status()).toBe(204)
    expect((await viewer.request.post(`${api}/comments`, { data: { content: 'deletion comment' } })).status()).toBe(201)
    expect((await viewer.request.put(`${api}/like`)).ok()).toBe(true)
    expect((await viewer.request.delete(api)).status()).toBe(403)
    await expect.poll(async () => (await (await owner.request.get('/api/v1/notifications')).json()).data.filter((n: { post_id: number }) => n.post_id === post.id).length).toBe(2)
    expect((await owner.request.patch(api, { data: { title: `edited${token}`, content: `edited${token}` } })).status()).toBe(200)
    await expect.poll(async () => (await (await owner.request.get(`/api/v1/search/posts?q=edited${token}`)).json()).data?.length, { timeout: 30_000 }).toBe(1)
    await owner.setViewportSize({ width: 390, height: 844 }); await owner.goto(`/posts/${post.id}`)
    await owner.getByLabel('帖子操作').click(); await owner.getByRole('button', { name: '删除', exact: true }).click()
    await expect(owner.getByRole('dialog')).toContainText('不可恢复')
    await owner.keyboard.press('Escape'); await expect(owner.getByRole('dialog')).not.toBeVisible()
    await owner.getByRole('button', { name: '删除', exact: true }).click()
    await owner.route(`**${api}`, route => route.request().method() === 'DELETE' ? route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'internal_error', message: 'retry' } }) }) : route.continue())
    await owner.getByRole('button', { name: '永久删除', exact: true }).click(); await expect(owner.getByRole('alert')).toBeVisible()
    await owner.unroute(`**${api}`); await owner.getByRole('button', { name: '永久删除', exact: true }).click()
    await expect(owner).not.toHaveURL(new RegExp(`/posts/${post.id}$`))
    expect((await owner.request.get(api)).status()).toBe(404); expect((await owner.request.delete(api)).status()).toBe(404)
    for (const url of ['/api/v1/posts', '/api/v1/posts/following', '/api/v1/bookmarks', `/api/v1/search/posts?q=edited${token}`]) {
      const response = await viewer.request.get(url); expect(response.ok()).toBe(true)
      expect((await response.json()).data.some((p: { id: number }) => p.id === post.id)).toBe(false)
    }
    await owner.goto('/notifications'); await expect(owner.getByText('原内容已删除', { exact: true })).toHaveCount(2)
    await expect(owner.getByRole('link', { name: '查看帖子' })).toHaveCount(0)
    const notifications = (await (await owner.request.get('/api/v1/notifications')).json()).data
    expect(notifications.filter((n: { resource_deleted: boolean }) => n.resource_deleted)).toHaveLength(2)
    expect(notifications.find((n: { type: string }) => n.type === 'user.followed').resource_deleted).toBe(false)
  } finally { await a.close(); await b.close() }
})
