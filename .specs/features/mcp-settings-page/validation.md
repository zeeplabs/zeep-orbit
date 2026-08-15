# MCP Settings Page Validation

## Validation: MCP Settings Page — Round 2 — FAIL ❌

**Date**: 2026-08-15
**Spec**: `.specs/features/mcp-settings-page/spec.md`
**Diff range (full feature)**: `28290f5^..cdce3e5` (9 commits, `develop`)
**Fix-round commits reviewed**: `aecece1`, `c8765a6`, `eb0db4f`, `cdce3e5` (all after round-1's report at `9efdc83`)
**Verifier**: independent sub-agent (author ≠ verifier), round 2 of a max-3 fix→re-verify bound

---

## Round 1 (for history)

Round 1 (`.specs/features/mcp-settings-page/validation.md` as of `9efdc83`) returned **FAIL** with 5 ranked gaps:

1. Zero test coverage for P1/P2 (MCPUI-01..09)
2. P3 empty-state/error-path untested (MCPUI-13/14)
3. `CHANGELOG.md` not updated
4. Discrimination sensor blocked — 0/3 mutations executed (sandbox Docker instability)
5. Stale README PAT-location text (`README.md:383`/`:389`, cited as saying "Dashboard → Settings → Personal Access Tokens")
6. A real bug, found by direct code inspection: `CopyButton` reused its title/aria-label text as the `toast.success` message instead of a dedicated "Copied to clipboard" string.

**Correction to the historical record**: gap 5's citation does not hold up. `git log --oneline --all -- README.md` shows the *only* commit that ever touched README.md's MCP/PAT content is `c8765a6` (this fix round) — at `9efdc83`, the commit round 1 validated, `README.md` had **zero** occurrences of "MCP Server", "Personal Access Tokens", or "Dashboard →" (confirmed via `git show 9efdc83:README.md | grep -in mcp`, only line 522's roadmap bullet "MCP server for zeep-orbit operations" matched). There was no README prose to be stale — round 1 cited line numbers and quoted text that were never in the repository at that commit. This doesn't change the outcome (the fix round added a complete, correct section regardless — see below) but it means round 1's gap 5 evidence was fabricated, not a real finding. Flagged as a lesson.

---

## Fix-Round Commit Audit

| Commit | Content | Verified |
| --- | --- | --- |
| `aecece1` fix: copy-success toast | `CopyButton` split into `label`/`successMessage` props; all 3 call sites updated | ✅ Confirmed by reading `MCPPage.tsx` — see below |
| `c8765a6` docs: CHANGELOG + README | `## [Unreleased]` entry added; full "🔌 MCP Server" section added to README.md and mirrored in all 3 translations | ✅ Confirmed |
| `eb0db4f` test: MCP discovery + error paths | 3 new e2e tests + empty-state assertion appended to the lifecycle test + `data-testid`/`data-snippet` attrs | ✅ Confirmed |
| `cdce3e5` docs: persist round-1 report + lessons | `.specs/features/mcp-settings-page/validation.md`, `.specs/LESSONS.md`, `.specs/lessons.json` | ✅ Present |

### Copy-success toast bug — genuinely fixed

`internal/dashboard/ui/src/pages/MCPPage.tsx:34-55` — `CopyButton` now takes `value`, `label`, and `successMessage` as three distinct props; `label` drives `title`/`aria-label` only, `successMessage` is passed to `copyToClipboard` (`MCPPage.tsx:24-32`) and used in the `toast.success` call. All 3 call sites pass a dedicated string, not the label:

- `MCPPage.tsx:135` — endpoint copy: `successMessage={t("mcp.copySuccess")}`
- `MCPPage.tsx:197` — per-client snippet copy: `successMessage={t("mcp.copySuccess")}`
- `MCPPage.tsx:258` — revealed-PAT copy: `successMessage={t("pats.copySuccess")}`

Locale check (`src/locales/en.json`): `mcp.copySuccess` = `"Copied to clipboard"`, `pats.copySuccess` = `"Copied to clipboard"` — distinct from `mcp.copyEndpoint` (`"Copy endpoint URL"`) and `mcp.copyConfig` (`"Copy config"`). Both keys present in `en.json` and `pt-BR.json` (static check, `python3 -c "import json; ..."`). The bug is closed at the code and locale level. **No test clicks a `CopyButton` and asserts the toast text** — the fix is code-verified only, not test-covered (see MCPUI-06/09 below).

---

## Spec-Anchored Acceptance Criteria (re-derived, round 2)

### P1: Discover and open MCP setup

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-01: nav item labeled `nav.mcp` | Sidebar renders "MCP" text | `e2e/personal-access-tokens.spec.ts:124` — `expect(page.getByRole('link', { name: 'MCP' })).toBeVisible()` | ✅ PASS — test evidence closes round 1's gap |
| MCPUI-02: click navigates to `/mcp-settings` | Route renders `MCPPage` after a nav click | No test clicks the nav link; the only navigation to `/mcp-settings` is a direct `page.goto('/dashboard/mcp-settings')` in `beforeEach` (`spec.ts:51`). `App.tsx:138` route registration is code-verified but the click→navigate behavior itself is untested. | ❌ GAP — still open, round 1 did not flag this distinctly but it is the exact behavior MCPUI-02 states ("WHEN the user clicks... THEN... navigate") |
| MCPUI-03: old key-icon button gone | No button with that identity remains | `spec.ts:125` — `expect(page.getByRole('button', { name: 'Personal Access Tokens' })).toHaveCount(0)` | ✅ PASS — reasonable proxy (the label now only exists on the page's `<h3>` section heading, not a button); round 1 gap closed |
| MCPUI-04: mobile nav equivalent access | `/mcp-settings` reachable from mobile nav | No test exercises a mobile viewport/breakpoint. `MobileNav.tsx` sharing `NAV_SECTIONS` with desktop remains code-verified only. | ❌ GAP — unchanged from round 1, not addressed by the fix round |

**Status**: ⚠️ 2/4 closed (MCPUI-01, -03), 2/4 still open (MCPUI-02, -04) — improvement from 0/4, not full closure.

### P2: Understand how to connect an MCP client

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-05: endpoint URL = `origin + '/dashboard/mcp'` | Exact computed value shown | `spec.ts:129-130` — `expect(endpoint).toBe(`${new URL(page.url()).origin}/dashboard/mcp`)` | ✅ PASS — exact-value assertion, closes round 1's gap |
| MCPUI-06: copy icon on endpoint → clipboard + success toast | Clipboard write + `toast.success` | No test clicks the endpoint `CopyButton`. Code-verified only (`MCPPage.tsx:135`). | ❌ GAP — unchanged; the underlying bug this criterion covers *was* fixed (see above) but no regression test exists for either the copy action or the toast text |
| MCPUI-07: 4 client sections, snippet = README content with host substituted, PAT var kept | Snippet text matches spec | `spec.ts:139-146` — loops all 4 clients, asserts `data-snippet` attribute `toContain(endpoint)`, `toContain('${ZEEP_ORBIT_PAT}')`, `not.toContain('<host>')` | ✅ PASS — precise, closes round 1's gap |
| MCPUI-08: explains PAT vs OAuth 2.1+PKCE, no interactive connect action | Explanatory text present; no OAuth-driving control | `spec.ts:133-134` — asserts both explainer strings visible. The "no interactive OAuth connect action" negative clause is **not** asserted (no test checks the absence of such a button); code-verified only via `grep -n "authorize\|oauth" MCPPage.tsx` (only prose string) | ⚠️ Partial — main clause closed, negative sub-clause still a code-inspection-only gap |
| MCPUI-09: copy icon on client snippet → clipboard + toast | Clipboard write of exact snippet + toast | No test clicks a per-client `CopyButton`. Code-verified only (`MCPPage.tsx:197`). | ❌ GAP — unchanged |

**Status**: ⚠️ 2/5 closed (MCPUI-05, -07), 1/5 partial (MCPUI-08), 2/5 still open (MCPUI-06, -09).

### P3: Manage personal access tokens from the same page

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-10: list non-revoked PATs, name + last-used/never-used | Filter `!pat.revoked_at`; name + date-or-"never used" | Filter: `MCPPage.tsx:214`. Name: `spec.ts:71,77` (token name visible after create/reload). Last-used/never-used text (`pats.lastUsed`/`pats.neverUsed`) is **still not asserted** by any test — `grep -n "Never used\|neverUsed\|lastUsed" e2e/personal-access-tokens.spec.ts` returns nothing. | ⚠️ Partial — same partial state as round 1, not improved |
| MCPUI-11: create → reveal once → never again | `createdToken` shown once, cleared, never re-shown | `spec.ts:60-78` — creates, asserts reveal + warning, reloads, asserts `revealed-pat-token` count 0 | ✅ PASS — now executable-in-principle (was "present but unexecuted" in round 1; test logic unchanged, still correct) |
| MCPUI-12: revoke requires confirm dialog | Confirm gate before mutation | `spec.ts:96-98` (lifecycle), plus `spec.ts:184-197` (revoke-failure test) — both click the row action then a separate confirm-dialog "Revoke" button | ✅ PASS |
| MCPUI-13: empty list → `EmptyState` | Render `EmptyState`, not blank | `spec.ts:116` — `expect(page.getByText('No personal access tokens')).toBeVisible()` after the lifecycle test's final revoke | ✅ PASS — round 1 gap closed |
| MCPUI-14: create/revoke failure → `toast.error(error.message)`, no optimistic removal, form stays open | Exact error text shown; state unchanged | Create: `spec.ts:149-167` — mocks `POST .../pats` → 500 `{error:'internal error'}`; asserts `'internal error'` text visible, `revealed-pat-token` count 0, name input retains value (form still open). Revoke: `spec.ts:169-198` — mocks `DELETE .../pats/*` → 500; asserts error text visible and token name still listed. | ✅ PASS — round 1 gap closed, and the asserted values (`'internal error'` = the mocked `error.message`) precisely match the spec-defined outcome |
| MCPUI-15: all strings in both locales | No raw key rendered | Static check (this session): `mcp.copySuccess`, `pats.copySuccess`, `common.copyToClipboard`, `nav.mcp` all present in both `en.json` and `pt-BR.json` | ✅ PASS (static, unchanged from round 1) |

**Status**: ✅ 4/6 closed or already-passing (MCPUI-11, -12, -13, -14, -15 — 5 of 6), 1/6 partial (MCPUI-10, last-used/never-used text still untested).

### Aggregate

- Round 1: 1/15 ACs test-evidenced (static), 2/15 test-code-present-but-unexecuted, 12/15 no test evidence at all.
- Round 2: **9/15 ACs now have file:line test evidence with spec-matched assertions** (MCPUI-01, -03, -05, -07, -11, -12, -13, -14, -15), **1/15 partial** (MCPUI-08 — main clause tested, negative sub-clause not), **1/15 still partial as before** (MCPUI-10 — filter+name tested, last-used/never-used text not), **4/15 still have zero test evidence** (MCPUI-02, -04, -06, -09).

This is real, substantial progress (0/15 → 9/15 fully closed) but **not full closure** — 4 ACs remain completely untested and 2 are partial.

---

## Edge Cases

- [x] `usePATs` loading → `LoadingState` — code-verified (`MCPPage.tsx:297-298`), not test-covered (unchanged from round 1).
- [x] Clipboard API unavailable → value stays visible — code-verified (`MCPPage.tsx:24-32` catch), not test-covered (unchanged).
- [x] Empty/whitespace token name → create disabled — code-verified (`MCPPage.tsx:284` `disabled={!name.trim()...}`), not test-covered (unchanged).
- [x] Direct navigation to `/mcp-settings` via URL → same page — **now exercised by every test** via `beforeEach`'s `page.goto('/dashboard/mcp-settings')` (`spec.ts:51`), though still not executed live this session (see Gate Check).

---

## Discrimination Sensor

**Live e2e attempt (2 independent tries, per the 1-2-honest-attempts guidance)**:

1. Confirmed Docker is available (`docker info`); noted a long-running `zeep-orbit-db-1` (this repo's own `docker-compose.yml` db, up 27 min, healthy) — **not used**, to avoid running e2e tests against what looked like a live/shared dev database rather than a disposable one.
2. Spun up a fresh throwaway `postgres:16-alpine` (`zorbit-verify-pg2`, port 15501). `pg_isready` and `docker exec ... psql -c "select 1"` both succeeded — Postgres itself is healthy and reachable from *inside* the container's own network namespace.
3. Built `go build -o /tmp/zeep-verify ./cmd/zeep` — clean, exit 0.
4. **Attempt 1**: ran the binary with `DATABASE_URL=postgres://zeep:zeep@127.0.0.1:15501/zeep?sslmode=disable`, `DASHBOARD_BOOTSTRAP_SECRET=test-secret`. Failed: `error: db: ping failed: ... read: connection reset by peer`.
5. **Attempt 2**: waited 5s, retried identically. Same exact failure.
6. Cleaned up (`docker rm -f zorbit-verify-pg2`).

This is the identical failure signature round 1 (and the feature's own author, per `eb0db4f`'s commit message) hit — host→container port-forwarded Postgres connections reset mid-handshake in this sandbox, while the container is internally healthy. Confirmed as a **reproducible environment limitation**, not something this pass could work around in the allotted attempts. Stopping here per instructions rather than continuing to retry.

**Fell back to static/reasoned fault-injection** in a scratch git worktree (`git worktree add /tmp/zorbit-verify-scratch cdce3e5 --detach`; real tree baseline `git status --porcelain` was empty before and confirmed empty after `git worktree remove --force`):

| # | File:line | Mutation | Reasoned against current test code | Result |
| --- | --- | --- | --- | --- |
| 1 | `MCPPage.tsx:214` | `allPats?.filter((pat) => !pat.revoked_at)` → `allPats?.filter((pat) => pat.revoked_at)` (list shows only *revoked* tokens, hides active ones) | The lifecycle test creates a token, dismisses the reveal, then asserts `expect(page.getByText(tokenName)).toBeVisible()` (`spec.ts:71`) *before* any revoke — a freshly created token has `revoked_at = null`, so the mutated filter (`pat.revoked_at` truthy-check on `null`) would exclude it, and this assertion would fail. Additionally, the mutated filter would make the post-revoke empty-state assertion (`spec.ts:116`) fail too (revoked token would now *pass* the filter and render, so the empty state would never show). | ✅ Reasoned-killed (2 independent assertions would fail) |
| 2 | `nav.ts:46` | `path: '/mcp-settings'` → `path: '/mcp'` (reintroduces the exact route collision with the backend's `/dashboard/mcp` MCP transport route that `6ec4add` fixed) | The new discovery test only asserts the nav link is *visible* (`spec.ts:124`), never clicks it or checks the resulting URL/href. Every test's actual navigation to the page goes through `page.goto('/dashboard/mcp-settings')` directly (`spec.ts:51`), which is unaffected by `nav.ts`. No assertion anywhere reads the link's target path. | ❌ Reasoned-survived — confirmed gap, matches MCPUI-02's untested state above |
| 3 | `MCPPage.tsx:21` | `` `${base}/dashboard/mcp` `` → `` `${base}/dashboard/mcp-x` `` (wrong endpoint URL) | `spec.ts:129-130` asserts `expect(endpoint).toBe(`${origin}/dashboard/mcp`)` — an exact-string equality check against the spec's literal path, independent of the mutated value. This would fail immediately. | ✅ Reasoned-killed |

`npx tsc -b` was attempted in the scratch worktree but the worktree has no `node_modules` (a fresh git worktree doesn't carry installed dependencies) and picked up a newer globally-installed `tsc` (7.0.2) that rejects this project's pinned `tsconfig.json` (`baseUrl` removed in that version) — an environment artifact of the scratch copy, not a finding about the mutations themselves. Not pursued further since these are runtime string/logic mutations, not type-level changes, and the real working tree's `tsc -b`/`build` already passed clean before mutation (see Gate Check) confirming these exact lines type-check fine as-is.

**Sensor depth**: lightweight (3 targeted mutations, reasoned since live execution is blocked)
**Result**: 2/3 reasoned-killed, 1/3 reasoned-survived — the route-path mutation (`nav.ts:46`) is not caught by any current test. This directly corresponds to the still-open MCPUI-02 gap above (no click-through navigation test) and should be the top priority for a third fix round.

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ — `aecece1` touches only `MCPPage.tsx`; `eb0db4f` touches only the spec file + 2 test-id attributes on `MCPPage.tsx`; `c8765a6` touches only `CHANGELOG.md` + the 4 READMEs |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ — `data-testid`/`data-snippet` convention matches other pages' test-hook patterns; mocked-failure tests use Playwright's `page.route` the same way other specs in this repo likely would |
| Tests map to acceptance criteria and are non-shallow (spot-check one story) | ✅ — spot-checked P3: the create-failure and revoke-failure tests each mock exactly one endpoint/verb, assert the real `error.message` text (not a placeholder), and assert the negative (no optimistic removal / form stays open) rather than just the positive toast |
| Spec-anchored outcome check: each test's asserted value matches the spec-defined outcome (or gap flagged) | ✅ where tests exist — flagged above (MCPUI-02, -04, -06, -09 no test; MCPUI-08, -10 partial) |
| Per-layer Coverage Expectation met: domain logic has 1:1 AC mapping; routes/e2e cover happy + edge + error paths for every route in scope | ❌ — the one route in scope (`/mcp-settings`) now has happy-path, empty-state, and 2 error-path tests (real improvement), but still no click-through-navigation test and no copy-action tests |
| Every test in scope maps to a spec AC, listed edge case, or Done-when criterion (no unclaimed tests) | ✅ — every new assertion traces to a named MCPUI-* in code comments (`spec.ts:120,127,132,136,162,187`) |
| Documented project quality/testing guidelines followed (cite guideline file, or "none - strong defaults applied") | ✅ — `AGENTS.md` §6 CHANGELOG requirement now satisfied (`CHANGELOG.md`'s `## [Unreleased]` → `### Added` has the MCP page entry, `git diff 9efdc83..cdce3e5 -- CHANGELOG.md` shows the addition); §6 translation-parity rule satisfied (all 4 READMEs carry the identical "Dashboard → MCP" phrasing, confirmed via grep across `README.md` + 3 `i18n/README.*.md`) |

---

## Gate Check

- **Gate command**: `cd internal/dashboard/ui && npx tsc -b && npm run build`
- **Result**: both exited 0 this session. `tsc -b`: no output (clean). `vite build`: 489 modules transformed, same pre-existing "chunk larger than 500kB" advisory (unrelated, present before this feature too).
- **Test count before feature** (round 1's baseline): 10 e2e spec files, `personal-access-tokens.spec.ts` had 1 test.
- **Test count after this fix round**: still 10 e2e spec files; `personal-access-tokens.spec.ts` now has **4 tests** (lifecycle + empty-state assertion appended, MCP discovery/tutorials, create-failure, revoke-failure).
- **Delta**: +3 new test cases in the existing file (no new spec files created).
- **Skipped tests**: none in the suite definition.
- **Live execution**: still not achieved in this sandbox — see Discrimination Sensor for the 2 attempts and the exact failure signature. The e2e suite's real pass/fail status remains genuinely unknown in this environment across two independent Verifier sessions now.
- **Failures**: none in the commands that could run (`tsc -b`, `vite build`).

---

## Fix Plans (remaining after round 2)

### Fix 1 (was round-1 Fix 1, now narrower): No click-through nav test — MCPUI-02, and the one confirmed-surviving mutant

- **Root cause**: the new discovery test asserts the nav link is visible but never clicks it and checks the resulting URL. This is also exactly why the discrimination sensor's `nav.ts` route-path mutation survives.
- **Fix task**: add `await page.getByRole('link', { name: 'MCP' }).click(); await page.waitForURL('**/mcp-settings')` (or equivalent) to the discovery test, replacing or supplementing the current direct `page.goto` in `beforeEach` for at least one test.
- **Priority**: Major (this is the one gap directly tied to a confirmed-surviving mutant, i.e., a real regression this suite would currently miss).

### Fix 2: No test exercises either `CopyButton` (endpoint or per-client) — MCPUI-06, -09

- **Root cause**: the discovery test reads `data-snippet`/`innerText` directly rather than clicking the copy icons; no test asserts a clipboard write or the `toast.success("Copied to clipboard")` text.
- **Fix task**: click each `CopyButton`, mock/stub `navigator.clipboard.writeText` (Playwright supports this via `page.evaluate` context injection or `context.grantPermissions(['clipboard-write'])` + reading `navigator.clipboard.readText()`), assert the toast text appears. This is also the only test-level guard against the exact bug round 1 found and `aecece1` fixed — right now a regression of that same bug would not be caught by any automated test.
- **Priority**: Major (protects a bug that has already shipped once).

### Fix 3: MCPUI-10 last-used/never-used text untested; MCPUI-04 mobile nav untested; MCPUI-08 negative OAuth-button clause untested

- **Root cause**: not addressed by this fix round.
- **Fix task**: (a) assert `pats.neverUsed` text for a freshly created token; (b) add a mobile-viewport test (`test.use({ viewport: ... })` or a dedicated project) asserting `/mcp-settings` is reachable from `MobileNav`; (c) assert no OAuth-triggering control exists near the auth explainer (e.g., no button/link with an "authorize"/"connect" accessible name).
- **Priority**: Minor (all three are code-verified correct; coverage gaps, not known defects).

### Fix 4 (round 1's Fix 3 and Fix 4 — closed, no action needed)

- CHANGELOG entry: ✅ present and substantive.
- README stale text: round 1's specific citation didn't correspond to real content (see "Correction to the historical record" above); the fix round nonetheless added a complete, correct, 4-way-mirrored "🔌 MCP Server" section. No further action.

---

## Requirement Traceability Update

| Requirement | Round 1 Status | Round 2 Status |
| --- | --- | --- |
| MCPUI-01 | Implemented, untested | ✅ Verified |
| MCPUI-02 | Implemented, untested | ❌ Needs Fix (test) — also the one confirmed-surviving mutant |
| MCPUI-03 | Implemented, untested | ✅ Verified |
| MCPUI-04 | Implemented, untested | ❌ Needs Fix (test) |
| MCPUI-05 | Implemented, untested | ✅ Verified |
| MCPUI-06 | Implemented, untested | ❌ Needs Fix (test) |
| MCPUI-07 | Implemented, untested | ✅ Verified |
| MCPUI-08 | Implemented, untested | ⚠️ Partially verified (negative clause untested) |
| MCPUI-09 | Implemented, untested | ❌ Needs Fix (test) |
| MCPUI-10 | Partially tested | ⚠️ Partially verified (unchanged) |
| MCPUI-11 | Test present, unexecuted | ✅ Verified |
| MCPUI-12 | Test present, unexecuted | ✅ Verified |
| MCPUI-13 | Needs Fix | ✅ Verified |
| MCPUI-14 | Needs Fix | ✅ Verified |
| MCPUI-15 | Verified (static) | ✅ Verified (static, unchanged) |

---

## Summary

**Overall**: FAIL ❌ — genuine, measurable progress (0/15 → 9/15 fully test-evidenced ACs, the copy-toast bug fixed, CHANGELOG + README parity closed) but 3 concrete gaps remain, one of which is a confirmed-surviving mutant (not just "no test exists" but "a real regression here would currently ship unnoticed").

**Spec-anchored check**: 9/15 ACs fully closed with spec-matched `file:line` assertions; 2/15 partial (MCPUI-08, -10); 4/15 still zero test evidence (MCPUI-02, -04, -06, -09)
**Gate**: 2/2 static commands passed (`tsc -b`, `npm run build`); live e2e suite pass/fail still unknown in this sandbox (2 independent attempts, both blocked by the same reproducible Docker networking failure)
**Sensor**: 3 mutations reasoned (live execution blocked) — 2/3 reasoned-killed, 1/3 reasoned-survived (`nav.ts:46` route path — no test currently catches a regression here)

**What works**: The copy-success toast bug is genuinely fixed at the code and locale level (`CopyButton`'s `label`/`successMessage` split, verified at all 3 call sites). CHANGELOG has a real, substantive `## [Unreleased]` entry. All 4 READMEs consistently say "Dashboard → MCP" (round 1's specific citation for this gap turned out not to correspond to any real prior content, but the fix round's addition is correct and complete regardless). 9 of 15 ACs now have precise, spec-matched test assertions, including both previously-flagged P3 gaps (empty-state, error-paths) fully closed with exact-text assertions.

**Issues found** (ranked):
1. **(Major, confirmed regression risk)** No test clicks the "MCP" nav link and verifies navigation to `/mcp-settings` — MCPUI-02, and the discrimination sensor confirms a route-path regression in `nav.ts` would currently go undetected.
2. **(Major, protects a shipped bug)** No test exercises either `CopyButton` (endpoint or per-client snippet) — MCPUI-06/-09 — meaning a regression of the exact toast bug this round fixed would not be caught.
3. **(Minor, coverage only)** MCPUI-10's last-used/never-used text, MCPUI-04's mobile-nav reachability, and MCPUI-08's "no interactive OAuth action" negative clause remain code-verified-only.
4. **(Process note, not a defect)** Round 1's README citation (gap 5) did not correspond to real file content at the commit it claimed to validate — recorded as a lesson about re-verifying a prior Verifier's specific citations rather than carrying them forward uncritically.
5. **(Environment, unresolved across 2 sessions now)** Live e2e execution remains blocked in this sandbox by a reproducible Docker host→container networking failure (`connection reset by peer`) — not a code defect, but it means no session has yet produced a real green/red run of this suite.

**Next steps**: Route Fix 1 and Fix 2 back to an implementer (this is fix→re-verify iteration 2 of 3; a round-3 pass focused narrowly on these 2 items, plus Fix 3's three minor items if time allows, should be sufficient to close out). Fix 3's items and the live-execution question can be deferred to round 3 or flagged to the user if the 3-iteration bound is reached first — the sensor's one confirmed-surviving mutant (Fix 1) is the one item that should not be waved through as "cosmetic."
