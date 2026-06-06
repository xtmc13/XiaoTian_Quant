import { test, expect } from './fixtures'

/**
 * Core user journey: Login → Dashboard navigation → Strategy creation flow.
 */

test.describe('Authentication', () => {
  test('login page renders correctly', async ({ page }) => {
    await page.goto('/login')
    await expect(page.locator('text=登录')).toBeVisible()
    await expect(page.locator('input[type="text"]')).toBeVisible()
    await expect(page.locator('input[type="password"]')).toBeVisible()
    await expect(page.locator('button:has-text("登录")')).toBeEnabled()
  })

  test('login with invalid credentials shows error', async ({ page }) => {
    await page.goto('/login')
    await page.fill('input[type="text"]', 'invalid_user')
    await page.fill('input[type="password"]', 'wrong_password')
    await page.click('button:has-text("登录")')
    await expect(page.locator('text=登录失败')).toBeVisible({ timeout: 5000 })
  })
})

test.describe('Dashboard', () => {
  test('dashboard loads with KPI cards', async ({ authPage }) => {
    await expect(authPage.locator('text=总资产估值')).toBeVisible()
    await expect(authPage.locator('text=胜率')).toBeVisible()
    await expect(authPage.locator('text=最大回撤')).toBeVisible()
  })

  test('navigation sidebar links work', async ({ authPage }) => {
    const links = [
      { label: '策略', path: '/strategy' },
      { label: '机器人', path: '/bots' },
      { label: '合约交易', path: '/contract-trading' },
      { label: '投资组合', path: '/portfolio' },
    ]
    for (const { label, path } of links) {
      await authPage.click(`text=${label}`)
      await authPage.waitForURL(`**${path}`)
      await expect(authPage).toHaveURL(new RegExp(path))
    }
  })
})

test.describe('Strategy Flow', () => {
  test('strategy list page renders', async ({ authPage }) => {
    await authPage.goto('/strategy')
    await expect(authPage.locator('text=策略配置')).toBeVisible()
  })

  test('create strategy modal opens', async ({ authPage }) => {
    await authPage.goto('/strategy')
    await authPage.click('button:has-text("新建策略")')
    await expect(authPage.locator('text=创建策略')).toBeVisible()
  })
})

test.describe('Contract Trading', () => {
  test('trading page loads with order form', async ({ authPage }) => {
    await authPage.goto('/contract-trading')
    await expect(authPage.locator('text=开仓')).toBeVisible()
    await expect(authPage.locator('text=平仓')).toBeVisible()
  })
})
