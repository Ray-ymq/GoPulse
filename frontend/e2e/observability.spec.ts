import { execFileSync } from 'node:child_process'
import { readFileSync, readlinkSync, realpathSync } from 'node:fs'
import { join } from 'node:path'
import { expect, test, type Browser, type Page } from '@playwright/test'

const adminUsername = process.env.GOPULSE_OBSERVABILITY_ADMIN_USERNAME
const demotionUsername = process.env.GOPULSE_OBSERVABILITY_DEMOTION_USERNAME
const userUsername = process.env.GOPULSE_OBSERVABILITY_USER_USERNAME
const password = process.env.GOPULSE_OBSERVABILITY_PASSWORD
const installPackage = process.env.GOPULSE_OBSERVABILITY_INSTALL_PACKAGE
const updatePackage = process.env.GOPULSE_OBSERVABILITY_UPDATE_PACKAGE
const logRequestID = process.env.GOPULSE_OBSERVABILITY_LOG_REQUEST_ID
const baseURL = process.env.GOPULSE_BASE_URL
const backendURL = process.env.GOPULSE_OBSERVABILITY_BACKEND_URL
const mysqlDatabase = process.env.GOPULSE_OBSERVABILITY_MYSQL_DATABASE
const mysqlRootPassword = process.env.GOPULSE_OBSERVABILITY_MYSQL_ROOT_PASSWORD

type Infrastructure = { project:string; envFile:string; composeFile:string; runDir:string }

function infrastructure(): Infrastructure | null {
  const project=process.env.GOPULSE_OBSERVABILITY_PROJECT, envFile=process.env.GOPULSE_OBSERVABILITY_ENV_FILE, composeFile=process.env.GOPULSE_OBSERVABILITY_COMPOSE_FILE, runDir=process.env.GOPULSE_OBSERVABILITY_RUN_DIR
  return project && envFile && composeFile && runDir ? {project,envFile,composeFile,runDir} : null
}
function compose(state: Infrastructure, ...args:string[]): string {
  return execFileSync('docker',['compose','--project-name',state.project,'--env-file',state.envFile,'--file',state.composeFile,...args],{encoding:'utf8'}).trim()
}
function ownedContainer(state: Infrastructure, service: string): string {
  const id=compose(state,'ps','-q',service)
  if (!id || execFileSync('docker',['inspect','--format','{{ index .Config.Labels "com.docker.compose.project" }}',id],{encoding:'utf8'}).trim() !== state.project) throw new Error(`${service} ownership verification failed`)
  return id
}
function verifyMonitorOwner(runDir:string): number {
  const record=JSON.parse(readFileSync(join(runDir,'monitor.json'),'utf8')) as {pid:number;startTicks:string;executablePath:string;workingDirectory:string;commandLineMarker:string}
  const stat=readFileSync(`/proc/${record.pid}/stat`,'utf8').trim(); const fields=stat.slice(stat.lastIndexOf(')')+2).split(/\s+/)
  if (fields[19] !== record.startTicks || realpathSync(readlinkSync(`/proc/${record.pid}/exe`)) !== realpathSync(record.executablePath) || realpathSync(readlinkSync(`/proc/${record.pid}/cwd`)) !== realpathSync(record.workingDirectory) || !readFileSync(`/proc/${record.pid}/cmdline`).toString().replaceAll('\0',' ').includes(record.commandLineMarker)) throw new Error('Monitor ownership verification failed')
  return record.pid
}
async function login(page: Page, username: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password!)
  await page.getByRole('button', { name:'登录', exact:true }).click()
  await expect(page).toHaveURL(/\/posts$/)
}
function trackUnexpectedBrowserOrigins(page: Page): string[] {
  const unexpected:string[]=[]
  const expected=baseURL ? new URL(baseURL).origin : ''
  page.on('request',(request)=>{ const url=new URL(request.url()); if ((url.protocol === 'http:' || url.protocol === 'https:') && url.origin !== expected) unexpected.push(request.url()) })
  return unexpected
}
async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}
async function backendStatus(path: string): Promise<{status:number; body:unknown}> {
  const response=await fetch(`${backendURL}${path}`)
  return {status:response.status,body:await response.json()}
}
async function createSocialPost(page: Page, marker: string): Promise<void> {
  await page.goto('/posts/new')
  await page.getByLabel('标题').fill(`Observability social ${marker}`)
  await page.getByLabel('正文').fill('This post proves that non-search social operations remain available.')
  await page.getByRole('button',{name:'发布',exact:true}).click()
  await expect(page).toHaveURL(/\/posts\/\d+$/)
  await page.getByPlaceholder('写下你的评论…').fill(`Comment ${marker}`)
  await page.getByRole('button',{name:'发布评论'}).click()
  await expect(page.getByText(`Comment ${marker}`)).toBeVisible()
  await page.getByRole('button',{name:'点赞',exact:true}).click()
  await expect(page.getByRole('button',{name:'取消点赞'})).toBeVisible()
}
async function seedLifecycleEvents(page: Page): Promise<void> {
  for (let index=0; index<26; index++) {
    const stopped=await page.request.post('/api/v1/exporter-plugins/redis-exporter/stop')
    expect(stopped.status()).toBe(200)
    const started=await page.request.post('/api/v1/exporter-plugins/redis-exporter/start')
    expect(started.status()).toBe(200)
  }
}
async function signInFromRedirect(page: Page, username: string, expectedPath: RegExp): Promise<void> {
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(password!)
  await page.getByRole('button',{name:'登录',exact:true}).click()
  await expect(page).toHaveURL(expectedPath)
}


