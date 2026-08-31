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
// Copy-to-clipboard assertions below need the browser's clipboard
// permission granted up front -- headless Chromium denies it by default,
// and MCPPage's copyToClipboard() silently swallows a denied write (same
// as Webhooks.tsx's pattern), so without this the toast would never fire
// and the test would fail for the wrong reason.
test.use({ permissions: ['clipboard-read', 'clipboard-write'] })

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

  // Purge any tokens left over from a previous run's retry (a failed
  // attempt above the revoke step -- e.g. Stage 4/5 below -- leaves its
  // token behind; Playwright then reruns the whole test, which creates a
  // second token, and unscoped assertions on shared account state start
  // matching more than one row). Each test creates and cleans up its own
  // token(s), so nothing here should ever legitimately exist beforehand.
  //
  // Scoped to this suite's own `e2e_pat_` name prefix (both create sites
  // below use it), not every token on the account -- this endpoint runs
  // against whatever BASE_URL points at, and an unscoped delete-everything
  // here would silently revoke a real user's tokens if that were ever
  // something other than a disposable test environment.
  const res = await page.request.get('/dashboard/api/me/pats')
  if (res.ok()) {
    const pats: Array<{ id: string; name: string }> = await res.json()
    for (const pat of pats) {
      if (!pat.name.startsWith('e2e_pat_')) continue
      await page.request.delete(`/dashboard/api/me/pats/${pat.id}`)
    }
  }

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

  // MCPUI-10: a token that has never authenticated a request shows "Never
  // used", not a last-used date. Scoped to this token's own row
  // (data-testid, keyed on the token's id -- names aren't unique, so keying
  // on tokenName the way this used to would break the moment two tokens
  // shared a name) rather than a bare page-wide text match -- with the
  // account-cleanup in beforeEach this is now the only row on the page,
  // but scoping costs nothing and matches the pattern used below where it
  // does matter.
  const patsRes = await page.request.get('/dashboard/api/me/pats')
  const [{ id: patId }] = (await patsRes.json()) as Array<{ id: string; name: string }>
  const patRow = page.getByTestId(`pat-row-${patId}`)
  await expect(patRow.getByText('Never used')).toBeVisible()

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

  // MCPUI-10: once a token has authenticated a request, its row switches
  // from "Never used" to a "Last used" date. Scoped to this token's row --
  // unscoped, a leftover token from a prior failed/retried run (see
  // beforeEach) would render its own "Last used" text and turn this into a
  // strict-mode violation (2 matching elements) instead of the intended
  // assertion.
  await page.reload()
  await expect(patRow.getByText(/^Last used /)).toBeVisible()

  // Stage 4: revoke the token -- it must disappear from the list without a
  // manual reload. The icon click is scoped to this row so a second,
  // unrelated row (if one somehow exists) can't absorb it.
  await patRow.getByRole('button', { name: 'Revoke' }).click()
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

  // MCPUI-08: both auth methods are explained, and there is no interactive
  // control that would drive the OAuth flow from the dashboard itself
  // (Out of Scope in spec.md -- this page only documents OAuth, Claude
  // Desktop's own client performs it).
  await expect(page.getByText('Personal Access Token (PAT)')).toBeVisible()
  await expect(page.getByText('OAuth 2.1 + PKCE')).toBeVisible()
  await expect(page.getByRole('button', { name: /connect/i })).toHaveCount(0)
  await expect(page.getByRole('link', { name: /connect/i })).toHaveCount(0)

  // MCPUI-07: all 4 clients are present, and each one's snippet embeds the
  // live endpoint (not the README's <host> placeholder) plus the
  // ZEEP_ORBIT_PAT env var reference, matching README.md's blocks. Codex's
  // TOML config references the env var by bare name
  // (`bearer_token_env_var = "ZEEP_ORBIT_PAT"`), unlike the 3 JSON
  // clients' `"Bearer ${ZEEP_ORBIT_PAT}"` header interpolation -- asserting
  // the `${...}` form for all 4 made this loop fail deterministically on
  // Codex before it ever reached the copy-toast assertions below.
  for (const client of ['Claude Code', 'Codex', 'Cursor', 'OpenCode']) {
    const snippetEl = page.getByTestId(`mcp-client-snippet-${client}`)
    await expect(snippetEl).toBeVisible()
    const snippet = await snippetEl.getAttribute('data-snippet')
    expect(snippet).toContain(endpoint)
    expect(snippet).toContain('ZEEP_ORBIT_PAT')
    expect(snippet).not.toContain('<host>')
  }

  // MCPUI-06/09: clicking each of the two distinct CopyButton call sites
  // (endpoint, and one client snippet) must show the real "copied" toast,
  // not the button's own title/tooltip text -- guards against regressing
  // the copy-success-toast bug fixed in aecece1.
  await page.getByTitle('Copy endpoint URL').click()
  await expect(page.getByText('Copied to clipboard')).toBeVisible()

  await page.getByTestId('mcp-client-card-Claude Code').getByTitle('Copy config').click()
  await expect(page.getByText('Copied to clipboard')).toBeVisible()
})

test('MCP nav link navigates to the settings page and is reachable on mobile', async ({ page }) => {
  // MCPUI-02: click-through navigation, not just a direct URL load --
  // guards against reintroducing the exact /mcp route collision fixed in
  // 6ec4add (a regression there would 404/misbehave rather than land on
  // this page).
  await page.goto('/dashboard/apps')
  await page.getByRole('link', { name: 'MCP' }).click()
  await expect(page).toHaveURL(/\/dashboard\/mcp-settings$/)
  await expect(page.getByRole('heading', { name: 'MCP', level: 1 })).toBeVisible()

  // MCPUI-04: the mobile "More" bottom sheet surfaces the same nav model
  // (NAV_SECTIONS), so the MCP entry is reachable on a narrow viewport too.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/dashboard/apps')
  await page.getByRole('button', { name: 'More' }).click()
  await expect(
    page.getByTestId('mobile-nav-sheet').getByRole('link', { name: 'MCP' }),
  ).toBeVisible()
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
