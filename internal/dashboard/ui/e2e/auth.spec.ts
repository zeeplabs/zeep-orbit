import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

test.describe('Authentication', () => {
  test('bootstrap + login + logout', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    // Should be on apps page
    await expect(page.locator('text=Apps')).toBeVisible()

    // Click user menu → logout
    await page.click('text=Sair')
    await expect(page.locator('text=Sair do dashboard?')).toBeVisible()
    await page.click('button:has-text("Sair")')
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
