import { test, expect, Page } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * Default end-user role per app. New end users (email register path) must get
 * the role chosen as default in the app's "End-user roles" panel, instead of
 * the hard-coded "member". Uses the shared storageState from global-setup.
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

test.beforeEach(async ({ context }) => {
  await context.addCookies(storageState.cookies)
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

async function registerAndGetRole(page: Page, appId: string, appName: string, email: string) {
  const res = await page.request.post(`/${appName}/auth/register`, {
    data: { email, password: 'test1234' },
  })
  expect(res.status()).toBe(201)
  const list = await page.request.get(`/dashboard/api/apps/${appId}/users?search=${encodeURIComponent(email)}`)
  expect(list.status()).toBe(200)
  const body = await list.json()
  const users: { email: string; role: string }[] = body.data
  return users.find((u) => u.email === email)?.role
}

const badge = (page: Page, role: string) => page.getByText(role, { exact: true }).locator('..')

test('new end users get the role chosen as default', async ({ page }) => {
  const appName = `e2e_default_role_${Date.now()}`
  await createAuthApp(page, appName)
  const appId = page.url().match(/\/apps\/([^/?]+)/)![1]

  await page.click('[role="tab"]:has-text("Login")')
  await expect(page.locator('text=End-user roles')).toBeVisible()

  // A fresh app starts with member as the default.
  await expect(badge(page, 'member').getByText('default', { exact: true })).toBeVisible()
  expect(await registerAndGetRole(page, appId, appName, `before-${Date.now()}@test.com`)).toBe('member')

  // Add "basic" and make it the default.
  await page.fill('input[placeholder="New role (e.g. viewer)"]', 'basic')
  await page.click('button:has-text("Add")')
  await expect(page.getByText('basic', { exact: true })).toBeVisible()
  await badge(page, 'basic').getByRole('button', { name: 'Set as default' }).click()
  await expect(badge(page, 'basic').getByText('default', { exact: true })).toBeVisible()
  await expect(badge(page, 'member').getByRole('button', { name: 'Set as default' })).toBeVisible()

  // The default role can't be removed while it is the default.
  await expect(badge(page, 'basic').locator('button[disabled]')).toHaveCount(1)

  // A user created now gets "basic" (the real sign-up path, no manual fix-up).
  expect(await registerAndGetRole(page, appId, appName, `after-${Date.now()}@test.com`)).toBe('basic')

  // It survives a reload.
  await page.reload()
  await page.click('[role="tab"]:has-text("Login")')
  await expect(badge(page, 'basic').getByText('default', { exact: true })).toBeVisible()

  // Switching back to member applies to the next user.
  await badge(page, 'member').getByRole('button', { name: 'Set as default' }).click()
  await expect(badge(page, 'member').getByText('default', { exact: true })).toBeVisible()
  expect(await registerAndGetRole(page, appId, appName, `back-${Date.now()}@test.com`)).toBe('member')
})
