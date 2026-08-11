import { test, expect, Page } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * Inbound webhooks — full lifecycle (.specs/features/inbound-webhooks).
 *
 * One test, sequential stages, each stage mapped to a specific spec.md
 * acceptance criterion (comments cite the exact AC). Scope is deliberately
 * bounded to what T17 covers: P1 "create webhook + capture sample" (AC1-6)
 * and P2 "map captured sample and activate for inserts" (AC1-6). The sibling
 * P2 story ("map update and delete with a match key") is out of scope here —
 * it's covered by the backend integration suite (internal/server/webhook_active_update_delete_test.go),
 * not re-exercised at the e2e layer per this task's instructions.
 *
 * Follows the enduser-roles.spec.ts convention: log in once in beforeAll and
 * reuse the session via storageState, because the dashboard's login endpoint
 * is rate-limited to 5 requests/minute per IP (internal/server/server.go,
 * authLimiter) and a fresh login per test would trip that limiter.
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

function uniqueAppName(prefix: string): string {
  return `${prefix}_${Date.now()}`
}

async function createBackendApp(page: Page, name: string): Promise<string> {
  await page.goto('/dashboard/apps')
  await page.click('button:has-text("Create app")')
  await page.click('button:has-text("Backend App")')
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="my-app"]', name)
  await page.click('button:has-text("Create app")')
  // "**/apps/*" would also match "/apps/new" itself before the POST
  // resolves — wait for the real "/apps/{id}" URL (enduser-roles.spec.ts
  // lesson), excluding "/apps/new".
  await page.waitForURL((url) => /\/apps\/(?!new$)[^/]+$/.test(url.pathname))
  const match = page.url().match(/\/apps\/([^/?]+)/)
  if (!match) throw new Error(`could not extract app id from ${page.url()}`)
  return match[1]
}

async function addTable(page: Page, tableName: string, columnName: string) {
  await page.click('[role="tab"]:has-text("Database")')
  await page.click('text=Add table')
  await page.fill('input[placeholder="table_name"]', tableName)
  await page.fill('input[placeholder="column_name"]', columnName)
  await page.click('text=Save table')
  await expect(page.locator('text=Save table')).toHaveCount(0)
}

/** Row count for a table via the dashboard's own data-browser query endpoint
 * — avoids needing a direct Postgres connection or the end-user JWT-guarded
 * API just to verify write/no-write outcomes. */
async function rowCount(page: Page, appName: string, tableName: string): Promise<number> {
  const res = await page.request.get(
    `/dashboard/api/data-browser/query?app=${encodeURIComponent(appName)}&table=${encodeURIComponent(tableName)}`,
  )
  expect(res.status()).toBe(200)
  const body = await res.json()
  return body.count as number
}

async function rows(page: Page, appName: string, tableName: string): Promise<Record<string, unknown>[]> {
  const res = await page.request.get(
    `/dashboard/api/data-browser/query?app=${encodeURIComponent(appName)}&table=${encodeURIComponent(tableName)}`,
  )
  expect(res.status()).toBe(200)
  const body = await res.json()
  return body.data as Record<string, unknown>[]
}

