import { test, expect, Page } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * App user email/phone editing (.specs/features/app-user-edit-fields).
 *
 * Same login-once/reuse-session pattern as enduser-roles.spec.ts — the
 * dashboard's login endpoint is rate-limited to 5/minute per IP.
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

async function createAuthApp(page: Page, name: string) {
  await page.goto('/dashboard/apps')
  await page.click('button:has-text("Create app")')
  await page.click('button:has-text("Backend App")')
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="my-app"]', name)
  await page.click('button[role="switch"]')
  await page.click('button:has-text("Create app")')
  await page.waitForURL((url) => /\/apps\/(?!new$)[^/]+$/.test(url.pathname))
}

function appIdFromUrl(url: string): string {
  const match = url.match(/\/apps\/([^/?]+)/)
  if (!match) throw new Error(`could not extract app id from ${url}`)
  return match[1]
}

function uniqueAppName(prefix: string): string {
  return `${prefix}_${Date.now()}`
}

test.describe('App user edit fields', () => {
  test('edits email, phone, and role from the drawer', async ({ page }) => {
    const appName = uniqueAppName('e2e_users_edit')
    await createAuthApp(page, appName)
    const appId = appIdFromUrl(page.url())

    const originalEmail = `enduser-${Date.now()}@test.com`
    const registerRes = await page.request.post(`/${appName}/auth/register`, {
      data: { email: originalEmail, password: 'test1234' },
    })
    expect(registerRes.status()).toBe(201)

    await page.goto(`/dashboard/apps/${appId}?tab=users`)
    await expect(page.locator('td', { hasText: originalEmail })).toBeVisible()

    const newEmail = `enduser-updated-${Date.now()}@test.com`
    await page.click('[title="Edit user"]')
    await expect(page.locator('text=Edit user')).toBeVisible()

    // AUE-01: the drawer opens pre-filled with the user's current email
    // (registration left phone empty, so the phone field starts blank too).
    await expect(page.locator('input[type="email"]')).toHaveValue(originalEmail)
    await expect(page.locator('[role="dialog"] input:not([type="email"])')).toHaveValue('')

    // AUE-02: capture the actual PUT body to prove all three keys are sent
    // together, not just that the table ends up with the right values.
    const putPromise = page.waitForRequest(
      (req) => req.method() === 'PUT' && /\/dashboard\/api\/apps\/[^/]+\/users\/[^/]+$/.test(new URL(req.url()).pathname),
    )

    await page.locator('input[type="email"]').fill(newEmail)
    // The phone field is the drawer's only other text input (email is the
    // only "email"-typed one; role is a combobox, not an input).
    await page.locator('[role="dialog"] input:not([type="email"])').fill('555-9876')

    await page.click('button:has-text("Save")')
    const putReq = await putPromise
    const putBody: { email?: string; phone?: string; role?: string } = putReq.postDataJSON()
    await expect(page.locator('text=Edit user')).toHaveCount(0)

    expect(putBody).toMatchObject({ email: newEmail, phone: '555-9876', role: 'member' })
    expect(Object.keys(putBody).sort()).toEqual(['email', 'phone', 'role'])

    await expect(page.locator('td', { hasText: newEmail })).toBeVisible()
    await expect(page.locator('td', { hasText: '555-9876' })).toBeVisible()
  })

  test('reaches the tab from the apps list card, switches tabs, and toggles user status', async ({ page }) => {
    const appName = uniqueAppName('e2e_users_nav')
    await createAuthApp(page, appName)

    const email = `enduser-${Date.now()}@test.com`
    const registerRes = await page.request.post(`/${appName}/auth/register`, {
      data: { email, password: 'test1234' },
    })
    expect(registerRes.status()).toBe(201)

    // AUT-05: the apps-list card's "Users" action opens the tab, not the
    // removed standalone route.
    await page.goto('/dashboard/apps')
    const card = page.locator('div.flex.h-full.flex-col', {
      has: page.locator('h3', { hasText: appName }),
    })
    await card.getByRole('button', { name: 'Users' }).click()
    await page.waitForURL(/\/apps\/[^/]+\?tab=users$/)
    await expect(page.locator('td', { hasText: email })).toBeVisible()

    // AUT-02: clicking another tab trigger and back updates the URL and
    // content without a full page navigation (SPA route stays mounted).
    await page.click('[role="tab"]:has-text("Login providers")')
    await page.waitForURL(/tab=auth$/)
    await page.click('[role="tab"]:has-text("Users")')
    await page.waitForURL(/tab=users$/)
    await expect(page.locator('td', { hasText: email })).toBeVisible()

    // AUT-04: activate/deactivate mutation still works unchanged from inside
    // the tab.
    const row = page.locator('tr', { has: page.locator('td', { hasText: email }) })
    await expect(row.locator('text=Active')).toBeVisible()
    await row.locator('[title="Deactivate"]').click()
    await expect(row.locator('text=Inactive')).toBeVisible()
    await row.locator('[title="Reactivate"]').click()
    await expect(row.locator('text=Active')).toBeVisible()
  })

  test('shows an error toast when the new email is already in use', async ({ page }) => {
    const appName = uniqueAppName('e2e_users_conflict')
    await createAuthApp(page, appName)
    const appId = appIdFromUrl(page.url())

    const emailA = `enduser-a-${Date.now()}@test.com`
    const emailB = `enduser-b-${Date.now()}@test.com`
    await page.request.post(`/${appName}/auth/register`, { data: { email: emailA, password: 'test1234' } })
    await page.request.post(`/${appName}/auth/register`, { data: { email: emailB, password: 'test1234' } })

    await page.goto(`/dashboard/apps/${appId}?tab=users`)
    await expect(page.locator('td', { hasText: emailB })).toBeVisible()

    const rowB = page.locator('tr', { has: page.locator('td', { hasText: emailB }) })
    await rowB.locator('[title="Edit user"]').click()
    await expect(page.locator('text=Edit user')).toBeVisible()

    await page.locator('input[type="email"]').fill(emailA)
    await page.click('button:has-text("Save")')

    await expect(page.getByText('email already in use')).toBeVisible()
    // The drawer stays open (save failed) and the row is unchanged.
    await expect(page.locator('text=Edit user')).toBeVisible()
    await page.click('button:has-text("Cancel")')
    await expect(page.locator('td', { hasText: emailB })).toBeVisible()
  })
})
