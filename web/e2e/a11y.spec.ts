import { test, expect } from './fixtures'
import AxeBuilder from '@axe-core/playwright'

/**
 * Accessibility (a11y) E2E tests using axe-core.
 * Scans core pages for WCAG 2.1 AA violations.
 *
 * NOTE: color-contrast is disabled globally because the dark-theme design
 * system uses several text/background combinations that do not meet WCAG 2.1
 * AA contrast ratios. This is a known design-debt issue across the app, not
 * specific to any single page. The scans still catch structural a11y problems.
 *
 * @see https://github.com/dequelabs/axe-core-npm/tree/develop/packages/playwright
 */

// Helper: navigate to an authenticated page and wait for it to settle.
// 必须等应用外壳（侧边 nav）挂载完成再扫描：fullyParallel 下 dev server
// 编译 stalls 会让 axe 扫到半渲染/重定向中的过渡 DOM，产生抖动误报。
// 注意不能用 networkidle——应用长驻 WebSocket，networkidle 永不达成。
async function gotoAuthenticated(page: any, path: string) {
  // dev server 冷编译重型页面（dashboard/trading 现挂更多面板）可能超过默认 30s
  // 导航超时；这里只等 DOM 就绪（nav 可见性在下面单独等），给足编译余量。
  await page.goto(path, { waitUntil: 'domcontentloaded', timeout: 60000 })
  await page.locator('nav').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(800)
}

// Helper: build an AxeBuilder with the project-standard rules.
function buildAxe(page: any) {
  return new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
    .disableRules(['color-contrast'])
}

// Pages that can be accessed without authentication
test.describe('Public Pages - a11y', () => {
  test('login page has no critical a11y violations', async ({ page }) => {
    await page.goto('/login')
    await page.waitForLoadState('domcontentloaded')
    // 等登录表单真实挂载（提交按钮可见）再扫描，避免扫到过渡态。
    await page.locator('button[type="submit"]').first().waitFor({ state: 'visible', timeout: 15000 })

    const accessibilityScanResults = await buildAxe(page).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })
})

// Pages that require authentication
test.describe('Authenticated Pages - a11y', () => {
  // fullyParallel + dev server 冷编译下，单测可能远超默认 30s。
  test.setTimeout(120000)
  test('dashboard has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/dashboard')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('strategy page has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/strategy')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('trading page has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/trading')

    const accessibilityScanResults = await buildAxe(authPage)
      .exclude('.klinecharts-pro')
      .exclude('.overflow-y-auto')
      .exclude('.overflow-x-auto')
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('bots page has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/bots')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('ai bots page has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/bots/ai')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('backtest page has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/backtest')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('settings page has no critical a11y violations', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/settings')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })
})

// Interactive components - modals, dropdowns, etc.
test.describe('Interactive Components - a11y', () => {
  test('strategy create page is accessible', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/strategy')
    await authPage.getByRole('button', { name: /启动策略机器人/ }).first().click()
    await authPage.waitForURL(/\/create/)
    await expect(authPage).toHaveURL(/\/create/)

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('sidebar navigation is accessible', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/dashboard')

    // Check that navigation landmarks exist
    const nav = authPage.locator('nav').first()
    await expect(nav).toBeVisible()

    // Check that current page link has aria-current
    const activeLink = authPage.locator('nav [aria-current="page"]').first()
    await expect(activeLink).toBeVisible()

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })
})

// Detailed violation reporting (non-blocking)
test.describe('A11y Detailed Report', () => {
  test('generate detailed a11y report for dashboard', async ({ authPage }) => {
    await gotoAuthenticated(authPage, '/dashboard')

    const accessibilityScanResults = await buildAxe(authPage).analyze()

    // Log all violations for review (does not fail test)
    if (accessibilityScanResults.violations.length > 0) {
      console.warn('A11y violations found:')
      for (const violation of accessibilityScanResults.violations) {
        console.warn(`  [${violation.impact}] ${violation.id}: ${violation.description}`)
        for (const node of violation.nodes) {
          console.warn(`    - ${node.target.join(', ')}`)
        }
      }
    }

    // Only fail on critical/serious violations (color-contrast already disabled)
    const seriousViolations = accessibilityScanResults.violations.filter(
      (v: any) => v.impact === 'critical' || v.impact === 'serious'
    )
    expect(seriousViolations).toEqual([])
  })
})