test.describe('Inbound webhooks — full lifecycle', () => {
  test('create, capture, map, activate, insert, and review the delivery log', async ({ page }) => {
    const appName = uniqueAppName('e2e_webhook')
    const appId = await createBackendApp(page, appName)

    // Target table for the mapping: one column, deliberately named
    // differently from the payload field it will be linked to
    // (employee_name vs. employeeName) so field-vs-column button locators
    // in the mapping editor can never collide on substring text.
    await addTable(page, 'employees', 'employee_name')

    // --- P1 AC1: create a webhook, get a unique URL containing a token ---
    await page.click('[role="tab"]:has-text("Webhooks")')
    await page.click('button:has-text("Add webhook")')
    await page.fill('input[placeholder="Webhook name"]', 'employee sync')
    await page.fill('input[placeholder="Event type path (e.g. eventType)"]', 'eventType')
    await page.fill('input[placeholder="Event id path (optional, for dedup)"]', 'eventId')
    await page.getByRole('button', { name: 'Save', exact: true }).click()

    // The URL is shown permanently on the webhook's own card, not just once
    // at creation — confirms it survives a reload, not only the create response.
    await expect(page.locator('code')).toBeVisible()
    const webhookUrl = (await page.locator('code').innerText()).trim()
    const urlMatch = webhookUrl.match(/\/hooks\/([^/]+)\/([^/]+)$/)
    expect(urlMatch).not.toBeNull()
    const [, webhookId, token] = urlMatch!
    expect(webhookId.length).toBeGreaterThan(0)
    expect(token.length).toBeGreaterThan(0)
    await page.reload()
    await page.click('[role="tab"]:has-text("Webhooks")')
    await expect(page.locator('code')).toHaveText(webhookUrl)

    // --- P1 AC2: while unmapped, every call is capture-only, no write ---
    const captureDecoy = await page.request.post(webhookUrl, {
      data: { eventType: 'noop', foo: 'bar' },
    })
    expect(captureDecoy.status()).toBe(200)
    expect(await rowCount(page, appName, 'employees')).toBe(0)

    // --- P1 AC3: a second capture call overwrites the stored sample, it
    // does not accumulate. The real sample used for mapping below carries
    // "employeeName" and must NOT retain "foo" from the decoy above. ---
    const captureSample = await page.request.post(webhookUrl, {
      data: { eventType: 'employee.created', eventId: 'evt-1', employeeName: 'Ada Lovelace' },
    })
    expect(captureSample.status()).toBe(200)
    expect(await rowCount(page, appName, 'employees')).toBe(0)

    // --- P1 AC4: a method mismatch (webhook is configured for POST)
    // rejects with 404 — and is NOT written to the delivery log. ---
    const methodMismatch = await page.request.get(webhookUrl)
    expect(methodMismatch.status()).toBe(404)

    // --- P1 AC5: a missing/invalid token rejects with 401, and the attempt
    // IS recorded in the delivery log. ---
    const badTokenUrl = webhookUrl.replace(token, 'wrong-token')
    const invalidToken = await page.request.post(badTokenUrl, {
      data: { eventType: 'employee.created', eventId: 'evt-bad-token', employeeName: 'Nobody' },
    })
    expect(invalidToken.status()).toBe(401)

    // Reload so the webhook list refetches with the just-captured sample
    // (useWebhooks has no polling — the two POSTs above bypassed the UI
    // entirely, so React Query's cache is still the pre-capture snapshot).
    await page.reload()
    await page.click('[role="tab"]:has-text("Webhooks")')
    await page.click('button:has-text("Map")')

    // Sample fields reflect only the LATEST capture: "employeeName" is
    // present, "foo" (from the overwritten decoy) is not (P1 AC3, cont'd).
    await expect(page.getByRole('button', { name: 'employeeName', exact: false })).toBeVisible()
    await expect(page.getByRole('button', { name: 'foo', exact: false })).toHaveCount(0)

    // --- P2 (map+activate) AC1: link a sample field to a column and save
    // an "insert" mapping for a given event-type value. ---
    await page.fill(
      'input[placeholder="Event type value (e.g. user.created)"]',
      'employee.created',
    )
    // Action select already defaults to "insert"; target table already
    // defaults to the app's only table ("employees").
    await page.getByRole('button', { name: 'employeeName', exact: false }).click()
    await page.getByRole('button', { name: 'employee_name', exact: false }).click()
    await page.click('button:has-text("Save mapping")')
    await expect(page.locator('text=Mapping saved')).toBeVisible()

    // The saved mapping's list entry must show the actual field->column
    // link, not just the event-type/action/table summary line — an app
    // owner needs to see what's really mapped without re-opening the
    // click-to-link picker. .last() targets the saved-mapping row, which
    // renders below the still-visible sample-field picker (the picker's
    // own "employeeName" button is the first match for this text).
    await expect(page.locator('text=employeeName').last()).toBeVisible()
    await expect(page.locator('text=employee_name').last()).toBeVisible()

    // --- P2 AC2: activating a webhook with at least one saved mapping
    // switches it from capture to active. ---
    await page.click('button:has-text("Activate")')
    await expect(page.locator('text=Webhook activated')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Active', exact: true })).toBeVisible()

    // --- P2 AC5: an active webhook call whose event-type value has no
    // saved mapping is a no-op (200, logged "unmapped", no write). ---
    const unmapped = await page.request.post(webhookUrl, {
      data: { eventType: 'other.event', eventId: 'evt-unmapped' },
    })
    expect(unmapped.status()).toBe(200)
    expect((await unmapped.json()).status).toBe('unmapped')
    expect(await rowCount(page, appName, 'employees')).toBe(0)

    // --- P2 AC6: a mapping that fails to resolve at the field-mapping layer
    // (its linked source field is absent from this particular payload) is a
    // write failure — 500, logged "write_error", nothing partially written. ---
    const writeError = await page.request.post(webhookUrl, {
      data: { eventType: 'employee.created', eventId: 'evt-write-error' },
    })
    expect(writeError.status()).toBe(500)
    expect(await rowCount(page, appName, 'employees')).toBe(0)

    // --- P2 AC3: a real call matching the saved "insert" mapping creates a
    // row with the mapped values. ---
    const insert = await page.request.post(webhookUrl, {
      data: { eventType: 'employee.created', eventId: 'evt-2', employeeName: 'Grace Hopper' },
    })
    expect(insert.status()).toBe(200)
    expect((await insert.json()).status).toBe('inserted')
    expect(await rowCount(page, appName, 'employees')).toBe(1)
    const written = await rows(page, appName, 'employees')
    expect(written[0].employee_name).toBe('Grace Hopper')

    // --- P2 AC4: a repeated event id (already processed) is skipped — 200,
    // logged "duplicate_skipped", no second row created. ---
    const duplicate = await page.request.post(webhookUrl, {
      data: { eventType: 'employee.created', eventId: 'evt-2', employeeName: 'Someone Else' },
    })
    expect(duplicate.status()).toBe(200)
    expect((await duplicate.json()).status).toBe('duplicate_skipped')
    expect(await rowCount(page, appName, 'employees')).toBe(1)

    // --- P1 AC6 / delivery log: every call above (except the 404
    // method-mismatch, which the design explicitly excludes from logging)
    // is recorded with the correct outcome. ---
    await page.click('button:has-text("Hide mapping")')
    await page.click('button:has-text("View deliveries")')

    // Outcome badges render through webhooks.outcome.<value> (en.json) —
    // asserting the translated label, not the raw wire-format outcome string.
    const expectedOutcomes = [
      'Captured', // decoy capture
      'Captured', // real sample capture
      'Invalid token', // bad token
      'Unmapped',
      'Write error',
      'Inserted',
      'Duplicate skipped',
    ]
    for (const outcome of expectedOutcomes) {
      await expect(page.getByText(outcome, { exact: true }).first()).toBeVisible()
    }
    await expect(page.getByText('Inserted', { exact: true })).toHaveCount(1)
    await expect(page.getByText('Duplicate skipped', { exact: true })).toHaveCount(1)
    await expect(page.getByText('Captured', { exact: true })).toHaveCount(2)

    void appId // app id only needed to build the create-app URL above
  })
})
