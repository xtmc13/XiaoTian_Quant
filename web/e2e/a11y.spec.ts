import { test, expect } from './fixtures'
import AxeBuilder from '@axe-core/playwright'

/**
 * Accessibility (a11y) E2E tests using axe-core.
 * Scans core pages for WCAG 2.1 AA violations.
 *
 * @see https://github.com/dequelabs/axe-core-npm/tree/develop/packages/playwright
 */

// Helper: login via UI to establish authenticated session
async function loginViaUI(page: any) {
  // Intercept API requests: return empty array for list endpoints, passthrough for others
  await page.route('**/api/**', async (route) => {
    const url = route.request().url()
    // Return empty array for endpoints that expect arrays
    if (url.includes('/strategies') || url.includes('/bots') || url.includes('/backtests') || url.includes('/orders') || url.includes('/trades')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
      return
    }
    // For auth endpoints, return mock user data
    if (url.includes('/auth/me')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' }),
      })
      return
    }
    // Passthrough other API requests (return 200 to prevent 401 logout)
    try {
      const response = await route.fetch()
      const body = await response.text()
      await route.fulfill({
        status: 200,
        headers: response.headers(),
        body,
      })
    } catch {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    }
  })

  await page.goto('/login')
  await page.waitForLoadState('domcontentloaded')
  await page.waitForTimeout(2000)

  // Set auth data in localStorage
  await page.evaluate(() => {
    localStorage.setItem('xt-token', 'e2e-test-token')
    localStorage.setItem('xt-auth', JSON.stringify({
      state: {
        token: 'e2e-test-token',
        user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' },
        isAuthenticated: true,
      },
      version: 0,
    }))
  })

  // Navigate to target page
  await page.goto('/dashboard')
  await page.waitForLoadState('domcontentloaded')
  await page.waitForTimeout(3000)

  // Debug: check URL
  const url = page.url()
  console.log('URL after login bypass:', url)
}

// Pages that can be accessed without authentication
test.describe('Public Pages - a11y', () => {
  test('login page has no critical a11y violations', async ({ page }) => {
    await page.goto('/login')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1000)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })
})

// Pages that require authentication
test.describe('Authenticated Pages - a11y', () => {
  test('dashboard has no critical a11y violations', async ({ page }) => {
    await loginViaUI(page)
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('strategy page has no critical a11y violations', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/strategy')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('trading page has no critical a11y violations', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/trading')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .exclude('.klinecharts-pro')
      .exclude('.overflow-y-auto')
      .exclude('.overflow-x-auto')
      .disableRules(['color-contrast'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('bots page has no critical a11y violations', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/bots')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('backtest page has no critical a11y violations', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/backtest')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('settings page has no critical a11y violations', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/settings')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })
})

// Interactive components - modals, dropdowns, etc.
test.describe('Interactive Components - a11y', () => {
  test('strategy create modal is accessible', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/strategy')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)
    await page.click('button:has-text("创建策略")')
    await expect(page.locator('text=创建策略').first()).toBeVisible()

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('sidebar navigation is accessible', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/dashboard')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    // Check that navigation landmarks exist
    const nav = page.locator('nav').first()
    await expect(nav).toBeVisible()

    // Check that current page link has aria-current
    const activeLink = page.locator('nav [aria-current="page"]').first()
    await expect(activeLink).toBeVisible()

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })
})

// Detailed violation reporting (non-blocking)
test.describe('A11y Detailed Report', () => {
  test('generate detailed a11y report for dashboard', async ({ page }) => {
    await loginViaUI(page)
    await page.goto('/dashboard')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    const accessibilityScanResults = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze()

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

    // Only fail on critical/serious violations
    const seriousViolations = accessibilityScanResults.violations.filter(
      (v: any) => v.impact === 'critical' || v.impact === 'serious'
    )
    expect(seriousViolations).toEqual([])
  })
})
