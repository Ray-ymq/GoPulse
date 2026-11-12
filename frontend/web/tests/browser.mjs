import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const now = new Date().toISOString()
const me = { id: 1, username: 'linjian', role: 'user', created_at: now }
const author = { id: 2, username: 'shanye', display_name: '山野来信', following: false }
const self = { id: 1, username: me.username, display_name: '林间', following: false }
let profile = { ...me, display_name: '林间', bio: '把日子过成喜欢的样子。', following: false, is_self: true }
let sequence = 10, loseNextPublish = false, expire = false, failBookmarks = false
const created = (id, title, content, owner = author) => ({ id, title, content, author: { ...owner }, created_at: now, updated_at: now, edited_at: null, content_revision: 1, like_count: 2, comment_count: 0, liked_by_me: false, bookmarked_by_me: false })
let posts = [created(3, '在山野里，找回生活原本的节奏', '周末沿着湖边走了很久。#生活日常'), created(2, '好的设计，是让日常多一点心动', '一段留白，一次心动。#设计灵感'), created(1, '把桌面收拾好', '分享今天的桌面。#数码科技')]
const comments = new Map(), commands = new Map(), epochs = []
const notifications = [{ id: 1, type: 'post.liked', created_at: now, read_at: null, actor: author, post_id: 3, comment_id: null, resource_deleted: false }, { id: 2, type: 'user.followed', created_at: now, read_at: null, actor: author, post_id: null, comment_id: null, resource_deleted: false }]
const unexpected = [], mutations = []
function respond(res, data, status = 200, headers = {}) { res.writeHead(status, { 'Content-Type': 'application/json', ...headers }); res.end(status === 204 ? '' : JSON.stringify(data)) }
function data(res, value, status = 200) { respond(res, { data: value }, status) }
function paged(res, values, cursor = null) { respond(res, { data: values, meta: { next_cursor: cursor } }) }
function failure(res, status, code) { respond(res, { error: { code, message: code } }, status) }
const backend = createServer(async (req, res) => {
 try {
  const url = new URL(req.url, 'http://fixture'), path = url.pathname.replace('/api/v1', ''), method = req.method
  let raw = ''; for await (const part of req) raw += part
  const input = raw ? JSON.parse(raw) : null
  if (!['GET', 'HEAD'].includes(method)) {
   mutations.push(path)
   if (req.headers.origin !== `http://${req.headers.host}`) return failure(res, 403, 'permission_denied')
  }
  if (path === '/auth/login' || path === '/auth/register') {
   if (input.password !== 'password123') return failure(res, 401, 'invalid_credentials')
   expire = false; return respond(res, { data: me }, path.endsWith('register') ? 201 : 200, { 'Set-Cookie': 'gopulse_session=fixture; HttpOnly; SameSite=Lax; Path=/' })
  }
  if (path === '/auth/logout') return respond(res, undefined, 204, { 'Set-Cookie': 'gopulse_session=; Max-Age=0; Path=/' })
  if (expire || !req.headers.cookie?.includes('gopulse_session=fixture')) return failure(res, 401, 'authentication_required')
  if (path === '/users/me') return data(res, me)
  if (path === '/users/me/profile' && method === 'PATCH') { profile = { ...profile, ...input }; self.display_name = profile.display_name; return data(res, profile) }
  if (path === '/users/me/following' || path === '/users/me/followers') return paged(res, path.endsWith('following') && !author.following ? [] : [{ ...author, bio: '在路上，也在生活里', created_at: now, is_self: false }])
  if (path === '/users/2/follow') { author.following = method === 'PUT'; posts.forEach(p => { if (p.author.id === 2) p.author.following = author.following }); return data(res, { following: author.following }) }
  if (path === '/users/linjian') return data(res, profile)
  if (path === '/users/shanye') return data(res, { ...author, bio: '在路上，也在生活里', created_at: now, is_self: false })
  if (path === '/search/users') return paged(res, [{ ...author, bio: '在路上，也在生活里', created_at: now, is_self: false }])
  if (path === '/post-commands/context') return data(res, { data_epoch: '11111111-1111-4111-8111-111111111111' })
  if (path === '/post-commands/result') { const result = commands.get(req.headers['idempotency-key']); return result ? respond(res, result) : failure(res, 404, 'command_result_unknown') }
  if (path === '/posts' && method === 'POST') {
   const key = req.headers['idempotency-key']; assert.ok(key); assert.ok(req.headers['x-gopulse-data-epoch']); epochs.push(key)
   if (commands.has(key)) return respond(res, commands.get(key), 201)
   const post = created(++sequence, input.title, input.content, self); posts.unshift(post); const result = { data: post, meta: { resource_state: 'available', post_id: post.id, replayed: false } }; commands.set(key, result)
   if (loseNextPublish) { loseNextPublish = false; return failure(res, 503, 'command_outcome_unknown') }
   return respond(res, result, 201)
  }
  if (path === '/posts' && method === 'GET') return url.searchParams.get('cursor') ? paged(res, posts.slice(2)) : paged(res, posts.slice(0, 2), posts.length > 2 ? 'page-two' : null)
  if (path === '/posts/following') return paged(res, posts.filter(p => p.author.following))
  if (path === '/bookmarks') { if (failBookmarks) { failBookmarks = false; return failure(res, 503, 'backend_busy') }; return paged(res, posts.filter(p => p.bookmarked_by_me)) }
  if (path === '/search/posts') { const q = url.searchParams.get('q'); return paged(res, posts.filter(p => `${p.title} ${p.content}`.includes(q))) }
  if (/^\/users\/[^/]+\/posts$/.test(path)) { const username = path.split('/')[2]; return paged(res, posts.filter(p => p.author.username === username)) }
  if (path === '/notifications') return paged(res, notifications)
  if (/^\/notifications\/\d+\/read$/.test(path)) { notifications.find(n => n.id === Number(path.split('/')[2])).read_at = now; return respond(res, null, 204) }
  const match = path.match(/^\/posts\/(\d+)(?:\/(like|bookmark|comments))?$/)
  if (match) {
   const post = posts.find(p => p.id === Number(match[1])); if (!post) return failure(res, 404, 'post_not_found')
   const action = match[2]
   if (action === 'like') { post.liked_by_me = method === 'PUT'; post.like_count = post.liked_by_me ? 3 : 2; return respond(res, null, 204) }
   if (action === 'bookmark') { post.bookmarked_by_me = method === 'PUT'; return respond(res, null, 204) }
   if (action === 'comments') { const list = comments.get(post.id) || []; if (method === 'POST') { const comment = { id: ++sequence, post_id: post.id, author: me, content: input.content, created_at: now }; list.push(comment); comments.set(post.id, list); post.comment_count++; return data(res, comment, 201) }; return paged(res, list) }
   if (method === 'PATCH') { Object.assign(post, input, { edited_at: now, content_revision: post.content_revision + 1 }); return data(res, post) }
   if (method === 'DELETE') { posts = posts.filter(p => p.id !== post.id); return respond(res, null, 204) }
   return data(res, post)
  }
  unexpected.push(`${method} ${path}`); failure(res, 404, 'not_found')
 } catch (error) { unexpected.push(String(error)); failure(res, 500, 'internal_error') }
})
await new Promise(resolve => backend.listen(0, '127.0.0.1', resolve))
const apiPort = backend.address().port, webPort = Number(process.env.GOPULSE_E2E_PORT || 4187), base = `http://127.0.0.1:${webPort}`
const cwd = fileURLToPath(new URL('../', import.meta.url))
const vite = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', String(webPort)], { cwd, env: { ...process.env, GOPULSE_API_TARGET: `http://127.0.0.1:${apiPort}` }, stdio: 'pipe' })
let output = ''; vite.stdout.on('data', d => output += d); vite.stderr.on('data', d => output += d)
let browser
try {
 let ready = false
 for (let i = 0; i < 100; i++) { if (vite.exitCode !== null) throw new Error(output); try { ready = (await fetch(base)).ok } catch {} if (ready) break; await new Promise(r => setTimeout(r, 100)) }
 assert.ok(ready, output)
 const artifactDir = process.env.GOPULSE_E2E_ARTIFACT_DIR
 if (artifactDir) await mkdir(artifactDir, { recursive: true })
 browser = await chromium.launch({ headless: true, ...(process.env.GOPULSE_CHROMIUM_CHANNEL ? { channel: process.env.GOPULSE_CHROMIUM_CHANNEL } : {}), args: ['--no-sandbox'] })
 const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } }), errors = []
 page.on('pageerror', e => errors.push(e.message))
 await page.goto(base + '/bookmarks'); await page.waitForURL('**/login?**')
 await page.getByLabel('用户名', { exact: true }).fill('linjian'); await page.getByLabel('密码', { exact: true }).fill('wrongpass'); await page.getByRole('button', { name: '登录', exact: true }).click(); await page.getByRole('alert').filter({ hasText: '用户名或密码不正确' }).waitFor()
 await page.getByLabel('密码', { exact: true }).fill('password123'); await page.getByRole('button', { name: '登录', exact: true }).click(); await page.waitForURL('**/bookmarks'); await page.getByText('把喜欢留在这里', { exact: true }).waitFor()
 await page.locator('.nav button').filter({ hasText: '发现广场' }).click(); await page.locator('.feed .post').first().waitFor(); assert.equal(await page.locator('.feed .post').count(), 2)
 if (artifactDir) await page.screenshot({ path: `${artifactDir}/desktop.png`, fullPage: true })
 await page.getByRole('button', { name: '继续阅读', exact: true }).click(); await page.waitForFunction(() => document.querySelectorAll('.feed .post').length === 3)
 await page.getByRole('button', { name: '点赞', exact: true }).first().click(); await page.waitForFunction(() => document.querySelector('.feed .post button[aria-label="取消点赞"]'))
 await page.locator('.feed .post').first().getByRole('button', { name: '收藏', exact: true }).click(); await page.locator('.feed .post').first().getByRole('button', { name: '已收藏', exact: true }).waitFor()
 failBookmarks = true; await page.locator('.nav button').filter({ hasText: '我的收藏' }).click(); await page.getByRole('alert').filter({ hasText: '服务繁忙' }).waitFor(); await page.getByRole('button', { name: '重新加载', exact: true }).click(); await page.locator('.feed .post').waitFor(); assert.equal(await page.locator('.feed .post').count(), 1)
 await page.locator('#search').fill('不存在的词'); await page.locator('#search').press('Enter'); await page.getByText('暂时没有相关分享', { exact: true }).waitFor()
 await page.locator('#search').fill('山野'); await page.locator('#search').press('Enter'); await page.locator('.feed .post').waitFor(); await page.getByRole('link', { name: '用户', exact: true }).click(); await page.locator('.person-list .person').waitFor(); await page.locator('.person-list').getByRole('button', { name: '关注山野来信', exact: true }).click(); await page.locator('.person-list').getByRole('button', { name: '取消关注山野来信', exact: true }).waitFor()
 await page.locator('.nav button').filter({ hasText: '我的关注' }).click(); await page.locator('.feed .post').first().waitFor()
 await page.locator('.feed .post-title').first().click(); await page.locator('.post-detail').waitFor(); await page.getByLabel('写下评论', { exact: true }).fill('本地验证 <img src=x onerror="window.injected=true">'); await page.getByRole('button', { name: '发送评论', exact: true }).click(); await page.locator('.comment').waitFor(); assert.equal(await page.locator('.comment img').count(), 0); assert.equal(await page.evaluate(() => window.injected), undefined)
 await page.locator('.sidebar').getByRole('button', { name: '发布新帖子', exact: true }).click(); await page.locator('dialog[open]').getByLabel('帖子标题', { exact: true }).fill('我的第一篇分享'); await page.locator('dialog[open]').getByLabel('分享内容', { exact: true }).fill('一笔一墨，皆是日常。'); await page.getByRole('button', { name: '发布帖子', exact: true }).click(); await page.waitForURL(/\/posts\/\d+$/); await page.getByRole('heading', { name: '我的第一篇分享', exact: true }).waitFor()
 await page.getByRole('button', { name: '编辑帖子', exact: true }).click(); await page.locator('dialog[open]').getByLabel('帖子标题', { exact: true }).fill('修改后的分享'); await page.getByRole('button', { name: '保存修改', exact: true }).click(); await page.getByRole('heading', { name: '修改后的分享', exact: true }).waitFor()
 await page.getByRole('button', { name: '删除帖子', exact: true }).click(); await page.getByRole('button', { name: '确认删除', exact: true }).click(); await page.waitForURL('**/me'); await page.getByRole('button', { name: '编辑资料', exact: true }).click(); await page.getByLabel('显示名称', { exact: true }).fill('林间来信'); await page.getByLabel('个人简介', { exact: true }).fill('把生活写在纸上。'); await page.getByRole('button', { name: '保存资料', exact: true }).click(); await page.getByRole('heading', { name: '林间来信', exact: true }).waitFor()
 loseNextPublish = true; await page.locator('.sidebar').getByRole('button', { name: '发布新帖子', exact: true }).click(); await page.locator('dialog[open]').getByLabel('帖子标题', { exact: true }).fill('需要查证的分享'); await page.locator('dialog[open]').getByLabel('分享内容', { exact: true }).fill('模拟服务已提交但响应未确认。'); await page.getByRole('button', { name: '发布帖子', exact: true }).click(); await page.getByRole('button', { name: '查证发布结果', exact: true }).waitFor(); await page.getByRole('button', { name: '查证发布结果', exact: true }).click(); await page.getByRole('heading', { name: '需要查证的分享', exact: true }).waitFor(); assert.equal(posts.filter(p => p.title === '需要查证的分享').length, 1)
 await page.locator('.nav button').filter({ hasText: '消息通知' }).click(); await page.getByRole('button', { name: '当前消息标为已读', exact: true }).click(); await page.waitForFunction(() => !document.querySelector('.nav .badge'))
 await page.goto(base + '/posts/999'); await page.getByRole('alert').filter({ hasText: '这篇帖子已被删除或不存在' }).waitFor()
 await page.goto(base + '/'); await page.locator('.feed .post').first().waitFor()
 for (const width of [1440, 1024, 768, 390, 320]) { await page.setViewportSize({ width, height: 900 }); assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `页面在 ${width}px 下溢出`); if (width === 390) { if (artifactDir) await page.screenshot({ path: `${artifactDir}/mobile.png` }); const r = await page.locator('.sidebar').boundingBox(); assert.equal(Math.round(r.y + r.height), 900); await page.getByRole('button', { name: '发布新帖子', exact: true }).click(); await page.locator('dialog[open]').waitFor(); assert.equal(await page.evaluate(() => document.querySelector('dialog[open]').scrollWidth > document.querySelector('dialog[open]').clientWidth), false); await page.keyboard.press('Escape') } }
 await page.setViewportSize({ width: 1440, height: 1000 }); await page.reload(); await page.locator('.feed .post').first().waitFor()
 expire = true; await page.locator('.nav button').filter({ hasText: '我的收藏' }).click(); await page.waitForURL('**/login?**'); await page.getByLabel('用户名', { exact: true }).fill('linjian'); await page.getByLabel('密码', { exact: true }).fill('password123'); await page.getByRole('button', { name: '登录', exact: true }).click(); await page.waitForURL('**/bookmarks')
 await page.locator('.sidebar').getByRole('button', { name: '退出登录', exact: true }).click(); await page.locator('dialog[open]').getByRole('button', { name: '退出登录', exact: true }).click(); await page.waitForURL('**/login')
 await page.goto(base + '/not-a-page'); await page.getByText('这一页，尚未写下。', { exact: true }).waitFor()
 assert.deepEqual(errors, []); assert.deepEqual(unexpected, []); assert.ok(mutations.includes('/auth/login')); assert.equal(epochs.length, 2)
 console.log('浏览器验证通过：同源代理与会话 Cookie、登录回跳、列表分页、点赞收藏、错误重试、搜索与关注、评论转义、发帖查证、编辑删除、资料更新、消息已读、会话失效、退出及 320–1440px 响应式。')
} finally {
 await browser?.close(); vite.kill('SIGTERM'); backend.closeAllConnections(); await new Promise(resolve => backend.close(resolve))
}
