import { test } from '@playwright/test'

// 手动调试 spec：需要真实网关（:8080）与可用账号，默认套件中跳过。
// 需要时显式开启：E2E_LIVE_BACKEND=1 npx playwright test e2e/gate-status.spec.ts
test.skip(!process.env.E2E_LIVE_BACKEND, '需要真实后端（设置 E2E_LIVE_BACKEND=1 运行）')

test('check gate status', async ({ page }) => {
  page.on('response', async (res) => {
    if (res.url().includes('/exchanges/configured')) {
      const body = await res.json()
      console.log('/exchanges/configured:', JSON.stringify(body, null, 2))
    }
  })

  await page.goto('http://localhost:5173/login')
  await page.getByPlaceholder('输入用户名').fill('admin')
  await page.getByPlaceholder('输入密码').fill('admin123')
  await page.click('button[type="submit"]')
  await page.waitForURL(/dashboard|markets/, { timeout: 10000 })
  await page.goto('http://localhost:5173/arbitrage/cross')
  await page.waitForLoadState('networkidle')
  await page.click('text=配置')
  await page.waitForTimeout(1000)

  const configured = await page.evaluate(async () => {
    const res = await fetch('/api/exchanges/configured', {
      headers: { Authorization: `Bearer ${localStorage.getItem('xt-token')}` },
    })
    return res.json()
  })
  console.log('FRONTEND FETCH /api/exchanges/configured:', JSON.stringify(configured, null, 2))

  await page.getByText('Gate.io').scrollIntoViewIfNeeded()
  await page.waitForTimeout(300)
  await page.screenshot({ path: 'gate-status.png', fullPage: true })
})
