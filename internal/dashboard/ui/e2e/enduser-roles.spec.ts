import { test, expect, Page } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * End-user roles configuration (.specs/features/enduser-roles-config).
 *
 * helpers.ts's createTestApp assumes a one-step "Novo App"/"Criar App" flow
 * that predates the current two-step Create app -> pick "Backend App" ->
 * /apps/new modal in AppsPage.tsx, and it never turns auth_email_enabled on
 * — the roles section (P1) and the end-user "role" claim only exist for
 * apps with end-user email auth enabled. This file has its own helper that
 * follows the current flow and flips the switch.
 *
 * The dashboard's own login endpoint is rate-limited to 5 requests/minute
 * per IP (internal/server/server.go, authLimiter — shared with /api/bootstrap,
 * so a bootstrap call and a login call draw from the same budget). Logging
 * in fresh per test blows through that budget once this file has more than
 * ~4 tests. So this file logs in exactly once, in beforeAll, and every test
 * reuses that session via storageState instead of calling login() again.
 */
let storageState: Awaited<ReturnType<import('@playwright/test').BrowserContext['storageState']>>

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  await bootstrapOrSkip(page)
  await login(page)
  storageState = await context.storageState()
  await context.close()
})

test.beforeEach(async ({ page, context }) => {
  await context.addCookies(storageState.cookies)
  await page.goto('/dashboard/apps')
})

async function createAuthApp(page: Page, name: string, authEmail = true) {
  await page.goto('/dashboard/apps')
  await page.click('button:has-text("Create app")')
  await page.click('button:has-text("Backend App")')
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="my-app"]', name)
  if (authEmail) await page.click('button[role="switch"]')
  await page.click('button:has-text("Create app")')
  // "**/apps/*" would also match the current "/apps/new" URL before the
  // POST finishes, resolving before the real redirect (and before the
  // server-side registry.Register call the next request depends on) — so
  // wait for the actual "/apps/{id}" URL, excluding "/apps/new" itself.
  await page.waitForURL((url) => /\/apps\/(?!new$)[^/]+$/.test(url.pathname))
}

function appIdFromUrl(url: string): string {
  const match = url.match(/\/apps\/([^/?]+)/)
  if (!match) throw new Error(`could not extract app id from ${url}`)
  return match[1]
}

// App names are unique across the whole instance, and the local Postgres
// volume persists across manual test runs — a fixed name would collide with
// a previous run's app and fail with a generic "internal error". Suffix
// with a timestamp so every run gets fresh, disposable names.
function uniqueAppName(prefix: string): string {
  return `${prefix}_${Date.now()}`
}

