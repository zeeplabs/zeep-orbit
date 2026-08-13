import { test, expect, Page } from '@playwright/test'
import { bootstrapOrSkip, login } from './helpers'

/**
 * Policy Templates & Help Drawer (.specs/features/policy-templates).
 *
 * One test, sequential stages, each stage commented with the exact spec.md
 * acceptance criterion it covers. Follows the webhooks.spec.ts convention:
 * log in once in beforeAll and reuse the session via storageState (the
 * dashboard's login endpoint is rate-limited to 5 requests/minute per IP).
 *
 * Template rows are opened via `getByRole('button', { name: 'Use this
 * template' }).nth(N)`, N being the template's fixed position in
 * TEMPLATE_DEFINITIONS (owner_only=0, open_read=1, read_only=2,
 * value_match=3, open_read_owner_write=4 — blocked_by_default has no
 * button). This is stable because every single-action template's own
 * "Apply" resets it back to "Use this template" on success
 * (PolicyTemplatePicker's applySequentially), and switching mode away and
 * back remounts the picker fresh — so the labelled set is always back to
 * its original order by the time the next stage opens a template.
 *
 * Pre-existing, unrelated bug found while writing this suite: a freshly
 * created table's RLS select defaults to "Public", which sends rls:
 * "disabled" to the backend — a value internal/dashboard/handler.go has
 * always rejected ("table X has an invalid rls value: disabled ..."), so
 * the very first "Add table" + "Save table" with default settings 400s.
 * This reproduces identically at the T7 commit (predates T8/T9) and is out
 * of scope for policy-templates — every table below explicitly switches
 * RLS to "Restricted" right after "Add table" to route around it.
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

async function createAuthApp(page: Page, name: string): Promise<string> {
  await page.goto('/dashboard/apps')
  await page.click('button:has-text("Create app")')
  await page.click('button:has-text("Backend App")')
  await page.waitForURL('**/apps/new')
  await page.fill('input[placeholder="my-app"]', name)
  await page.click('button[role="switch"]') // enable end-user email auth (owner_id/roles)
  await page.click('button:has-text("Create app")')
  await page.waitForURL((url) => /\/apps\/(?!new$)[^/]+$/.test(url.pathname))
  const match = page.url().match(/\/apps\/([^/?]+)/)
  if (!match) throw new Error(`could not extract app id from ${page.url()}`)
  return match[1]
}

/** Adds a table with RLS explicitly switched to "Restricted" (rls:
 * "enabled", hasOwnerColumn true) — see the file-level note on the
 * pre-existing "Public"-default bug this routes around. */
/** "Add table" is disabled while another table card is expanded in edit
 * mode — collapse whichever one is currently open (if any) first. */
async function collapseOpenTable(page: Page) {
  await page.click('[role="tab"]:has-text("Database")')
  // A table's own "Schema"/"Policies" tablist only renders while that table
  // card is expanded — absent when every table is collapsed.
  const schemaTab = page.getByRole('tab', { name: 'Schema', exact: true })
  if (await schemaTab.isVisible().catch(() => false)) {
    await schemaTab.click()
    await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  }
}

async function addRestrictedTable(page: Page, tableName: string, columnName: string) {
  await collapseOpenTable(page)
  await page.click('[role="tab"]:has-text("Database")')
  await page.click('text=Add table')
  await page.fill('input[placeholder="table_name"]', tableName)
  await page.getByRole('combobox').first().click()
  await page.getByRole('option', { name: 'Restricted', exact: true }).click()
  await page.fill('input[placeholder="column_name"]', columnName)
  await page.click('text=Save table')
  await expect(page.locator('text=Save table')).toHaveCount(0)
  await page.click(`text=${tableName}`)
  await page.click('[role="tab"]:has-text("Policies")')
}

