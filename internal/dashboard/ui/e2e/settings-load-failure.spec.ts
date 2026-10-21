import { test, expect, Route } from '@playwright/test'

/**
 * Settings forms whose stored values fail to load must stay hidden: their defaults are
 * not the saved config, and Save would overwrite the real values (disable Google login,
 * clear storage, reset soft delete) with empty ones. Data is mocked; the shell is real.
 */

const json = (route: Route, body: unknown) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })

const SYSTEM_OK = {
  soft_delete_enabled: false,
  max_csv_export_rows: 10000,
  statement_timeout_ms: 30000,
  require_rls_default: false,
  retention_days: 0,
}
const GOOGLE_OK = { config_set: false, enabled: false, config: {} }

const cases = [
  { tab: 'auth', url: /\/dashboard\/api\/config\/auth\/providers\/google/, ok: GOOGLE_OK },
  { tab: 'storage', url: /\/dashboard\/api\/config\/system$/, ok: { ...SYSTEM_OK, storage_config: null } },
  { tab: 'database', url: /\/dashboard\/api\/config\/system$/, ok: SYSTEM_OK },
]

for (const c of cases) {
  test(`${c.tab} tab: failed load hides the form and sends no write; retry restores it`, async ({ page }) => {
    let failing = true
    const writes: string[] = []
    await page.route(c.url, (route) => {
      const method = route.request().method()
      if (method !== 'GET') {
        writes.push(method)
        return route.fulfill({ status: 500, body: '{}' })
      }
      return failing
        ? route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' })
        : json(route, c.ok)
    })

    await page.goto(`/dashboard/configuracoes?tab=${c.tab}`)
    const alert = page.getByRole('alert').filter({ hasText: 'Error loading config' })
    await expect(alert).toBeVisible()
    await expect(page.getByRole('main').getByRole('button', { name: /^Save/ })).toHaveCount(0)

    failing = false
    await alert.getByRole('button', { name: 'Try again' }).click()
    await expect(alert).toHaveCount(0)
    await expect(page.getByRole('main').getByRole('button', { name: /^Save/ }).first()).toBeVisible()
    expect(writes).toEqual([])
  })
}