test.describe('End-user roles configuration', () => {
  test('adds a new role via Settings', async ({ page }) => {
    await createAuthApp(page, uniqueAppName('e2e_roles_add'))

    await page.click('[role="tab"]:has-text("Login providers")')

    await expect(page.locator('text=End-user roles')).toBeVisible()
    await expect(page.getByText('member', { exact: true })).toBeVisible()

    await page.fill('input[placeholder="New role (e.g. viewer)"]', 'viewer')
    await page.click('button:has-text("Add")')

    await expect(page.getByText('viewer', { exact: true })).toBeVisible()
  })

  test('blocks removing a role in use, shows error', async ({ page }) => {
    const appName = uniqueAppName('e2e_roles_block')
    await createAuthApp(page, appName)

    const appId = appIdFromUrl(page.url())

    // Register an end-user through the app's own auth API — it gets the
    // default "member" role, the same one seeded into enduser_roles_config.
    const registerRes = await page.request.post(`/${appName}/auth/register`, {
      data: { email: `enduser-${Date.now()}@test.com`, password: 'test1234' },
    })
    expect(registerRes.status()).toBe(201)

    await page.goto(`/dashboard/apps/${appId}?tab=auth`)
    await page.click('[role="tab"]:has-text("Login providers")')

    // Remove the "member" chip — it's in use by the end-user just created.
    await page.click('[title="Remove role"]')

    await expect(page.locator('text=role in use')).toBeVisible()
    // The chip is still there — the blocked removal never persisted.
    await expect(page.getByText('member', { exact: true })).toBeVisible()
  })

  test('hides the roles section when auth_email_enabled is false', async ({ page }) => {
    // No email auth — the "role" claim doesn't exist for this app, so the
    // Settings section that manages it must not render at all (ROLECFG-08).
    await createAuthApp(page, uniqueAppName('e2e_roles_noauth'), false)

    await page.click('[role="tab"]:has-text("Login providers")')
    // Wait for the tab panel to actually finish rendering before asserting
    // absence — otherwise toHaveCount(0) can resolve on its first poll,
    // before the panel (and the section it would contain) has mounted,
    // passing vacuously regardless of the gate. This marker text is always
    // on this tab, gate or no gate.
    await expect(page.getByText('Register and login via email/password').first()).toBeVisible()

    await expect(page.locator('text=End-user roles')).toHaveCount(0)
  })

  // Merged into one test/one login: the dashboard's own login endpoint is
  // rate-limited to 5/minute per IP (internal/server/server.go, authLimiter),
  // and this spec file runs 5 tests sequentially in well under a minute —
  // splitting P2's sub-behaviors into more top-level tests would trip that
  // limiter. Each stage below still asserts one behavior independently.
  test('edits an end-user role via drawer (save, cancel, orphan role)', async ({ page }) => {
    const appName = uniqueAppName('e2e_roles_edit')
    await createAuthApp(page, appName)
    const appId = appIdFromUrl(page.url())

    // Add "viewer" so there's a second option to switch to.
    await page.click('[role="tab"]:has-text("Login providers")')
    await page.fill('input[placeholder="New role (e.g. viewer)"]', 'viewer')
    await page.click('button:has-text("Add")')
    await expect(page.getByText('viewer', { exact: true })).toBeVisible()

    // Register an end-user — defaults to the "member" role.
    const email = `enduser-${Date.now()}@test.com`
    const registerRes = await page.request.post(`/${appName}/auth/register`, {
      data: { email, password: 'test1234' },
    })
    expect(registerRes.status()).toBe(201)

    await page.goto(`/dashboard/apps/${appId}?tab=users`)

    // ROLECFG-09: the role column is plain text — no input/select inline.
    const roleCell = page.locator('td', { hasText: 'member' })
    await expect(roleCell).toBeVisible()
    await expect(roleCell.locator('input, select')).toHaveCount(0)

    // ROLECFG-14: cancelling the drawer never calls the mutation. Assert on
    // the network call itself, not just DOM text — the "member" cell text
    // predates the click either way, so checking it alone can pass even if
    // a PUT fired and simply hadn't refetched yet (a stale-DOM false pass).
    const roleUpdateRequests: string[] = []
    page.on('request', (req) => {
      if (req.method() === 'PUT' && /\/users\/[^/]+$/.test(new URL(req.url()).pathname)) {
        roleUpdateRequests.push(req.url())
      }
    })
    await page.click('[title="Edit user"]')
    await expect(page.locator('text=Edit user')).toBeVisible()
    // The drawer now also has a phone-country combobox (app-user-phone-mask);
    // role select is the last combobox in the dialog.
    await page.getByRole('combobox').last().click()
    await page.getByRole('option', { name: 'viewer' }).click()
    await page.click('button:has-text("Cancel")')
    await expect(page.locator('text=Edit user')).toHaveCount(0)
    // Give any pending request time to actually reach the network layer
    // before asserting none did.
    await page.waitForLoadState('networkidle')
    expect(roleUpdateRequests).toHaveLength(0)
    await expect(page.locator('td', { hasText: 'member' })).toBeVisible()
    await expect(page.locator('td', { hasText: 'viewer' })).toHaveCount(0)

    // ROLECFG-12: a role assigned outside enduser_roles_config (orphan) is
    // still shown pre-selected in the drawer, not silently swapped out. Set
    // it directly through the (unchanged) role-update endpoint, bypassing
    // the UI's own list — the same thing a pre-existing/legacy value would do.
    const usersRes = await page.request.get(`/dashboard/api/apps/${appId}/users`)
    const usersBody = await usersRes.json()
    const user = usersBody.data.find((u: { email: string }) => u.email === email)
    const orphanRes = await page.request.put(`/dashboard/api/apps/${appId}/users/${user.id}`, {
      data: { email: user.email, phone: user.phone ?? '', role: 'orphaned_role' },
    })
    expect(orphanRes.status()).toBe(200)
    await page.goto(`/dashboard/apps/${appId}?tab=users`)
    await expect(page.locator('td', { hasText: 'orphaned_role' })).toBeVisible()
    await page.click('[title="Edit user"]')
    await expect(page.getByRole('combobox').last().getByText('orphaned_role', { exact: true })).toBeVisible()

    // ROLECFG-10/11/13: switching to a configured role and saving updates
    // the table — no typing involved.
    // The drawer now also has a phone-country combobox (app-user-phone-mask);
    // role select is the last combobox in the dialog.
    await page.getByRole('combobox').last().click()
    await page.getByRole('option', { name: 'viewer' }).click()
    await page.click('button:has-text("Save")')
    await expect(page.locator('td', { hasText: 'viewer' })).toBeVisible()
  })

  test('creates a policy selecting roles via chips', async ({ page }) => {
    await createAuthApp(page, uniqueAppName('e2e_roles_policy'))

    // Add a role beyond the "member" default so the chip toggle is
    // exercised against more than a single option.
    await page.click('[role="tab"]:has-text("Login providers")')
    await page.fill('input[placeholder="New role (e.g. viewer)"]', 'admin')
    await page.click('button:has-text("Add")')
    await expect(page.getByText('admin', { exact: true })).toBeVisible()

    // Add a table with one column.
    await page.click('[role="tab"]:has-text("Database")')
    await page.click('text=Add table')
    await page.fill('input[placeholder="table_name"]', 'items')
    await page.fill('input[placeholder="column_name"]', 'title')
    await page.click('text=Save table')
    await expect(page.locator('text=Save table')).toHaveCount(0)

    // Saving collapses the card into a clickable summary row; clicking it
    // re-enters edit mode, which is where its Schema/Policies tabs live.
    await page.click('text=items')

    // Open the table's Policies tab and create a policy via chips only.
    await page.click('[role="tab"]:has-text("Policies")')
    await page.click('button:has-text("Add policy")')
    await page.fill('input[placeholder="Policy name"]', 'admin_only')
    // Select only "admin" — "member" stays untoggled.
    await page.click('button:has-text("admin")')
    // The default clause needs a claim value picked to pass validation —
    // unrelated to the roles-chips behavior under test, just satisfying
    // the form's existing "value required" rule. Two triggers show "Claim"
    // text: the value-source select (already selected to "Claim") and the
    // claim-value select (still showing its placeholder) — the second one.
    await page.locator('button', { hasText: 'Claim' }).nth(1).click()
    await page.getByRole('option', { name: 'role' }).click()
    await page.click('button:has-text("Save policy")')

    await expect(page.locator('text=Policy created')).toBeVisible()
    // The persisted policy shows exactly the role picked via chips.
    await expect(page.getByText('admin', { exact: true })).toBeVisible()
  })
})
