import { execFileSync } from 'node:child_process'
import { readFileSync, readlinkSync, realpathSync } from 'node:fs'
import { join } from 'node:path'
import { expect, test } from '@playwright/test'

const adminUsername = process.env.GOPULSE_OBSERVABILITY_ADMIN_USERNAME
const userUsername = process.env.GOPULSE_OBSERVABILITY_USER_USERNAME
const password = process.env.GOPULSE_OBSERVABILITY_PASSWORD
const updatePackage = process.env.GOPULSE_OBSERVABILITY_UPDATE_PACKAGE

async function login(page: import('@playwright/test').Page, username: string): Promise<void> {
  await page.goto('/login'); await page.getByLabel('用户名').fill(username); await page.getByLabel('密码').fill(password!); await page.getByRole('button', { name:'登录', exact:true }).click(); await expect(page).toHaveURL(/\/posts$/)
}

test('ordinary user is isolated from observability and exporter routes and APIs', async ({ page }) => {
  test.skip(!userUsername || !password, 'observability user credentials are required')
  const observed: string[] = []
  page.on('request', (request) => { if (request.url().includes('/api/v1/observability/') || request.url().includes('/api/v1/exporter-plugins')) observed.push(request.url()) })
  await login(page, userUsername!)
  await expect(page.getByRole('link', { name:'可观测' })).toHaveCount(0)
  await page.goto('/admin/observability/exporters'); await expect(page).toHaveURL(/\/forbidden$/); await expect(page.getByRole('heading', { name:'无权访问管理区域' })).toBeVisible()
  expect(observed).toEqual([])
  for (const endpoint of ['observability/metrics?metric=gopulse_redis_up','observability/logs','observability/events','exporter-plugins','exporter-plugins/redis-exporter']) {
    const response = await page.request.get(`/api/v1/${endpoint}`)
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
  for (const endpoint of ['exporter-plugins/redis-exporter/start','exporter-plugins/redis-exporter/stop']) {
    const response = await page.request.post(`/api/v1/${endpoint}`)
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
  for (const endpoint of ['exporter-plugins/install','exporter-plugins/redis-exporter/update']) {
    const response = await page.request.post(`/api/v1/${endpoint}`, { multipart:{ package:{ name:'plugin.tar.gz', mimeType:'application/gzip', buffer:Buffer.from('denied') } } })
    expect(response.status()).toBe(403); expect((await response.json()).error.code).toBe('permission_denied')
  }
})

test('administrator completes overview and real exporter management loop', async ({ page }) => {
  test.setTimeout(120_000)
  test.skip(!adminUsername || !password || !updatePackage, 'observability admin credentials and update package are required')
  await login(page, adminUsername!)
  await page.getByRole('link', { name:'可观测' }).click(); await expect(page).toHaveURL(/\/admin\/observability$/)
  await expect(page.getByRole('heading', { name:'可观测总览' })).toBeVisible()
  for (const heading of ['Redis 可用状态','最近日志','最近事件','当前事实']) await expect(page.getByRole('heading',{name:heading})).toBeVisible()
  await expect(page.getByText('running', { exact:true })).toBeVisible({ timeout:15_000 })

  await page.getByRole('link', { name:'管理 Exporter' }).click(); await expect(page).toHaveURL(/\/admin\/observability\/exporters$/)
  await expect(page.getByRole('heading', { name:'Redis Exporter', exact:true })).toBeVisible()
  await expect(page.locator('.state-pill')).toHaveText('running', { timeout:15_000 })

  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name:'停止', exact:true }).click()
  await expect(page.locator('.state-pill')).toHaveText('stopped', { timeout:20_000 })
  await page.getByRole('button', { name:'启动', exact:true }).click()
  await expect(page.locator('.state-pill')).toHaveText('running', { timeout:20_000 })

  await page.locator('input[type=file]').setInputFiles(updatePackage!)
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name:'确认更新' }).click()
  await expect(page.getByText(/v1\.8\.3/)).toBeVisible({ timeout:30_000 })
  await expect(page.locator('.state-pill')).toHaveText('running')

  await page.getByRole('link', { name:'Events', exact:true }).click()
  await page.getByLabel('事件').selectOption('exporter_plugin_updated')
  await page.getByRole('button', { name:'应用筛选' }).click()
  await expect.poll(async () => { await page.getByRole('button', { name:'刷新', exact:true }).click(); await page.waitForTimeout(500); return page.locator('.record-card').count() }, { timeout:20_000 }).toBeGreaterThan(0)
  await expect(page.getByText('exporter_plugin_updated', { exact:true }).first()).toBeVisible()

  await page.getByRole('link', { name:'Metrics', exact:true }).click(); await expect(page.locator('.series-card').first()).toBeVisible({ timeout:15_000 })
  await page.getByRole('link', { name:'返回社交' }).click(); await expect(page).toHaveURL(/\/posts$/)
})