async function getPolicies(
  page: Page,
  appId: string,
  table: string,
): Promise<{ pg_policy_name: string; action: string; roles: string[]; clauses: Record<string, unknown>[] }[]> {
  const res = await page.request.get(`/dashboard/api/apps/${appId}/tables/${table}/policies`)
  expect(res.status()).toBe(200)
  return res.json()
}

const useTemplateButton = (page: Page, n: number) => page.getByRole('button', { name: 'Use this template' }).nth(n)

test.describe('Policy Templates & Help Drawer', () => {
  test('templates create policies, the composite handles partial failure, and the help drawer/mode toggle preserve state', async ({ page }) => {
    const appName = uniqueAppName('e2e_policy_tpl')
    const appId = await createAuthApp(page, appName)

    // --- Table "posts": owner_id exists (Restricted), plus a real column
    // ("status") for the value_match template. ---
    await addRestrictedTable(page, 'posts', 'status')

    // --- P1 AC1 / PTPL-07: with zero policies, the template list shows
    // exactly the 5 actionable templates (each has "Use this template") plus
    // the non-actionable "Blocked by default" affordance, visible without an
    // extra click (Edge Cases: never hidden behind an extra click). Only 5
    // buttons exist — TEMPLATE_DEFINITIONS has 6 entries, so this alone
    // proves blocked_by_default renders no actionable button. ---
    await expect(page.getByText('Blocked by default', { exact: true })).toBeVisible()
    await expect(useTemplateButton(page, 0)).toHaveCount(1) // sanity: locator resolves
    await expect(page.getByRole('button', { name: 'Use this template' })).toHaveCount(5)

    // --- P1 AC3 (PTPL-01): "Only the owner sees/edits" for 2 actions. ---
    await useTemplateButton(page, 0).click()
    await page.getByRole('button', { name: 'select', exact: true }).click()
    await page.getByRole('button', { name: 'update', exact: true }).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    await page.getByRole('button', { name: 'Apply template' }).click()
    await expect(page.locator('text=Policy created')).toBeVisible()
    await expect(page.getByText('tpl_owner_only_select', { exact: true })).toBeVisible()
    await expect(page.getByText('tpl_owner_only_update', { exact: true })).toBeVisible()
    const ownerOnlyPolicies = (await getPolicies(page, appId, 'posts')).filter((p) =>
      p.pg_policy_name.startsWith('tpl_owner_only_'),
    )
    expect(ownerOnlyPolicies.map((p) => p.action).sort()).toEqual(['select', 'update'])

    // --- P1 AC4 (PTPL-02): "All signed-in users with the selected role can
    // view" — a single select policy, no ownership clause exposed. ---
    await useTemplateButton(page, 1).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    await page.getByRole('button', { name: 'Apply template' }).click()
    await expect(page.getByText('tpl_open_read_select', { exact: true })).toBeVisible()

    // --- P1 AC8: a single-action template's create call failing surfaces
    // via toast.error and leaves no stuck state — reapplying the SAME
    // template/role now collides on the generated name (tpl_open_read_select
    // already exists from the apply above), forcing a real backend
    // rejection instead of a client-side validation short-circuit. ---
    await useTemplateButton(page, 1).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    const applyOpenReadAgain = page.getByRole('button', { name: 'Apply template' })
    await applyOpenReadAgain.click()
    // internal/dashboard/handler.go's exact 409 message for a duplicate
    // policy name, surfaced verbatim via toast.error(error.message).
    await expect(page.getByText('a policy with this name already exists on this table')).toBeVisible()
    // Not stuck: the button is enabled again and a second attempt is still
    // possible (isApplying's `finally` cleared) — assert the exact opposite
    // policy count didn't change (no duplicate silently created).
    await expect(applyOpenReadAgain).toBeEnabled()
    expect((await getPolicies(page, appId, 'posts')).filter((p) => p.pg_policy_name === 'tpl_open_read_select')).toHaveLength(1)
    await page.getByRole('button', { name: 'Close', exact: true }).click() // collapse this template's draft before moving on

    // --- P1 AC5 (PTPL-04): "Nobody edits, read-only" — exactly one select
    // policy, no action picker for this template. ---
    await useTemplateButton(page, 2).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    await page.getByRole('button', { name: 'Apply template' }).click()
    await expect(page.getByText('tpl_read_only_select', { exact: true })).toBeVisible()
    const readOnly = (await getPolicies(page, appId, 'posts')).find((p) => p.pg_policy_name === 'tpl_read_only_select')
    expect(readOnly?.action).toBe('select')

    // --- P1 AC6 (PTPL-05): "Visible when a value matches" — a real column
    // + a literal value, no Column/Operator/ValueSource shown. ---
    await useTemplateButton(page, 3).click()
    // Unlike addRestrictedTable's draft screen, the table's own RLS select
    // (header, always visible regardless of active tab) is still the FIRST
    // combobox on screen here — the value_match column select is the LAST.
    await page.getByRole('combobox').last().click()
    await page.getByRole('option', { name: 'status', exact: true }).click()
    await page.getByPlaceholder('Value').fill('published')
    await page.getByRole('button', { name: 'member', exact: true }).click()

    // --- P3 AC1/AC3 (PTPL-08): opening "Help" over an in-progress template
    // draft doesn't discard it; closing the drawer preserves the draft. ---
    await page.getByRole('button', { name: 'Help', exact: true }).click()
    const helpDialog = page.getByRole('dialog')
    await expect(page.getByText('Building an advanced policy', { exact: true })).toBeVisible()
    // spot-check one example's rendered clause text, scoped to the drawer
    // (the same clause text also appears in the policy list below it).
    await expect(helpDialog.getByText(/owner_id = claim:sub/)).toBeVisible()
    // P3 AC2: >=3 worked examples, and none of them uses anything outside
    // the real operator/claim allowlist — asserted on the drawer's actual
    // rendered text (not just a source-code comment) so a future example
    // that slips in "LIKE"/"now()"/an invented claim fails this test.
    const helpText = await helpDialog.innerText()
    const exampleCount = await helpDialog.locator('p.font-semibold').count()
    expect(exampleCount).toBeGreaterThanOrEqual(3)
    expect(helpText.toLowerCase()).not.toContain('like ')
    expect(helpText).not.toContain('now()')
    // Every "claim:X" rendered in the drawer must be one of the 3 real
    // claims — a future example slipping in an invented claim fails here.
    const claimsUsed = [...helpText.matchAll(/claim:(\w+)/g)].map((m) => m[1])
    expect(claimsUsed.length).toBeGreaterThan(0)
    for (const claim of claimsUsed) {
      expect(['role', 'sub', 'email']).toContain(claim)
    }
    await page.keyboard.press('Escape')
    await expect(page.getByText('Building an advanced policy', { exact: true })).toHaveCount(0)
    await expect(page.getByPlaceholder('Value')).toHaveValue('published')

    await page.getByRole('button', { name: 'Apply template' }).click()
    await expect(page.getByText('tpl_value_match_select', { exact: true })).toBeVisible()
    const valueMatch = (await getPolicies(page, appId, 'posts')).find((p) => p.pg_policy_name === 'tpl_value_match_select')
    expect(valueMatch?.clauses[0]).toMatchObject({ column: 'status', operator: '=', value_source: 'literal', value: 'published' })

    // --- Edge Cases: switching to "Advanced mode" carries over the role(s)
    // already picked in a template draft. Open the composite template, pick
    // "member", then flip the toggle instead of applying. ---
    await useTemplateButton(page, 4).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    await page.getByRole('switch').click() // "Advanced mode" toggle, on
    await expect(page.locator('input[placeholder="Policy name"]')).toBeVisible()
    // The advanced form's own role chip inherited the template's selection —
    // a role chip toggled "on" renders with the primary-filled classes.
    await expect(page.getByRole('button', { name: 'member', exact: true })).toHaveAttribute(
      'class',
      /bg-\[var\(--primary\)\]/,
    )

    // --- T8 AC: the toggle round-trips back to the template list. ---
    await page.getByRole('switch').click() // off
    await expect(page.getByText('Blocked by default', { exact: true })).toBeVisible()
    await expect(page.locator('input[placeholder="Policy name"]')).toHaveCount(0)

    // --- P2 AC1 (PTPL-06) happy path: the composite creates all 3 policies
    // (select, update, delete) in one apply. ---
    await useTemplateButton(page, 4).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    await page.getByRole('button', { name: 'Apply template' }).click()
    await expect(page.getByText('select: created', { exact: true })).toBeVisible()
    await expect(page.getByText('update: created', { exact: true })).toBeVisible()
    await expect(page.getByText('delete: created', { exact: true })).toBeVisible()
    const compositeHappy = (await getPolicies(page, appId, 'posts')).filter((p) =>
      p.pg_policy_name.startsWith('tpl_open_read_owner_write_'),
    )
    expect(compositeHappy.map((p) => p.action).sort()).toEqual(['delete', 'select', 'update'])

    // --- P2 AC2/AC3 (PTPL-06 partial failure + retry-skip): a second table,
    // isolated from "posts", so the collision below is unambiguous. ---
    await addRestrictedTable(page, 'comments', 'body')

    // The picker's existingPolicies snapshot (fetched when this tab mounted)
    // is empty right now. Creating a same-named policy out-of-band, through
    // the raw API instead of the app's own mutation flow, leaves that
    // snapshot stale — the picker will NOT pre-emptively skip it, so the
    // sequential apply below genuinely attempts (and fails) the "update"
    // call instead of skipping it. This is what forces AC2's failure, as
    // opposed to AC3's skip-on-retry (exercised right after).
    const collision = await page.request.post(`/dashboard/api/apps/${appId}/tables/comments/policies`, {
      data: {
        name: 'tpl_open_read_owner_write_update',
        action: 'update',
        roles: ['member'],
        clauses: [{ column: 'owner_id', operator: 'IS NOT NULL' }],
      },
    })
    expect(collision.status()).toBe(201)

    await useTemplateButton(page, 4).click()
    await page.getByRole('button', { name: 'member', exact: true }).click()
    await page.getByRole('button', { name: 'Apply template' }).click()

    // select succeeded, update failed (name collision), delete never attempted.
    await expect(page.getByText('select: created', { exact: true })).toBeVisible()
    await expect(page.getByText(/^update: failed/)).toBeVisible()
    await expect(page.getByText('delete: pending', { exact: true })).toBeVisible()
    const afterFailure = await getPolicies(page, appId, 'comments')
    expect(afterFailure.some((p) => p.pg_policy_name === 'tpl_open_read_owner_write_select')).toBe(true)
    expect(afterFailure.some((p) => p.pg_policy_name === 'tpl_open_read_owner_write_delete')).toBe(false)

    // --- P2 AC3: reapplying retries only what's still pending (delete) and
    // skips both select (created above) and update (the collision) — no
    // duplicate-create attempt against either. ---
    await page.getByRole('button', { name: 'Apply template' }).click()
    await expect(page.getByText('select: already exists, skipped', { exact: true })).toBeVisible()
    await expect(page.getByText('update: already exists, skipped', { exact: true })).toBeVisible()
    await expect(page.getByText('delete: created', { exact: true })).toBeVisible()
    const afterRetry = await getPolicies(page, appId, 'comments')
    expect(afterRetry.some((p) => p.pg_policy_name === 'tpl_open_read_owner_write_delete')).toBe(true)
    expect(afterRetry.filter((p) => p.pg_policy_name === 'tpl_open_read_owner_write_select')).toHaveLength(1)
    expect(afterRetry.filter((p) => p.pg_policy_name === 'tpl_open_read_owner_write_update')).toHaveLength(1)
  })
})
