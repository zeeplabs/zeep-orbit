import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login, createTestApp } from './helpers'

test.describe('App Management', () => {
  test('create app with table', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    await page.goto('/dashboard/apps')
    await page.click('text=Novo App')
    await page.waitForURL('**/apps/new')

    // Fill app name
    await page.fill('input[placeholder="meu_app"]', 'e2e_app')

    // Add a table
    await page.click('text=Adicionar Tabela')
    await page.fill('input[placeholder="nome_da_tabela"]', 'items')

    // Add a column
    await page.click('text=Adicionar Coluna')
    await page.fill('input[placeholder="nome"]', 'title')
    await page.selectOption('select:below(:text("Tipo"))', 'text')

    // Submit
    await page.click('text=Criar')
    await page.waitForURL('**/apps')

    // App should be listed
    await expect(page.locator('text=e2e_app')).toBeVisible()
  })

  test('delete app', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)
    await createTestApp(page, 'e2e_to_delete')

    // Hover to show delete button and click
    await page.hover('text=e2e_to_delete')
    await page.click('[title="Deletar app"]')
    await expect(page.locator('text=Deletar app?')).toBeVisible()
    await page.click('button:has-text("Deletar")')
  })
})