function infrastructure(): { project:string; envFile:string; composeFile:string; runDir:string } | null {
  const project=process.env.GOPULSE_OBSERVABILITY_PROJECT, envFile=process.env.GOPULSE_OBSERVABILITY_ENV_FILE, composeFile=process.env.GOPULSE_OBSERVABILITY_COMPOSE_FILE, runDir=process.env.GOPULSE_OBSERVABILITY_RUN_DIR
  return project && envFile && composeFile && runDir ? {project,envFile,composeFile,runDir} : null
}
function compose(state: ReturnType<typeof infrastructure>, ...args:string[]): string {
  return execFileSync('docker',['compose','--project-name',state!.project,'--env-file',state!.envFile,'--file',state!.composeFile,...args],{encoding:'utf8'}).trim()
}
function verifyMonitorOwner(runDir:string): number {
  const record=JSON.parse(readFileSync(join(runDir,'monitor.json'),'utf8')) as {pid:number;startTicks:string;executablePath:string;workingDirectory:string;commandLineMarker:string}
  const stat=readFileSync(`/proc/${record.pid}/stat`,'utf8').trim(); const fields=stat.slice(stat.lastIndexOf(')')+2).split(/\s+/)
  if (fields[19] !== record.startTicks || realpathSync(readlinkSync(`/proc/${record.pid}/exe`)) !== realpathSync(record.executablePath) || realpathSync(readlinkSync(`/proc/${record.pid}/cwd`)) !== realpathSync(record.workingDirectory) || !readFileSync(`/proc/${record.pid}/cmdline`).toString().replaceAll('\0',' ').includes(record.commandLineMarker)) throw new Error('Monitor ownership verification failed')
  return record.pid
}

test('overview isolates and recovers real VictoriaMetrics and Monitor failures', async ({ page }) => {
  test.setTimeout(120_000)
  const state=infrastructure()
  test.skip(!adminUsername || !password || !state, 'isolated infrastructure controls are required')
  if (!/^gopulse-observability-[a-f0-9]{12}$/.test(state!.project)) throw new Error('unsafe Compose project')
  await login(page,adminUsername!); await page.goto('/admin/observability')
  const metrics=page.getByTestId('metrics-region'), exporter=page.getByTestId('exporter-region')
  await expect(exporter.getByText('running',{exact:true})).toBeVisible({timeout:15_000})
  const vmID=compose(state,'ps','-q','victoriametrics')
  if (!vmID || execFileSync('docker',['inspect','--format','{{ index .Config.Labels "com.docker.compose.project" }}',vmID],{encoding:'utf8'}).trim() !== state!.project) throw new Error('VictoriaMetrics ownership verification failed')
  try {
    compose(state,'stop','victoriametrics')
    await metrics.getByRole('button',{name:'重试'}).click()
    await expect(metrics.getByText(/Metrics 暂时不可用/)).toBeVisible({timeout:10_000})
    await expect(exporter.getByText('running',{exact:true})).toBeVisible()
  } finally { compose(state,'start','victoriametrics') }
  await expect.poll(async()=>{ await metrics.getByRole('button',{name:'重试'}).click(); await page.waitForTimeout(500); return metrics.locator('.notice').count() },{timeout:20_000}).toBe(0)

  const monitorPID=verifyMonitorOwner(state!.runDir)
  try {
    process.kill(monitorPID,'SIGSTOP')
    await exporter.getByRole('button',{name:'重试'}).click()
    await expect(exporter.getByText(/Monitor 暂时不可用/)).toBeVisible({timeout:10_000})
    await expect(metrics.locator('.notice')).toHaveCount(0)
  } finally { process.kill(monitorPID,'SIGCONT') }
  await expect.poll(async()=>{ await exporter.getByRole('button',{name:'重试'}).click(); await page.waitForTimeout(500); return exporter.getByText('running',{exact:true}).count() },{timeout:20_000}).toBe(1)
})
