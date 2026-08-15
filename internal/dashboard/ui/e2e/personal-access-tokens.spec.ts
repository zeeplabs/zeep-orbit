import { test, expect } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * MCP settings page (.specs/features/mcp-settings-page), including the
 * Personal Access Tokens management migrated from the mcp-server spec's
 * T16 modal.
 *
 * 4 tests, following the webhooks.spec.ts / policy-templates.spec.ts
 * convention: log in once in beforeAll and reuse the session via
 * storageState (the dashboard's login endpoint is rate-limited to 5
 * requests/minute per IP). Tests run sequentially (playwright.config.ts:
 * fullyParallel: false, workers: 1) against one shared account, so test
 * order matters -- the lifecycle test runs first and ends with zero
 * active tokens, which the empty-state assertion at its end and the two
 * later tests both rely on / restore.
 *
 * 1. Full lifecycle: create a token (assert plaintext shown once) ->
 *    reload (assert value hidden, entry still listed) -> confirm the
 *    UI-created token authenticates a raw HTTP request to /dashboard/mcp
 *    (before revocation) -> revoke it (assert removed from the list) ->
 *    confirm the same raw request now fails (401) after revocation ->
 *    assert the empty state renders now that zero tokens remain.
 * 2. MCP discovery + tutorials: labeled sidebar nav item, old unlabeled
 *    key-icon button gone, live endpoint URL, PAT vs OAuth 2.1 explainer,
 *    and all 4 client config snippets (endpoint substituted, no <host>
 *    placeholder left over).
 * 3. Create failure: mocked 500 -> error toast, form stays open, no token
 *    revealed.
 * 4. Revoke failure: mocked 500 -> error toast, token stays listed; then
 *    revokes it for real (unmocked) so it doesn't linger in the shared
 *    account across other e2e runs.
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

  // Stage 6: this test suite creates and revokes exactly one token per run
  // against a fresh bootstrap, so after the revoke above the account has
  // zero active tokens -- the empty state must render instead of a blank
  // list (MCPUI-13).
  await expect(page.getByText('No personal access tokens')).toBeVisible()
})

test('renders MCP discovery and per-client connection tutorials', async ({ page }) => {
  // MCPUI-01/03: a labeled "MCP" sidebar link exists; the old unlabeled
  // key-icon button (which used to open a modal, accessible name
  // "Personal Access Tokens") no longer exists as a button anywhere --
  // that name now only labels the page's <h3> section heading.
  await expect(page.getByRole('link', { name: 'MCP' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Personal Access Tokens' })).toHaveCount(0)

  // MCPUI-05: endpoint URL is the live origin + /dashboard/mcp, not a
  // placeholder.
  const endpoint = await page.getByTestId('mcp-endpoint-url').innerText()
  expect(endpoint).toBe(`${new URL(page.url()).origin}/dashboard/mcp`)

  // MCPUI-08: both auth methods are explained.
  await expect(page.getByText('Personal Access Token (PAT)')).toBeVisible()
  await expect(page.getByText('OAuth 2.1 + PKCE')).toBeVisible()

  // MCPUI-07: all 4 clients are present, and each one's snippet embeds the
  // live endpoint (not the README's <host> placeholder) plus the
  // ${ZEEP_ORBIT_PAT} env var reference, matching README.md's blocks.
  for (const client of ['Claude Code', 'Codex', 'Cursor', 'OpenCode']) {
    const snippetEl = page.getByTestId(`mcp-client-snippet-${client}`)
    await expect(snippetEl).toBeVisible()
    const snippet = await snippetEl.getAttribute('data-snippet')
    expect(snippet).toContain(endpoint)
    expect(snippet).toContain('${ZEEP_ORBIT_PAT}')
    expect(snippet).not.toContain('<host>')
  }
})

test('shows an error and keeps the form open when token creation fails', async ({ page }) => {
  await page.route('**/dashboard/api/me/pats', (route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 500, body: JSON.stringify({ error: 'internal error' }) })
    }
    return route.continue()
  })

  const tokenName = `e2e_pat_fail_${Date.now()}`
  await page.getByRole('button', { name: 'New token' }).click()
  await page.getByLabel('Name').fill(tokenName)
  await page.getByRole('button', { name: 'Create' }).click()

  // MCPUI-14: create failure surfaces a toast, doesn't reveal a token, and
  // leaves the create form open (not silently closed as if it had worked).
  await expect(page.getByText('internal error')).toBeVisible()
  await expect(page.getByTestId('revealed-pat-token')).toHaveCount(0)
  await expect(page.getByLabel('Name')).toHaveValue(tokenName)
})

test('shows an error and keeps the token listed when revocation fails', async ({ page }) => {
  const tokenName = `e2e_pat_revokefail_${Date.now()}`
  await page.getByRole('button', { name: 'New token' }).click()
  await page.getByLabel('Name').fill(tokenName)
  await page.getByRole('button', { name: 'Create' }).click()
  await expect(page.getByTestId('revealed-pat-token')).toBeVisible()
  await page.getByRole('button', { name: 'Done' }).click()

  await page.route('**/dashboard/api/me/pats/*', (route) => {
    if (route.request().method() === 'DELETE') {
      return route.fulfill({ status: 500, body: JSON.stringify({ error: 'internal error' }) })
    }
    return route.continue()
  })

  await page.getByRole('button', { name: 'Revoke' }).first().click()
  await page.getByRole('button', { name: 'Revoke', exact: true }).last().click()

  // MCPUI-14: revoke failure surfaces a toast and does NOT optimistically
  // remove the token from the list.
  await expect(page.getByText('internal error')).toBeVisible()
  await expect(page.getByText(tokenName)).toBeVisible()

  // Clean up for real so this token doesn't linger across other e2e runs
  // sharing the same account.
  await page.unroute('**/dashboard/api/me/pats/*')
  await page.getByRole('button', { name: 'Revoke' }).first().click()
  await page.getByRole('button', { name: 'Revoke', exact: true }).last().click()
  await expect(page.getByText(tokenName)).toHaveCount(0)
})
