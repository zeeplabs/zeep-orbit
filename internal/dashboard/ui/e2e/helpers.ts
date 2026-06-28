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
  await page.waitForURL('**/login')
  await page.fill('input[type="email"]', 'admin@test.com')
  await page.fill('input[type="password"]', 'test1234')
  await page.click('button[type="submit"]')
  await page.waitForURL('**/apps')
}

export async function createTestApp(page: Page, name = 'e2e_test') {
  await page.goto('/dashboard/apps')
  await page.click('text=Novo App')
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="meu_app"]', name)
  await page.click('text=Criar')
  await page.waitForURL('**/apps')
}

export function expectOk(response: { status(): number }) {
  expect(response.status()).toBe(200)
}
