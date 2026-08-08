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
 */
async function createAuthApp(page: Page, name: string) {
  await page.goto('/dashboard/apps')
  await page.click('button:has-text("Create app")')
  await page.click('button:has-text("Backend App")')
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="my-app"]', name)
  await page.click('button[role="switch"]')
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
    await bootstrapOrSkip(page)
    await login(page)
    await createAuthApp(page, uniqueAppName('e2e_roles_add'))

    await page.click('[role="tab"]:has-text("Login providers")')

    await expect(page.locator('text=End-user roles')).toBeVisible()
    await expect(page.getByText('member', { exact: true })).toBeVisible()

    await page.fill('input[placeholder="New role (e.g. viewer)"]', 'viewer')
    await page.click('button:has-text("Add")')

    await expect(page.getByText('viewer', { exact: true })).toBeVisible()
  })

  test('blocks removing a role in use, shows error', async ({ page }) => {
    await bootstrapOrSkip(page)
    await login(page)
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
})
