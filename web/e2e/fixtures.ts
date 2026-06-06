import { test as base, expect, type Page } from '@playwright/test'

/**
 * E2E test fixtures — authenticated session & common helpers.
 */
export const test = base.extend<{
  authPage: Page
}>({
  authPage: async ({ page, context }, use) => {
    // Set E2E auth bypass flag before any page loads
    await context.addInitScript(() => {
      (window as any).__E2E_AUTH__ = true
    })
    // Intercept auth API to simulate authenticated user
    await page.route('**/api/auth/me', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 1,
          username: 'e2e_user',
          role: 'user',
          nickname: 'E2E Tester',
        }),
      })
    })
    // First visit to set localStorage auth state, then reload
    await page.goto('/dashboard')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(500)
    await page.evaluate(() => {
      localStorage.setItem('xt-auth', JSON.stringify({
        state: {
          token: 'e2e-test-token',
          user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' },
          isAuthenticated: true,
        },
        version: 0,
      }))
      window.location.reload()
    })
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)
    // Debug: check auth state
    const debug = await page.evaluate(() => ({
      e2eAuth: (window as any).__E2E_AUTH__,
      url: window.location.href,
      localStorage: localStorage.getItem('xt-auth'),
    }))
    console.log('Auth debug:', debug)
    await use(page)
  },
})

export { expect }
