import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login, createTestApp } from './helpers'

test.describe('App Management', () => {
  test('create app, then add, edit and delete a table', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    await createTestApp(page, 'e2e_app')
    // createTestApp already waits for the redirect to /apps/:id

    // Add a table
    await page.click('text=Adicionar Tabela')
    await page.fill('input[placeholder="tabela_1"]', 'items')
    await page.fill('input[placeholder="nome_coluna"]', 'title')
    await page.click('text=Salvar tabela')

    // Table now shows saved (collapsed), no longer in edit mode
    await expect(page.locator('text=Salvar tabela')).toHaveCount(0)
    await expect(page.locator('text=items')).toBeVisible()

    // "Adicionar Tabela" is available again once nothing is being edited
    await expect(page.locator('text=Adicionar Tabela')).toBeEnabled()

    // Edit the saved table: add a second column
    await page.click('text=Editar')
    await page.click('text=Adicionar Coluna')
    const columnNames = page.locator('input[placeholder="nome_coluna"]')
    await columnNames.nth(1).fill('description')
    await page.click('text=Salvar tabela')
    await expect(page.locator('text=Salvar tabela')).toHaveCount(0)

    // Delete the table
    page.once('dialog', (dialog) => dialog.accept())
    await page.click('[class*="border-red-500"]')
    await expect(page.locator('text=Nenhuma tabela')).toBeVisible()
  })

  test('delete app', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)
    await createTestApp(page, 'e2e_to_delete')

    await page.goto('/dashboard/apps')

    // Hover to show delete button and click
    await page.hover('text=e2e_to_delete')
    await page.click('[title="Deletar app"]')
    await expect(page.locator('text=Deletar app?')).toBeVisible()
    await page.click('button:has-text("Deletar")')
  })
})
