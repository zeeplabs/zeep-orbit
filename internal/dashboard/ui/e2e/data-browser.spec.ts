import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login, createTestApp } from './helpers'

test.describe('Data Browser', () => {
  test('browse app tables', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)
    await createTestApp(page, 'e2e_db_test')

    await page.goto('/dashboard/data-browser')
    await expect(page.locator('text=e2e_db_test')).toBeVisible()
  })

  test('export CSV', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)
    await createTestApp(page, 'e2e_csv_test')

    await page.goto('/dashboard/data-browser')
    await page.click('text=e2e_csv_test')
    await page.click('text=CSV')
  })
})
