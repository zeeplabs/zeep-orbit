import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login, createTestApp } from './helpers'

/**
 * App Members tab (rbac-per-app T-09). Covers the user-visible flow:
 *   1. Open the Members tab from the app details page
 *   2. Add an existing dashboard user as a member with a role
 *   3. Change the member's role
 *   4. Remove the member
 *
 * The full "Independent Test" from .specs/features/rbac-per-app/spec.md
 * (line 46) — including the "≥1 admin" invariant and the dual-admin
 * sequence — is covered by the Go-level test
 * `internal/dashboard/app_members_handler_test.go::TestAppMembersIndependentTest`.
 * This e2e exercises the UI surface only, with one DB-backed second
 * user so add/change/remove can be observed end-to-end.
 */
test.describe('App Members', () => {
  test('add member, change role, remove', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)

    // The bootstrap creates only one user (admin@test.com). To exercise
    // add/change/remove, create a second one via the API as superadmin.
    const secondEmail = `editor-${Date.now()}@test.com`
    const createRes = await page.request.post('/dashboard/api/users', {
      data: { email: secondEmail, name: 'Editor User', password: 'test1234', role: 'member' },
    })
    expect(createRes.status()).toBe(201)

    await createTestApp(page, 'e2e_members')

    // Open the Members tab
    await page.click('[role="tab"]:has-text("Membros")')
    // Empty state visible
    await expect(page.locator('text=Nenhum membro ainda')).toBeVisible()

    // --- Add the second user as viewer ---
    await page.click('button:has-text("Adicionar membro")')
    // Open the user Select (the trigger is the only button with the placeholder)
    await page.click('button:has-text("Escolha um usuário")')
    // Pick the second user
    await page.click(`[role="option"]:has-text("${secondEmail}")`)
    // Submit
    await page.click('button:has-text("Adicionar membro"):not(:has-text("Adicionando"))')

    // The empty state is gone; the new member is in the list
    await expect(page.locator(`text=${secondEmail}`)).toBeVisible()
    // Role badge reads "Viewer" (the initial state — we picked editor
    // in the code but e2e was authored to leave the default; the test
    // asserts the *behavior*, not a specific initial role).
    await expect(page.locator('text=Viewer')).toBeVisible()

    // --- Change the member's role to editor ---
    // The change button has data-testid="member-change-{user_id}" — we
    // look it up by the surrounding row.
    const row = page.locator('tr', { hasText: secondEmail })
    await row.locator('[data-testid^="member-change-"]').click()
    // Open the role Select inside the drawer
    await page.click('button#change-member-role')
    await page.click('[role="option"]:has-text("Editor")')
    await page.click('button:has-text("Salvar"):not(:has-text("Salvando"))')

    // The role badge now reads "Editor" in the row
    await expect(row.locator('text=Editor')).toBeVisible()

    // --- Remove the member ---
    await row.locator('[data-testid^="member-remove-"]').click()
    // ConfirmDialog opens
    await expect(page.locator('text=Remover membro?')).toBeVisible()
    await page.click('button:has-text("Remover"):not(:has-text("Removendo"))')

    // The member is gone; the empty state is back
    await expect(page.locator(`text=${secondEmail}`)).toHaveCount(0)
    await expect(page.locator('text=Nenhum membro ainda')).toBeVisible()
  })
})
