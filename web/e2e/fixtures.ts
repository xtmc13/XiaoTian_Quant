import { test as base, expect, type Page } from '@playwright/test'

/**
 * E2E test fixtures — authenticated session & common helpers.
 *
 * Strategy: use context-level init script to inject auth state on EVERY
 * page load, plus route interception for the auth API. This survives
 * reloads, navigations, and route guards.
 */
export const test = base.extend<{
  authPage: Page
}>({
  authPage: async ({ page, context }, use) => {
    // 1. Inject auth state into localStorage before any page scripts run
    await context.addInitScript(() => {
      (window as any).__E2E_AUTH__ = true
      localStorage.setItem('xt-token', 'e2e-test-token')
      const authData = JSON.stringify({
        state: {
          token: 'e2e-test-token',
          user: { id: 1, username: 'e2e_user', role: 'user', nickname: 'E2E Tester' },
          isAuthenticated: true,
        },
        version: 0,
      })
      localStorage.setItem('xt-auth', authData)
    })

    // 2. Intercept auth API to simulate authenticated user
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

    // 3. Broad API fallback: avoid hitting a real backend that would 401 our
    // fake token. Tests can override specific routes after this fixture runs.
    await page.route('**/api/**', async (route) => {
      const url = route.request().url()
      const method = route.request().method()

      // Auth already handled above; let it fall through if this route somehow
      // matches first (Playwright matches most recently added route).
      if (url.includes('/auth/me')) return route.fallback()

      // Endpoints that the UI expects to be arrays
      if (
        url.includes('/strategies') ||
        url.includes('/bots') ||
        url.includes('/ai-bots') ||
        url.includes('/backtests') ||
        url.includes('/orders') ||
        url.includes('/trades') ||
        url.includes('/positions') ||
        url.includes('/portfolio') ||
        url.includes('/social') ||
        url.includes('/config') ||
        url.includes('/markets') ||
        url.includes('/exchanges')
      ) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
      }

      // Mutating endpoints — return a generic success object
      if (method === 'POST' || method === 'PUT' || method === 'DELETE') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true }),
        })
      }

      // Everything else
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({}),
      })
    })

    // 4. Initial navigation to trigger auth state hydration
    await page.goto('/dashboard')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1000)

    // Debug: verify auth state
    const debug = await page.evaluate(() => ({
      e2eAuth: (window as any).__E2E_AUTH__,
      url: window.location.href,
      localStorage: localStorage.getItem('xt-auth'),
    }))
    console.warn('Auth debug:', debug)

    await use(page)
  },
})

export { expect }
