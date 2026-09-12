import { expect,test,type Page } from '@playwright/test'
const password=process.env.GOPULSE_ACCEPTANCE_PASSWORD!
async function login(page:Page,user=process.env.GOPULSE_ADMIN_USERNAME!){await page.goto('/login');await page.getByLabel('用户名').fill(user);await page.getByLabel('密码').fill(password);await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(user===process.env.GOPULSE_USER_USERNAME?/\/posts$/:/\/admin\/$/)}
test('real overview, catalog rule lifecycle, users and audit',async({page})=>{
 test.setTimeout(180000)
 const origin=new URL(process.env.GOPULSE_BASE_URL!).origin
 page.on('request',r=>expect(new URL(r.url()).origin).toBe(origin))
 await login(page)
 await expect(page.locator('[data-section]')).toHaveCount(6)
 await expect(page.locator('[data-section="key_metrics"] dl')).toHaveCount(6)
 await expect(page.locator('[data-section="components"] dl')).toHaveCount(6)
 await expect(page.locator('[data-section="plugins"] dl')).toHaveCount(6)
 await expect(page.getByText('该分区响应无效，已隐藏未经验证的数据。')).toHaveCount(0)
 await page.goto('/admin/alerts')
 for(const source of ['metrics','logs','events']){
  await page.getByRole('button',{name:'创建规则'}).click()
  await page.getByLabel('名称',{exact:true}).fill('browser-'+source)
  await page.getByLabel('来源',{exact:true}).selectOption(source)
  if(source==='metrics')await page.getByLabel('指标',{exact:true}).selectOption('gopulse_backend_outbox_pending')
  await page.getByLabel('运算',{exact:true}).selectOption('gte')
  await page.getByLabel('阈值',{exact:true}).fill('0')
  await page.getByRole('button',{name:'保存规则'}).click()
  const row=page.locator('[data-rule-id]').filter({hasText:'browser-'+source})
  await expect(row).toHaveCount(1)
  await row.getByRole('button',{name:'编辑',exact:true}).click()
  await page.getByLabel('名称',{exact:true}).fill('browser-'+source+'-edited')
  page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'保存规则'}).click()
  await expect(row).toContainText('-edited')
  // Real second writer causes a revision conflict, not a mocked API response.
  await row.getByRole('button',{name:'编辑',exact:true}).click()
  const id=await row.getAttribute('data-rule-id')
  const read=await page.request.get('/api/v1/alerts/rules/'+id);const body=(await read.json()).data
  expect((await page.request.post('/api/v1/alerts/rules/'+id+'/disable',{data:{revision:body.revision},headers:{Origin:origin}})).status()).toBe(200)
  page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'保存规则'}).click()
  await expect(page.getByRole('status')).toContainText('已被其他管理员修改')
  await row.getByRole('button',{name:'启用',exact:true}).click()
  await expect(row.getByRole('button',{name:'停用',exact:true})).toBeVisible()
 }
 // Scheduler reads real VM/ES sources; all three created rules trigger on >=0.
 await expect.poll(async()=>{const r=await page.request.get('/api/v1/alerts/current');return (await r.json()).data.filter((x:{name:string})=>x.name.startsWith('browser-')).length},{timeout:60000}).toBe(3)
 await page.getByRole('button',{name:'current',exact:true}).click();await expect(page.locator('article')).toHaveCount(3)
 await page.getByRole('button',{name:'rules',exact:true}).click()
 const row=page.locator('[data-rule-id]').first();page.once('dialog',d=>d.accept());await row.getByRole('button',{name:'停用',exact:true}).click()
 await page.getByRole('button',{name:'history',exact:true}).click();await expect(page.locator('article').filter({hasText:'rule_disabled'})).toHaveCount(1)
 await page.goto('/admin/users');await page.getByLabel('精确用户 ID').fill(process.env.GOPULSE_USER_ID!);await page.getByRole('button',{name:'查询用户'}).click()
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'提升为超级管理员'}).click();await expect(page.getByRole('status')).toContainText('角色已变更')
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'降级为普通用户'}).click();await expect(page.getByRole('status')).toContainText('角色已变更')
 await page.getByLabel('精确用户 ID').fill(process.env.GOPULSE_ADMIN_ID!);await page.getByRole('button',{name:'查询用户'}).click();await expect(page.getByRole('button',{name:'降级为普通用户'})).toBeDisabled()
 await page.goto('/admin/audit');await expect(page.locator('tbody')).toContainText('rule.create');await expect(page.locator('tbody')).toContainText('user.role.change');await expect(page.locator('tbody')).toContainText('plugin.start')
 await page.getByLabel('资源',{exact:true}).selectOption('plugin');await page.getByRole('button',{name:'筛选审计'}).click();await expect(page.locator('tbody')).toContainText('requested');await expect(page.locator('tbody')).toContainText('completed')
})
test('self demotion clears management; ordinary user cannot mount new pages',async({page})=>{
 await login(page,process.env.GOPULSE_DEMOTION_USERNAME!)
 await page.goto('/admin/users');await page.getByLabel('精确用户 ID').fill(process.env.GOPULSE_DEMOTION_ID!);await page.getByRole('button',{name:'查询用户'}).click();page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'降级为普通用户'}).click();await expect(page).toHaveURL(/\/posts$/);await expect(page.locator('.admin-shell')).toHaveCount(0)
 expect((await page.request.get('/api/v1/admin/overview?range=15m')).status()).toBe(403)
 const requests:string[]=[];page.on('request',r=>{if(/\/api\/v1\/(admin|alerts)\//.test(r.url()))requests.push(r.url())})
 for(const path of ['','alerts','users','audit']){await page.goto('/admin/'+path);await expect(page).toHaveURL(/\/posts$/)}expect(requests).toEqual([])
})
