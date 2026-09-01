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
