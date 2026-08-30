import { Page, expect } from '@playwright/test'

const BOOTSTRAP_SECRET = process.env.BOOTSTRAP_SECRET || 'test-secret'
const BASE = process.env.BASE_URL || 'http://localhost:8080'

export async function bootstrapOrSkip(page: Page) {
  const res = await page.request.get(`${BASE}/dashboard/api/bootstrap/status`)
  const { bootstrapped } = await res.json()
  if (bootstrapped) return

  await page.request.post(`${BASE}/dashboard/api/bootstrap`, {
    data: {
      secret: BOOTSTRAP_SECRET,
      email: 'admin@test.com',
      password: 'test1234',
    },
  })
}

export async function login(page: Page) {
  await page.goto('/dashboard')
  // Most specs run with the shared storageState set up by global-setup.ts,
  // which already lands here on /apps -- only submit the login form when a
  // spec genuinely starts unauthenticated (e.g. auth.spec.ts opts out of
  // storageState), so the rest don't each trip a fresh POST /api/login
  // against the backend's per-IP rate limiter.
  await Promise.race([page.waitForURL('**/login'), page.waitForURL('**/apps')])
  if (!page.url().includes('/login')) return
  await page.fill('input[type="email"]', 'admin@test.com')
  await page.fill('input[type="password"]', 'test1234')
  await page.click('button[type="submit"]')
  await page.waitForURL('**/apps')
}

export async function createTestApp(page: Page, name = 'e2e_test') {
  await page.goto('/dashboard/apps')
  await page.getByRole('button', { name: 'Create app' }).first().click()
  await page.getByText('Backend App', { exact: true }).click()
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="my-app"]', name)
  await page.getByRole('button', { name: 'Create app' }).click()
  // "**/apps/*" would also match the current "/apps/new" URL before the
  // POST finishes, resolving before the real redirect (and before the
  // server-side app-creation call the next step depends on) -- so wait for
  // the actual "/apps/{id}" URL, excluding "/apps/new" itself. Same fix as
  // enduser-roles.spec.ts's own createAuthApp.
  await page.waitForURL((url) => /\/apps\/(?!new$)[^/]+$/.test(url.pathname))
}

export function expectOk(response: { status(): number }) {
  expect(response.status()).toBe(200)
}
