import {expect,test} from '@playwright/test'
test('unknown plugin completion and failed configuration remain safe in audit DOM and bundles',async({page})=>{
 await page.goto('/login');await page.getByLabel('用户名').fill(process.env.GOPULSE_ADMIN_USERNAME!);await page.getByLabel('密码').fill(process.env.GOPULSE_ACCEPTANCE_PASSWORD!);await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page.locator('[data-section]')).toHaveCount(6)
 const canary=process.env.GOPULSE_SECRET_CANARY!
 const scripts=await page.locator('script[src]').evaluateAll(xs=>xs.map(x=>(x as HTMLScriptElement).src))
 for(const url of scripts){const text=await(await page.request.get(url)).text();expect(text.includes(canary)).toBe(false)}
 for(const path of ['','alerts','audit']){await page.goto('/admin/'+path);await expect(page.locator('.admin-content')).toBeVisible();expect((await page.locator('body').innerText()).includes(canary)).toBe(false)}
 await page.getByLabel('操作',{exact:true}).selectOption('plugin.stop');await page.getByLabel('结果',{exact:true}).selectOption('unknown');await page.getByRole('button',{name:'筛选审计'}).click();await expect(page.locator('tbody')).toContainText('requested');await expect(page.locator('tbody')).toContainText('unknown')
 await page.getByLabel('操作',{exact:true}).selectOption('plugin.configuration');await page.getByLabel('结果',{exact:true}).selectOption('failed');await page.getByRole('button',{name:'筛选审计'}).click();await expect(page.locator('tbody')).toContainText('completed');await expect(page.locator('tbody')).toContainText('operation_failed');expect((await page.locator('body').innerText()).includes(canary)).toBe(false)
})
