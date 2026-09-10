import { expect, test } from '@playwright/test'

test('profile and user search close the real two-user discovery loop in the user shell', async ({ browser }) => {
  test.setTimeout(90_000)
  const token = Date.now().toString(36)
  const username = `profile_${token}`
  const password = `password-${token}-123`
  const ownerContext = await browser.newContext()
  const viewerContext = await browser.newContext()
  const owner = await ownerContext.newPage()
  const viewer = await viewerContext.newPage()
  const register = async (page: typeof owner, name: string) => {
    await page.goto('/register')
    await page.getByLabel('用户名', { exact: true }).fill(name)
    await page.getByLabel('密码', { exact: true }).fill(password)
    await page.getByRole('button', { name: '注册并登录' }).click()
    await expect(page).toHaveURL(/\/posts$/)
  }
  try {
    await register(owner, username)
    await owner.getByRole('link', { name: '发布', exact: true }).click()
    await owner.getByLabel('标题', { exact: true }).fill(`profile story ${token}`)
    await owner.getByLabel('正文', { exact: true }).fill('A real user discovery story.')
    await owner.getByRole('button', { name: '发布', exact: true }).click()
    await expect(owner).toHaveURL(/\/posts\/\d+$/)
    const postURL = owner.url()
    // Warm the shared detail cache before changing the author's mutable name.
    await owner.reload()
    await expect(owner.getByRole('heading', { name: `profile story ${token}` })).toBeVisible()
    await owner.getByRole('link', { name: '我的资料' }).click()
    await owner.getByRole('button', { name: '编辑资料' }).click()
    await expect(owner.getByLabel('显示名称', { exact: true })).toHaveValue(username)
    await owner.getByLabel('显示名称', { exact: true }).fill(' ')
    await expect(owner.getByRole('button', { name: '保存资料' })).toBeDisabled()
    const displayName = `发现 ${token}`
    await owner.getByLabel('显示名称', { exact: true }).fill(` ${displayName} `)
    await owner.getByLabel('个人介绍', { exact: true }).fill('  这是我的简介  ')
    await owner.getByRole('button', { name: '保存资料' }).click()
    await expect(owner.getByRole('status')).toContainText('资料已更新')
    await owner.reload()
    await expect(owner.getByRole('heading', { name: displayName, exact: true })).toBeVisible()

    await register(viewer, `viewer_${token}`)
    await viewer.getByRole('link', { name: '搜索', exact: true }).click()
    await viewer.getByRole('tab', { name: '用户', exact: true }).focus()
    await viewer.keyboard.press('Enter')
    for (const query of [username, displayName, token]) {
      await viewer.getByLabel('搜索词', { exact: true }).fill(query)
      await viewer.getByRole('button', { name: '搜索', exact: true }).click()
      await expect(viewer.locator('.user-result').filter({ hasText: username })).toBeVisible()
    }
    await viewer.reload()
    await viewer.locator('.user-result').filter({ hasText: username }).getByRole('link').click()
    await expect(viewer.getByRole('heading', { name: displayName, exact: true })).toBeVisible()
    await expect(viewer.getByText('这是我的简介', { exact: true })).toBeVisible()
    await expect(viewer.getByRole('button', { name: '编辑资料' })).toHaveCount(0)
    await expect(viewer.getByRole('link', { name: `profile story ${token}`, exact: true })).toBeVisible()
    await viewer.goto(postURL)
    await expect(viewer.locator('.detail-card').getByText(displayName, { exact: true })).toBeVisible()
    await viewer.goto('/posts')
    await expect(viewer.locator('.post-card').filter({ hasText: `profile story ${token}` }).getByText(displayName, { exact: true })).toBeVisible()
    await viewer.goto(`/search?q=${token}`)
    await expect.poll(async () => {
      const response = await viewerContext.request.get(`/api/v1/search/posts?q=${token}`)
      const body = await response.json()
      return body.data?.some((post: { title: string }) => post.title === `profile story ${token}`) ?? false
    }, { timeout: 30_000 }).toBe(true)
    await viewer.reload()
    await expect(viewer.locator('.post-card').getByText(displayName, { exact: true })).toBeVisible()

    const base = new URL(viewer.url()).origin
    const api = viewerContext.request
    expect((await api.get(`${base}/api/v1/users/not_found_${token}`)).status()).toBe(404)
    expect((await api.patch(`${base}/api/v1/users/me/profile`, { data: { display_name: '' } })).status()).toBe(400)
    expect((await api.patch(`${base}/api/v1/users/me/profile`, { data: { display_name: 'Name', bio: '', username } })).status()).toBe(400)
    const publicData = (await (await api.get(`${base}/api/v1/users/${username}`)).json()).data
    expect(Object.keys(publicData).sort()).toEqual(['bio', 'created_at', 'display_name', 'following', 'id', 'is_self', 'username'])

    for (const [name, width, height] of [['desktop', 1440, 1000], ['tablet', 820, 1000], ['mobile', 390, 844]] as const) {
      await viewer.setViewportSize({ width, height })
      await viewer.goto('/posts')
      await expect(viewer.getByRole('tab', { name: '全部', exact: true })).toBeVisible()
      await expect(viewer.getByRole('tab', { name: 'Following', exact: true })).toBeEnabled()
      await expect(viewer.getByRole('link', { name: '我的资料' })).toBeVisible()
      await expect(viewer.locator('.user-context')).toBeVisible({ visible: name === 'desktop' })
      expect(await viewer.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await viewer.screenshot({ path: `test-results/profile-${name}.png`, fullPage: true })
    }
    await viewer.getByRole('button', { name: '退出', exact: true }).click()
    await expect(viewer).toHaveURL(/\/login$/)
    await viewer.goto(`/users/${username}`)
    await expect(viewer).toHaveURL(/\/login\?redirect=/)
    await viewer.getByLabel('用户名', { exact: true }).fill(`viewer_${token}`)
    await viewer.getByLabel('密码', { exact: true }).fill(password)
    await viewer.getByRole('button', { name: '登录', exact: true }).click()
    await expect(viewer.getByRole('heading', { name: displayName, exact: true })).toBeVisible()
  } finally { await ownerContext.close(); await viewerContext.close() }
})

test('user shell leaves the representative administrator layout isolated', async ({ page }) => {
  const username = process.env.GOPULSE_PROFILE_ADMIN_USER
  const password = process.env.GOPULSE_PROFILE_ADMIN_PASSWORD
  test.skip(!username || !password, 'Requires the isolated acceptance administrator account')
  await page.goto('/login')
  await page.getByLabel('用户名', { exact: true }).fill(username!)
  await page.getByLabel('密码', { exact: true }).fill(password!)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/posts$/)
  await page.getByRole('link', { name: '管理中心' }).click()
  await page.getByRole('link', { name: 'Metrics', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Plugin Metrics' })).toBeVisible()
  await expect(page.locator('.user-shell')).toHaveCount(0)
  await expect(page.locator('.admin-shell')).toBeVisible()
  for (const [name, width, height] of [['desktop', 1440, 1000], ['mobile', 390, 844]] as const) {
    await page.setViewportSize({ width, height })
    await page.screenshot({ path: `test-results/profile-admin-${name}.png`, fullPage: true })
  }
})
