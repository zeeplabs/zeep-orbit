import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * Personal Access Tokens management, now on the dedicated MCP settings page
 * (.specs/features/mcp-settings-page, migrated from the mcp-server spec's
 * T16 modal).
 *
 * One test, sequential stages, following the webhooks.spec.ts /
 * policy-templates.spec.ts convention: log in once in beforeAll and reuse
 * the session via storageState (the dashboard's login endpoint is
 * rate-limited to 5 requests/minute per IP).
 *
 * Stages map to the original T16 Done-when, now exercised via direct
 * navigation to /mcp-settings instead of opening a modal: create a token
 * (assert plaintext shown once) -> reload (assert value hidden, entry
 * still listed) -> confirm the UI-created token authenticates a raw HTTP
 * request to /dashboard/mcp (before revocation) -> revoke it (assert
 * removed from the list) -> confirm the same raw request now fails (401)
 * after revocation.
 *
 * Route note: the page lives at /mcp-settings, not /mcp -- /mcp would
 * resolve to /dashboard/mcp under the router's basename, colliding with
 * the backend's own MCP transport route at that exact path.
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
  await page.goto('/dashboard/mcp-settings')
})

test('create, reload, authenticate, and revoke a personal access token', async ({ page }) => {
  const tokenName = `e2e_pat_${Date.now()}`

  // Stage 1: the MCP settings page renders the PAT section directly (no
  // modal to open) -- create a token, whose plaintext must be shown
  // exactly once (Done-when carried over from mcp-server T16).
  await expect(page.getByRole('heading', { name: 'Personal Access Tokens' })).toBeVisible()

  await page.getByRole('button', { name: 'New token' }).click()
  await page.getByLabel('Name').fill(tokenName)
  await page.getByRole('button', { name: 'Create' }).click()

  await expect(page.getByText("Copy this token now. You won't be able to see it again.")).toBeVisible()
  const revealedToken = await page.getByTestId('revealed-pat-token').innerText()
  expect(revealedToken.length).toBeGreaterThan(10)

  await page.getByRole('button', { name: 'Done' }).click()
  await expect(page.getByText(tokenName)).toBeVisible()

  // Stage 2: reload the page -- the plaintext must never be shown again,
  // only the entry itself remains listed.
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Personal Access Tokens' })).toBeVisible()
  await expect(page.getByText(tokenName)).toBeVisible()
  await expect(page.getByTestId('revealed-pat-token')).toHaveCount(0)

  // Stage 3: the UI-created token must actually authenticate a raw HTTP
  // request to /dashboard/mcp (not via a full MCP client -- a bearer-auth'd
  // request reaches the MCP protocol layer and gets a non-401 response,
  // same as an unauthenticated request there yields exactly 401).
  const authedRes = await page.request.post('/dashboard/mcp', {
    headers: {
      Authorization: `Bearer ${revealedToken}`,
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
    },
    data: {},
  })
  expect(authedRes.status()).not.toBe(401)

  // Stage 4: revoke the token -- it must disappear from the list without a
  // manual reload.
  await page.getByRole('button', { name: 'Revoke' }).first().click()
  await page.getByRole('button', { name: 'Revoke', exact: true }).last().click()
  await expect(page.getByText(tokenName)).toHaveCount(0)

  // Stage 5: the same raw request must now fail with 401 -- no propagation
  // delay, revoked immediately.
  const revokedRes = await page.request.post('/dashboard/mcp', {
    headers: {
      Authorization: `Bearer ${revealedToken}`,
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
    },
    data: {},
  })
  expect(revokedRes.status()).toBe(401)
})
