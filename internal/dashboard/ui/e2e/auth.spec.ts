import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

test.describe('Authentication', () => {
  // This file exercises the login/logout flow itself, so it must start
  // unauthenticated -- opts out of the shared storageState every other spec
  // reuses (see playwright.config.ts / global-setup.ts).
  test.use({ storageState: { cookies: [], origins: [] } })

  test('bootstrap + login + logout', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    // Should be on apps page
    await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible()

    // Click user menu → logout
    await page.getByRole('button', { name: 'Log Out' }).first().click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText('Leave dashboard?')).toBeVisible()
    await dialog.getByRole('button', { name: 'Log Out' }).click()
    await page.waitForURL('**/login')
  })

  test('invalid credentials shows error', async ({ page }) => {
    await page.goto('/dashboard')
    await page.waitForURL('**/login')
    await page.fill('input[type="email"]', 'wrong@test.com')
    await page.fill('input[type="password"]', 'wrongpass')
    await page.click('button[type="submit"]')
    await expect(page.locator('text=Invalid credentials')).toBeVisible()
  })
})
