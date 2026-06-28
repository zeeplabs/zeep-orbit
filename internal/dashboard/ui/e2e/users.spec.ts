import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

test.describe('User Management', () => {
  test('list users page loads', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    await page.goto('/dashboard/usuarios')
    await expect(page.locator('text=Gerenciar Usuários')).toBeVisible()
  })
})
