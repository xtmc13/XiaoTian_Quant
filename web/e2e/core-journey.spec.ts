import { test, expect } from './fixtures'

/**
 * Core user journey: Login → Dashboard navigation → Strategy creation flow.
 */

test.describe('Authentication', () => {
  test('login page renders correctly', async ({ page }) => {
    await page.goto('/login')
    await expect(page.locator('text=小天量化')).toBeVisible()
    await expect(page.locator('input[type="text"]')).toBeVisible()
    await expect(page.locator('input[type="password"]')).toBeVisible()
    await page.fill('input[type="text"]', 'e2e_user')
    await page.fill('input[type="password"]', 'e2e_password')
    await expect(page.locator('button[type="submit"]:has-text("登录")')).toBeEnabled()
  })

  test('login with invalid credentials shows error', async ({ page }) => {
    await page.route('**/api/auth/login', async (route) => {
      await route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ message: '用户名或密码错误' }),
      })
    })
    await page.goto('/login')
    await page.fill('input[type="text"]', 'invalid_user')
    await page.fill('input[type="password"]', 'wrong_password')
    await page.click('button[type="submit"]:has-text("登录")')
    await expect(page.locator('text=用户名或密码错误')).toBeVisible({ timeout: 5000 })
  })
})

test.describe('Dashboard', () => {
  test('dashboard loads with KPI cards', async ({ authPage }) => {
    await expect(authPage.locator('text=总资产估值').first()).toBeVisible()
    await expect(authPage.locator('text=胜率').first()).toBeVisible()
    await expect(authPage.locator('text=最大回撤').first()).toBeVisible()
  })

  test('navigation sidebar links work', async ({ authPage }) => {
    const links = [
      { label: '仪表盘', path: '/dashboard' },
      { label: '策略', path: '/strategy' },
      { label: '回测', path: '/backtest' },
    ]
    for (const { label, path } of links) {
      const link = authPage.locator('nav').getByText(label).first()
      await link.scrollIntoViewIfNeeded()
      await link.click()
      await authPage.waitForURL(`**${path}`)
      await expect(authPage).toHaveURL(new RegExp(path))
    }
  })
})

test.describe('Strategy Flow', () => {
  test('strategy list page renders', async ({ authPage }) => {
    await authPage.goto('/strategy')
    await expect(authPage.locator('text=策略配置').first()).toBeVisible()
  })

  test('create strategy modal opens', async ({ authPage }) => {
    await authPage.goto('/strategy')
    await authPage.click('button:has-text("创建策略")')
    await expect(authPage.locator('text=创建策略').first()).toBeVisible()
  })
})

test.describe('Trading', () => {
  test('trading page loads with order form', async ({ authPage }) => {
    await authPage.goto('/trading')
    await expect(authPage.locator('text=限价').first()).toBeVisible()
    await expect(authPage.locator('text=市价').first()).toBeVisible()
  })
})
