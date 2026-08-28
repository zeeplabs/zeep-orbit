import { chromium } from '@playwright/test'
import { STORAGE_STATE_PATH } from './storage-state'

// Bare origin, matching helpers.ts's own BASE convention (not
// playwright.config.ts's `use.baseURL`, which already includes `/dashboard`
// and is irrelevant here since we build absolute URLs by hand).
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const BOOTSTRAP_SECRET = process.env.BOOTSTRAP_SECRET || 'test-secret'

/**
 * Logs in once and persists the session so every project reuses it via
 * `storageState`. Without this, the ~20 `login()` call sites spread across
 * the legacy spec files would each submit a fresh POST /api/login, tripping
 * the backend's per-IP rate limiter (5/min, internal/server/server.go
 * `authLimiter`) well before the suite finishes — a real security control,
 * not something to relax for CI's convenience.
 */
export default async function globalSetup() {
  const browser = await chromium.launch()
  const page = await browser.newPage()

  const statusRes = await page.request.get(`${BASE}/dashboard/api/bootstrap/status`)
  const { bootstrapped } = await statusRes.json()
  if (!bootstrapped) {
    await page.request.post(`${BASE}/dashboard/api/bootstrap`, {
      data: { secret: BOOTSTRAP_SECRET, email: 'admin@test.com', password: 'test1234' },
    })
  }

  await page.goto(`${BASE}/dashboard`)
  await page.waitForURL('**/login')
  await page.fill('input[type="email"]', 'admin@test.com')
  await page.fill('input[type="password"]', 'test1234')
  await page.click('button[type="submit"]')
  await page.waitForURL('**/apps')

  await page.context().storageState({ path: STORAGE_STATE_PATH })
  await browser.close()
}
