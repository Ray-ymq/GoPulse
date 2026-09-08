import { expect, test, type Page } from '@playwright/test'

test('edit 搜索 cache: author saves current content across all read paths', async ({ browser }) => {
  test.setTimeout(120_000)
  const token = Date.now().toString(16)
  const old = `oldedit${token}`; const next = `newedit${token}`
  const ownerContext = await browser.newContext(); const otherContext = await browser.newContext()
  const owner = await ownerContext.newPage(); const other = await otherContext.newPage()
  async function register(page: Page, username: string) {
    await page.goto('/register'); await page.getByLabel('用户名').fill(username); await page.getByLabel('密码').fill(`edit-${token}-password`)
    await page.getByRole('button', { name: '注册并登录' }).click(); await expect(page).toHaveURL(/\/posts$/)
  }
  try {
    await register(owner, `ea_${token}`)
    const created = await owner.request.post('/api/v1/posts', { data: { title: old, content: old } })
    expect(created.status()).toBe(201)
    const initial = (await created.json()).data
    const path = `/posts/${initial.id}`; const api = `/api/v1${path}`
    await owner.goto(path); await owner.reload()
    await register(other, `eb_${token}`)
    await other.goto(path); await expect(other.getByLabel('帖子操作')).toHaveCount(0)
    expect((await other.request.patch(api, { data: { title: next, content: next } })).status()).toBe(403)
    expect((await owner.request.patch('/api/v1/posts/999999999', { data: { title: next, content: next } })).status()).toBe(404)
    expect((await owner.request.patch(api, { data: { title: ' ', content: next } })).status()).toBe(400)
    expect((await owner.request.patch(api, { data: { title: next, content: next, content_revision: 99 } })).status()).toBe(400)
    expect((await other.request.put(`/api/v1/users/${initial.author.id}/follow`)).status()).toBe(200)
    expect((await other.request.put(`${api}/bookmark`)).status()).toBe(204)
    await expect.poll(async () => (await (await owner.request.get(`/api/v1/search/posts?q=${old}`)).json()).data?.some((p: { id: number }) => p.id === initial.id), { timeout: 30_000 }).toBe(true)
    await owner.getByLabel('帖子操作').click(); await owner.getByRole('link', { name: '编辑', exact: true }).click()
    await expect(owner.getByLabel('标题')).toHaveValue(old)
    await owner.getByLabel('标题').fill(next); await owner.getByLabel('正文').fill(next)
    await owner.route(`**${api}`, route => route.request().method() === 'PATCH' ? route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'internal_error', message: '保存暂时失败' } }) }) : route.continue())
    await owner.getByRole('button', { name: '保存', exact: true }).click()
    await expect(owner.getByRole('alert')).toBeVisible(); await expect(owner.getByLabel('正文')).toHaveValue(next)
    await owner.unroute(`**${api}`)
    await owner.getByRole('button', { name: '保存', exact: true }).click()
    await expect(owner).toHaveURL(new RegExp(`${path}$`)); await expect(owner.getByRole('heading', { name: next, exact: true })).toBeVisible()
    await expect(owner.getByText('已编辑', { exact: false })).toBeVisible()
    const updated = (await (await other.request.get(api)).json()).data
    expect(updated.content_revision).toBe(2); expect(updated.content).toBe(next); expect(updated.edited_at).toBeTruthy()
    const noop = (await (await owner.request.patch(api, { data: { title: next, content: next } })).json()).data
    expect(noop.content_revision).toBe(2); expect(noop.edited_at).toBe(updated.edited_at)
    await expect.poll(async () => (await (await owner.request.get(`/api/v1/search/posts?q=${next}`)).json()).data?.some((p: { id: number }) => p.id === initial.id), { timeout: 30_000 }).toBe(true)
    await expect.poll(async () => (await (await owner.request.get(`/api/v1/search/posts?q=${old}`)).json()).data?.length, { timeout: 30_000 }).toBe(0)
    for (const url of ['/posts', '/posts?feed=following', '/bookmarks', `/users/ea_${token}`, `/search?q=${next}`]) {
      await other.goto(url)
      if (url.includes('feed=following')) await other.getByRole('tab', { name: 'Following', exact: true }).click()
      const row = other.locator('.post-card').filter({ hasText: next })
      await expect(row).toBeVisible(); await expect(row.getByText('已编辑', { exact: false })).toBeVisible(); await expect(row).not.toContainText(old)
    }
  } finally { await ownerContext.close(); await otherContext.close() }
})