test('ordinary user is isolated from every observability route and API', async ({ page }) => {
  test.skip(!userUsername || !password, 'observability user credentials are required')
  const unexpected=trackUnexpectedBrowserOrigins(page)
  const observed:string[]=[]
  page.on('request',(request)=>{ if (request.url().includes('/api/v1/observability/') || request.url().includes('/api/v1/exporter-plugins')) observed.push(request.url()) })
  await login(page,userUsername!)
  await expect(page.getByRole('link',{name:'可观测'})).toHaveCount(0)
  for (const path of ['/admin/observability','/admin/observability/metrics','/admin/observability/logs','/admin/observability/events','/admin/observability/exporters']) {
    await page.goto(path)
    await expect(page).toHaveURL(/\/forbidden$/)
    await expect(page.getByRole('heading',{name:'无权访问管理区域'})).toBeVisible()
  }
  expect(observed).toEqual([])
  for (const endpoint of ['observability/metrics?metric=gopulse_redis_up','observability/logs','observability/events','exporter-plugins','exporter-plugins/redis-exporter']) {
    const response=await page.request.get(`/api/v1/${endpoint}`)
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
  for (const endpoint of ['exporter-plugins/redis-exporter/start','exporter-plugins/redis-exporter/stop']) {
    const response=await page.request.post(`/api/v1/${endpoint}`)
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
  for (const endpoint of ['exporter-plugins/install','exporter-plugins/redis-exporter/update']) {
    const response=await page.request.post(`/api/v1/${endpoint}`,{multipart:{package:{name:'plugin.tar.gz',mimeType:'application/gzip',buffer:Buffer.from('denied')}}})
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
  expect(unexpected).toEqual([])
})

test('direct management login returns admin to the target and rejects an ordinary user before mounting it', async ({ browser }) => {
  test.skip(!adminUsername || !userUsername || !password, 'observability credentials are required')
  const adminContext=await browser.newContext(), userContext=await browser.newContext()
  try {
    const admin=await adminContext.newPage(); const adminRequests:string[]=[]
    admin.on('request',(request)=>{ if (request.url().includes('/api/v1/observability/')) adminRequests.push(request.url()) })
    await admin.goto('/admin/observability/logs')
    await expect(admin).toHaveURL(/\/login\?/)
    expect(new URL(admin.url()).searchParams.get('redirect')).toBe('/admin/observability/logs')
    await signInFromRedirect(admin,adminUsername!,/\/admin\/observability\/logs$/)
    await expect(admin.getByRole('heading',{name:'应用日志'})).toBeVisible()
    expect(adminRequests.length).toBeGreaterThan(0)

    const user=await userContext.newPage(); const userRequests:string[]=[]
    user.on('request',(request)=>{ if (request.url().includes('/api/v1/observability/')) userRequests.push(request.url()) })
    await user.goto('/admin/observability/events')
    await expect(user).toHaveURL(/\/login\?/)
    expect(new URL(user.url()).searchParams.get('redirect')).toBe('/admin/observability/events')
    await signInFromRedirect(user,userUsername!,/\/forbidden$/)
    await expect(user.getByRole('heading',{name:'无权访问管理区域'})).toBeVisible()
    expect(userRequests).toEqual([])
  } finally { await adminContext.close(); await userContext.close() }
})

test('administrator completes the real exporter management loop with install, query, operation, and update', async ({ page }) => {
  test.setTimeout(180_000)
  test.skip(!adminUsername || !password || !installPackage || !updatePackage, 'observability admin credentials and packages are required')
  const unexpected=trackUnexpectedBrowserOrigins(page)
  await login(page,adminUsername!)
  await createSocialPost(page,'normal')
  await page.getByRole('link',{name:'可观测'}).click()
  await expect(page.getByRole('heading',{name:'可观测总览'})).toBeVisible()
  await expect(page.getByTestId('exporter-region').getByText('未安装',{exact:true})).toBeVisible()
  await expect(page.getByTestId('metrics-region').getByText('暂无样本',{exact:true})).toBeVisible()

  await page.getByRole('link',{name:'Exporter',exact:true}).click()
  await expect(page.getByRole('heading',{name:'安装 Redis Exporter'})).toBeVisible()
  const fileInput=page.locator('input[type=file]')
  await fileInput.setInputFiles(installPackage!)
  await page.getByRole('button',{name:'安装并启动'}).click()
  await expect(page.locator('.state-pill')).toHaveText('running',{timeout:30_000})
  await expect(page.getByText(/v1\.8\.2/)).toBeVisible()
  await expect(fileInput).toHaveValue('')

  const lastSuccess=page.locator('.exporter-details > div').filter({hasText:'最近成功'}).locator('strong')
  await expect.poll(async()=>{ await page.getByRole('button',{name:'刷新状态'}).click(); await page.waitForTimeout(500); return lastSuccess.textContent() },{timeout:30_000}).not.toBe('—')
  page.once('dialog',(dialog)=>dialog.accept())
  await page.getByRole('button',{name:'停止',exact:true}).click()
  await expect(page.locator('.state-pill')).toHaveText('stopped',{timeout:20_000})
  await expect(page.getByRole('button',{name:'停止',exact:true})).toBeDisabled()
  await page.getByRole('button',{name:'启动',exact:true}).click()
  await expect(page.locator('.state-pill')).toHaveText('running',{timeout:20_000})
  await expect(page.getByRole('button',{name:'启动',exact:true})).toBeDisabled()

  await fileInput.setInputFiles(updatePackage!)
  page.once('dialog',(dialog)=>dialog.accept())
  await page.getByRole('button',{name:'确认更新'}).click()
  await expect(page.getByText(/v1\.8\.3/)).toBeVisible({timeout:30_000})
  await expect(page.locator('.state-pill')).toHaveText('running')
  await expect(fileInput).toHaveValue('')

  await fileInput.setInputFiles(installPackage!)
  page.once('dialog',(dialog)=>dialog.accept())
  await page.getByRole('button',{name:'确认更新'}).click()
  await expect(page.getByText('当前状态不允许执行该操作。')).toBeVisible()
  await expect(page.getByText(/v1\.8\.3/)).toBeVisible()
  await expect(fileInput).toHaveValue('')

  await page.getByRole('link',{name:'Metrics',exact:true}).click()
  await expect.poll(async()=>{ await page.getByRole('button',{name:'刷新',exact:true}).click(); await page.waitForTimeout(750); return page.locator('.series-card').count() },{timeout:60_000}).toBeGreaterThan(0)
  const metricRequest=page.waitForRequest((request)=>request.url().includes('/api/v1/observability/metrics?') && request.url().includes('gopulse_redis_cpu_seconds_total'))
  await page.getByLabel('指标').selectOption('gopulse_redis_cpu_seconds_total')
  await page.getByLabel('范围').selectOption('1h')
  await page.getByRole('button',{name:'应用',exact:true}).click()
  const request=await metricRequest; const query=new URL(request.url()).searchParams
  expect([...query.keys()].sort()).toEqual(['metric','range'])
  expect(query.get('metric')).toBe('gopulse_redis_cpu_seconds_total'); expect(query.get('range')).toBe('1h')
  await expect(page.getByText('60s',{exact:true})).toBeVisible()
  await expect.poll(async()=>{ if (await page.locator('.series-card').count()) return 1; await page.getByRole('button',{name:'应用',exact:true}).click(); await page.waitForTimeout(750); return page.locator('.series-card').count() },{timeout:60_000}).toBeGreaterThan(0)
  await expect(page.locator('.series-card strong').first()).toContainText('mode=')
  await page.getByRole('link',{name:'返回社交'}).click(); await expect(page).toHaveURL(/\/posts$/)
  expect(unexpected).toEqual([])
})

test('Logs and Events support real filters, pagination, cursor recovery, and DTO-only rendering', async ({ page }) => {
  test.setTimeout(240_000)
  test.skip(!adminUsername || !password || !logRequestID, 'observability query seed is required')
  await login(page,adminUsername!)
  await page.goto('/admin/observability/logs')
  await page.getByLabel('服务').selectOption('backend')
  await page.getByLabel('模块').selectOption('http')
  await page.getByLabel('固定消息').selectOption('http request completed')
  await page.getByRole('button',{name:'应用筛选'}).click()
  await expect.poll(async()=>{ if (await page.getByRole('button',{name:'加载更多'}).count()) return 1; await page.getByRole('button',{name:'刷新',exact:true}).click(); await page.waitForTimeout(500); return 0 },{timeout:45_000}).toBe(1)
  await expect(page.locator('.record-card')).toHaveCount(50)
  await page.getByRole('button',{name:'加载更多'}).click()
  await expect.poll(()=>page.locator('.record-card').count()).toBeGreaterThan(50)
  const logCards=await page.locator('.record-card').allTextContents(); expect(new Set(logCards).size).toBe(logCards.length)
  await page.getByRole('textbox',{name:'Request ID',exact:true}).fill(logRequestID!)
  await page.getByRole('button',{name:'应用筛选'}).click()
  await expect(page.locator('.record-card')).toHaveCount(1)
  await expect(page.getByText(logRequestID!,{exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'加载更多'})).toHaveCount(0)

  await page.getByRole('textbox',{name:'Request ID',exact:true}).fill('')
  await page.getByRole('button',{name:'应用筛选'}).click()
  await expect(page.getByRole('button',{name:'加载更多'})).toBeVisible()
  await page.route('**/api/v1/observability/logs?cursor=*',async(route)=>{ const url=new URL(route.request().url()); url.searchParams.set('cursor','invalid'); await route.continue({url:url.toString()}) },{times:1})
  await page.getByRole('button',{name:'加载更多'}).click()
  await expect(page.getByText('分页游标已失效，请刷新首页结果。')).toBeVisible()
  await expect(page.getByRole('button',{name:'加载更多'})).toHaveCount(0)
  await page.unroute('**/api/v1/observability/logs?cursor=*')
  expect(await page.locator('.metadata-list dt').allTextContents()).not.toEqual(expect.arrayContaining(['_index','_id','_score','pit','cursor','kafka','envelope']))

  await seedLifecycleEvents(page)
  await page.goto('/admin/observability/events')
  await expect.poll(async()=>{ if (await page.getByRole('button',{name:'加载更多'}).count()) return 1; await page.getByRole('button',{name:'刷新',exact:true}).click(); await page.waitForTimeout(750); return 0 },{timeout:60_000}).toBe(1)
  await expect(page.locator('.record-card')).toHaveCount(50)
  await page.getByRole('button',{name:'加载更多'}).click()
  await expect.poll(()=>page.locator('.record-card').count()).toBeGreaterThan(50)
  const eventCards=await page.locator('.record-card').allTextContents(); expect(new Set(eventCards).size).toBe(eventCards.length)
  await page.getByLabel('事件').selectOption('exporter_plugin_updated')
  await page.getByRole('button',{name:'应用筛选'}).click()
  await expect(page.getByText('exporter_plugin_updated',{exact:true}).first()).toBeVisible()
  await expect(page.getByRole('button',{name:'加载更多'})).toHaveCount(0)

  await page.getByLabel('事件').selectOption('')
  await page.getByLabel('严重度').selectOption('')
  await page.getByRole('button',{name:'应用筛选'}).click()
  await expect(page.getByRole('button',{name:'加载更多'})).toBeVisible()
  await page.route('**/api/v1/observability/events?cursor=*',async(route)=>{ const url=new URL(route.request().url()); url.searchParams.set('cursor','invalid'); await route.continue({url:url.toString()}) },{times:1})
  await page.getByRole('button',{name:'加载更多'}).click()
  await expect(page.getByText('分页游标已失效，请刷新首页结果。')).toBeVisible()
  await page.unroute('**/api/v1/observability/events?cursor=*')
  expect(await page.locator('.metadata-list dt').allTextContents()).not.toEqual(expect.arrayContaining(['_index','_id','_score','pit','cursor','message_id','kafka','envelope']))
})

test('overview isolates and recovers VictoriaMetrics, Monitor, and Elasticsearch failures while social APIs stay available', async ({ page }) => {
  test.setTimeout(240_000)
  const state=infrastructure()
  test.skip(!adminUsername || !password || !state || !backendURL, 'isolated infrastructure controls are required')
  if (!/^gopulse-observability-[a-f0-9]{12}$/.test(state!.project)) throw new Error('unsafe Compose project')
  const unexpected=trackUnexpectedBrowserOrigins(page)
  await login(page,adminUsername!); await page.goto('/admin/observability')
  const metrics=page.getByTestId('metrics-region'), logs=page.getByTestId('logs-region'), events=page.getByTestId('events-region'), exporter=page.getByTestId('exporter-region')
  await expect(exporter.getByText('running',{exact:true})).toBeVisible({timeout:20_000})
  await expect(logs.locator('.notice')).toHaveCount(0); await expect(events.locator('.notice')).toHaveCount(0)

  ownedContainer(state!,'victoriametrics')
  try {
    compose(state!,'stop','victoriametrics')
    await metrics.getByRole('button',{name:'重试'}).click()
    await expect(metrics.getByText(/Metrics 暂时不可用/)).toBeVisible({timeout:10_000})
    await expect(exporter.getByText('running',{exact:true})).toBeVisible(); await expect(logs.locator('.notice')).toHaveCount(0); await expect(events.locator('.notice')).toHaveCount(0)
    expect((await backendStatus('/health')).status).toBe(200); expect((await backendStatus('/ready')).status).toBe(200)
  } finally { compose(state!,'start','victoriametrics') }
  await expect.poll(async()=>{ await metrics.getByRole('button',{name:'重试'}).click(); await page.waitForTimeout(500); return metrics.locator('.notice').count() },{timeout:30_000}).toBe(0)

  const monitorPID=verifyMonitorOwner(state!.runDir)
  try {
    process.kill(monitorPID,'SIGSTOP')
    await exporter.getByRole('button',{name:'重试'}).click()
    await expect(exporter.getByText(/Monitor 暂时不可用/)).toBeVisible({timeout:10_000})
    await expect(metrics.locator('.notice')).toHaveCount(0); await expect(logs.locator('.notice')).toHaveCount(0); await expect(events.locator('.notice')).toHaveCount(0)
    expect((await backendStatus('/health')).status).toBe(200); expect((await backendStatus('/ready')).status).toBe(200)
  } finally { process.kill(monitorPID,'SIGCONT') }
  await expect.poll(async()=>{ await exporter.getByRole('button',{name:'重试'}).click(); await page.waitForTimeout(500); return exporter.getByText('running',{exact:true}).count() },{timeout:30_000}).toBe(1)

  ownedContainer(state!,'elasticsearch')
  try {
    compose(state!,'stop','elasticsearch')
    await logs.getByRole('button',{name:'重试'}).click(); await events.getByRole('button',{name:'重试'}).click()
    await expect(logs.getByText(/Logs 暂时不可用/)).toBeVisible({timeout:15_000}); await expect(events.getByText(/Events 暂时不可用/)).toBeVisible({timeout:15_000})
    await expect(metrics.locator('.notice')).toHaveCount(0); await expect(exporter.getByText('running',{exact:true})).toBeVisible()
    expect((await backendStatus('/health')).status).toBe(200)
    const ready=await backendStatus('/ready'); expect(ready.status).toBe(503); expect(ready.body).toMatchObject({checks:{elasticsearch:'down'}})
    await createSocialPost(page,'es-down')
  } finally { compose(state!,'start','elasticsearch') }
  await expect.poll(async()=> (await backendStatus('/ready')).status,{timeout:60_000}).toBe(200)
  await page.goto('/admin/observability')
  await expect.poll(async()=>{ await page.getByTestId('logs-region').getByRole('button',{name:'重试'}).click(); await page.waitForTimeout(500); return page.getByTestId('logs-region').locator('.notice').count() },{timeout:30_000}).toBe(0)
  await expect.poll(async()=>{ await page.getByTestId('events-region').getByRole('button',{name:'重试'}).click(); await page.waitForTimeout(500); return page.getByTestId('events-region').locator('.notice').count() },{timeout:30_000}).toBe(0)
  expect(unexpected).toEqual([])
})

test('management pages remain keyboard-readable, narrow-screen usable, and reject malformed upstream-shaped browser data', async ({ page }) => {
  test.skip(!adminUsername || !password, 'observability admin credentials are required')
  await login(page,adminUsername!)
  await page.setViewportSize({width:375,height:812})
  for (const path of ['/admin/observability','/admin/observability/metrics','/admin/observability/logs','/admin/observability/events','/admin/observability/exporters']) {
    await page.goto(path); await expect(page.locator('.admin-content')).toBeVisible(); await expectNoHorizontalOverflow(page)
  }
  await page.goto('/admin/observability/metrics')
  await expect(page.getByLabel('指标')).toBeVisible(); await expect(page.getByLabel('范围')).toBeVisible()
  await page.keyboard.press('Tab'); expect(await page.evaluate(()=>document.activeElement?.tagName)).not.toBe('BODY')
  await page.goto('/admin/observability/exporters')
  await expect(page.locator('section[aria-busy]')).toBeVisible(); await expect(page.locator('input[type=file]')).toBeVisible()

  const sentinel='<img src=x onerror="window.__gopulseSentinel=1">'
  await page.route('**/api/v1/observability/logs?*',async(route)=>route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({data:[{timestamp:new Date().toISOString(),level:'info',service:'backend',module:'http',message:'http request completed',unknown:sentinel}],meta:{next_cursor:null}})}),{times:1})
  await page.goto('/admin/observability/logs')
  await expect(page.getByText('查询失败，请稍后重试。')).toBeVisible()
  await expect(page.getByText(sentinel,{exact:true})).toHaveCount(0)
  expect(await page.evaluate(()=>(window as typeof window & {__gopulseSentinel?:number}).__gopulseSentinel)).toBeUndefined()
})

test('database role demotion clears the management view while preserving the social session', async ({ page }) => {
  const state=infrastructure()
  test.skip(!demotionUsername || !password || !state || !mysqlDatabase || !mysqlRootPassword || !installPackage, 'demotion controls are required')
  if (!/^[A-Za-z0-9_]{3,32}$/.test(demotionUsername!)) throw new Error('unsafe demotion username')
  await login(page,demotionUsername!)
  await page.goto('/admin/observability/exporters')
  await expect(page.locator('.state-pill')).toHaveText('running',{timeout:20_000})
  await page.locator('input[type=file]').setInputFiles(installPackage!)
  compose(state!,'exec','-T','mysql','mysql','--user=root',`--password=${mysqlRootPassword}`,mysqlDatabase!,'--execute',`UPDATE users SET role='user' WHERE username='${demotionUsername}' AND role='admin'`)
  await page.getByRole('button',{name:'刷新状态'}).click()
  await expect(page).toHaveURL(/\/forbidden$/)
  await expect(page.getByRole('heading',{name:'无权访问管理区域'})).toBeVisible()
  await expect(page.getByText('redis-exporter',{exact:false})).toHaveCount(0)
  await page.goto('/posts'); await expect(page).toHaveURL(/\/posts$/); await expect(page.getByText(`@${demotionUsername}`)).toBeVisible()
  expect(await page.evaluate(()=>({local:Object.keys(localStorage),session:Object.keys(sessionStorage)}))).toEqual({local:[],session:[]})
})
