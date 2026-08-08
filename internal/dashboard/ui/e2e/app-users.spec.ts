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

    await page.goto(`/dashboard/apps/${appId}/users`)
    await expect(page.locator('td', { hasText: originalEmail })).toBeVisible()

    const newEmail = `enduser-updated-${Date.now()}@test.com`
    await page.click('[title="Edit user"]')
    await expect(page.locator('text=Edit user')).toBeVisible()

    await page.locator('input[type="email"]').fill(newEmail)
    // The phone field is the drawer's only other text input (email is the
    // only "email"-typed one; role is a combobox, not an input).
    await page.locator('[role="dialog"] input:not([type="email"])').fill('555-9876')

    await page.click('button:has-text("Save")')
    await expect(page.locator('text=Edit user')).toHaveCount(0)

    await expect(page.locator('td', { hasText: newEmail })).toBeVisible()
    await expect(page.locator('td', { hasText: '555-9876' })).toBeVisible()
  })

  test('shows an error toast when the new email is already in use', async ({ page }) => {
    const appName = uniqueAppName('e2e_users_conflict')
    await createAuthApp(page, appName)
    const appId = appIdFromUrl(page.url())

    const emailA = `enduser-a-${Date.now()}@test.com`
    const emailB = `enduser-b-${Date.now()}@test.com`
    await page.request.post(`/${appName}/auth/register`, { data: { email: emailA, password: 'test1234' } })
    await page.request.post(`/${appName}/auth/register`, { data: { email: emailB, password: 'test1234' } })

    await page.goto(`/dashboard/apps/${appId}/users`)
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
