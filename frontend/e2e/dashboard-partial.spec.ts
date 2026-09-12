import {expect,test} from '@playwright/test'
test('live upstream failure remains local in dashboard DOM',async({page})=>{
 await page.goto('/login');await page.getByLabel('用户名').fill(process.env.GOPULSE_ADMIN_USERNAME!);await page.getByLabel('密码').fill(process.env.GOPULSE_ACCEPTANCE_PASSWORD!);await page.getByRole('button',{name:'登录',exact:true}).click()
 await expect(page.locator('[data-section]')).toHaveCount(6)
 for(const name of process.env.GOPULSE_PARTIAL_SECTIONS!.split(','))await expect(page.locator(`[data-section="${name}"] > p`).first()).toContainText(/degraded|unavailable/)
 await expect(page.locator('[data-section="alerts"] > p').first()).toContainText('healthy')
 await expect(page.locator('[data-section="alerts"]')).toContainText('warning_firing')
})
