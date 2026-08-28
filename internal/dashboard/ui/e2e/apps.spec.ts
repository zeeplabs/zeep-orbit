import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login, createTestApp } from './helpers'

test.describe('App Management', () => {
  test('create app, then add, edit and delete a table', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    await createTestApp(page, 'e2e_app')
    // createTestApp already waits for the redirect to /apps/:id

    // Add a table
    await page.click('text=Add table')
    await page.fill('input[placeholder="table_name"]', 'items')
    await page.fill('input[placeholder="column_name"]', 'title')
    await page.click('text=Save table')

    // Table now shows saved (collapsed), no longer in edit mode
    await expect(page.locator('text=Save table')).toHaveCount(0)
    await expect(page.locator('text=items')).toBeVisible()

    // "Add table" is available again once nothing is being edited
    await expect(page.locator('text=Add table')).toBeEnabled()

    // Edit the saved table: the collapsed row itself is the edit affordance
    // (no separate "Edit" button/label exists in TableCard.tsx)
    await page.click('text=items')
    await page.click('text=Add Column')
    const columnNames = page.locator('input[placeholder="column_name"]')
    await columnNames.nth(1).fill('description')
    await page.click('text=Save table')
    await expect(page.locator('text=Save table')).toHaveCount(0)

    // Delete the table -- the delete action only lives inside the edit form
    // (re-enter it), and confirmation is a real ConfirmDialog component, not
    // a native browser dialog.
    await page.click('text=items')
    await page.click('button:has-text("Delete table")')
    const deleteDialog = page.getByRole('dialog')
    await expect(deleteDialog.getByText('Delete table?')).toBeVisible()
    await deleteDialog.getByRole('button', { name: 'Delete table' }).click()
    await expect(page.locator('text=No tables')).toBeVisible()
  })

  test('delete app', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)
    await createTestApp(page, 'e2e_to_delete')

    await page.goto('/dashboard/apps')

    // Hover to show delete button and click
    await page.hover('text=e2e_to_delete')
    await page.click('[title="Delete app"]')
    await expect(page.locator('text=Remove app?')).toBeVisible()
    await page.click('button:has-text("Remove")')
  })
})
