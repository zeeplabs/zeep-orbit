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
    await page.click('[role="tab"]:has-text("Members")')
    // The app creator is auto-added as an Admin member on creation, so
    // there's no "no members yet" empty state to observe here -- just the
    // owner's row (header row + 1 data row).
    const table = page.locator('main table')
    await expect(table.getByRole('row')).toHaveCount(2)

    // --- Add the second user as viewer ---
    await page.click('button:has-text("Add member")')
    const addDrawer = page.getByRole('dialog')
    // Open the user Select (the trigger is the only button with the placeholder;
    // it's a Radix Select trigger with role="combobox", not getByRole('button'))
    await addDrawer.locator('button:has-text("Pick a user")').click()
    // Pick the second user
    await page.click(`[role="option"]:has-text("${secondEmail}")`)
    // The role Select defaults to "editor" (AppMembersList.tsx's
    // `useState<AppRole>("editor")`) -- select "Viewer" explicitly so the
    // add and the later role-change actually exercise different roles.
    await page.click('button#add-member-role')
    await page.click('[role="option"]:has-text("Viewer")')
    // Submit -- scoped to the drawer, since the page-level "Add member"
    // trigger button behind it matches the same text and is covered by it
    await addDrawer.locator('button:has-text("Add member"):not(:has-text("Adding"))').click()

    // The new member joins the owner's row in the list
    await expect(table.getByRole('row')).toHaveCount(3)
    await expect(page.locator(`text=${secondEmail}`)).toBeVisible()
    await expect(page.locator('text=Viewer')).toBeVisible()

    // --- Change the member's role to editor ---
    // The change button has data-testid="member-change-{user_id}" — we
    // look it up by the surrounding row.
    const row = page.locator('tr', { hasText: secondEmail })
    await row.locator('[data-testid^="member-change-"]').click()
    // Open the role Select inside the drawer
    await page.click('button#change-member-role')
    await page.click('[role="option"]:has-text("Editor")')
    await page.click('button:has-text("Save"):not(:has-text("Saving"))')

    // The role badge now reads "Editor" in the row -- exact match, since the
    // second user's display name ("Editor User") also contains the substring
    await expect(row.getByText('Editor', { exact: true })).toBeVisible()

    // --- Remove the member ---
    await row.locator('[data-testid^="member-remove-"]').click()
    // ConfirmDialog opens
    await expect(page.locator('text=Remove member?')).toBeVisible()
    await page.click('button:has-text("Remove"):not(:has-text("Removing"))')

    // The member is gone; only the owner's row remains
    await expect(page.locator(`text=${secondEmail}`)).toHaveCount(0)
    await expect(table.getByRole('row')).toHaveCount(2)
  })
})
