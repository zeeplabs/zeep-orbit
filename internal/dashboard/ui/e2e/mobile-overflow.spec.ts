import { test, expect, Page } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * Narrow-viewport layout guard: the app detail tabs that carry dense rows
 * (table schema editor, webhook cards) must not make the page scroll sideways
 * at phone width.
 */
test.use({ viewport: { width: 390, height: 844 } })

const BASE = process.env.BASE_URL || 'http://localhost:8080'

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

async function hasHorizontalOverflow(page: Page): Promise<{ scrollWidth: number; innerWidth: number }> {
  return page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    innerWidth: window.innerWidth,
  }))
}

test('table schema editor and webhook cards fit a 390px viewport', async ({ page }) => {
  const name = `overflow_${Date.now()}`
  const api = `${BASE}/dashboard/api`

  const appRes = await page.request.post(`${api}/apps`, { data: { name } })
  expect(appRes.ok(), await appRes.text()).toBeTruthy()
  const app = await appRes.json()

  const tableRes = await page.request.post(`${api}/apps/${app.id}/tables`, {
    data: {
      name: 'orders',
      rls: '',
      columns: [
        { name: 'customer_email', type: 'text', required: true, unique: false, default: '' },
        { name: 'total_cents', type: 'integer', required: false, unique: false, default: '0' },
        { name: 'created_at_client', type: 'timestamptz', required: true, unique: false, default: 'now()', default_is_expression: true },
      ],
    },
  })
  expect(tableRes.ok(), await tableRes.text()).toBeTruthy()

  const hookRes = await page.request.post(`${api}/apps/${app.id}/webhooks`, {
    data: { name: 'Payment provider events', event_type_path: 'type', method: 'POST' },
  })
  expect(hookRes.ok(), await hookRes.text()).toBeTruthy()

  await page.goto(`/dashboard/apps/${app.id}`)
  await page.getByRole('tab', { name: 'Database' }).click()
  await page.getByText('orders', { exact: true }).first().click()
  await expect(page.getByRole('button', { name: 'Save table' })).toBeVisible()

  // On phones each column is a card with its own Required/Unique labels.
  await expect(page.getByText('Unique', { exact: true }).locator('visible=true').first()).toBeVisible()
  expect(await hasHorizontalOverflow(page)).toEqual({ scrollWidth: 390, innerWidth: 390 })

  await page.getByRole('tab', { name: 'Webhooks' }).click()
  await expect(page.getByText('Payment provider events')).toBeVisible()
  expect(await hasHorizontalOverflow(page)).toEqual({ scrollWidth: 390, innerWidth: 390 })

  await page.getByRole('button', { name: 'Map', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Hide mapping' })).toBeVisible()
  expect(await hasHorizontalOverflow(page)).toEqual({ scrollWidth: 390, innerWidth: 390 })

  await page.request.delete(`${api}/apps/${app.id}`)
})
