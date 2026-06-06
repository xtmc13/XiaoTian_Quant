import { test, expect } from '@playwright/test'

test('debug login form', async ({ page }) => {
  await page.goto('/login')
  await page.waitForLoadState('domcontentloaded')
  await page.waitForTimeout(1000)

  // Check if inputs exist
  const usernameInput = page.locator('input[autocomplete="username"]')
  const passwordInput = page.locator('input[autocomplete="current-password"]')

  console.log('Username input count:', await usernameInput.count())
  console.log('Password input count:', await passwordInput.count())

  // Fill inputs
  await usernameInput.first().fill('e2e_user')
  await passwordInput.first().fill('e2e_password')

  // Screenshot after fill
  await page.screenshot({ path: 'debug-after-fill.png' })

  // Check values
  const usernameValue = await usernameInput.first().inputValue()
  const passwordValue = await passwordInput.first().inputValue()
  console.log('Username value:', usernameValue)
  console.log('Password value:', passwordValue)

  expect(usernameValue).toBe('e2e_user')
  expect(passwordValue).toBe('e2e_password')
})
