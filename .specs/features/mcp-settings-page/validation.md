# MCP Settings Page Validation

## Validation: MCP Settings Page - FAIL ❌

**Date**: 2026-08-15
**Spec**: `.specs/features/mcp-settings-page/spec.md`
**Diff range**: `28290f5^..9efdc83` (5 commits, develop)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

No `tasks.md` exists (Tasks phase skipped — Medium scope, per spec.md's own traceability note). Verified against spec.md's 15 requirement IDs (MCPUI-01..15) and inline "Independent Test" descriptions instead.

| Commit | Content | Status |
| --- | --- | --- |
| `28290f5` feat: add MCP nav item to sidebar | `nav.ts` | ✅ Present |
| `ca44ee7` feat: add MCP settings page | `MCPPage.tsx`, `App.tsx`, locales | ✅ Present |
| `7595428` refactor: remove PAT modal in favor of MCP page | `DashboardShell.tsx`, `SidebarFooter.tsx`, `MobileNav.tsx`, deletes `PersonalAccessTokens.tsx` | ✅ Present |
| `6ec4add` fix: avoid MCP page route colliding with backend endpoint | `App.tsx`, `nav.ts` (`/mcp` → `/mcp-settings`) | ✅ Present |
| `9efdc83` test: move PAT e2e coverage to MCP settings page | `e2e/personal-access-tokens.spec.ts` | ✅ Present |

---

## Spec-Anchored Acceptance Criteria

**Important caveat before the table**: this project has **no frontend unit-test framework** (no vitest; `package.json` scripts are only `dev`/`build`/`preview`/`test:e2e`, `internal/dashboard/ui/package.json:10-12`). The only test asset touching this feature is one Playwright e2e file, `e2e/personal-access-tokens.spec.ts`, and it exercises **only** the P3 (PAT CRUD) story. P1 (nav discovery) and P2 (client tutorials) have **zero** test files referencing them — confirmed via `grep -rln "mcp\|MCP" e2e/*.spec.ts` returning only `personal-access-tokens.spec.ts`. Per the evidence-or-zero rule, criteria with no test file:line are marked GAP even where direct code inspection shows the behavior is implemented correctly — that distinction is called out per row.

### P1: Discover and open MCP setup

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-01: nav item labeled `nav.mcp` under Deployment | Sidebar renders "MCP" (not raw key) alongside SDKs | `internal/dashboard/ui/src/components/layout/nav.ts:46` — `{ icon: 'key', labelKey: 'nav.mcp', path: '/mcp-settings' }`; `en.json:12`/`pt-BR.json:12` define `nav.mcp`. No test asserts this renders. | ❌ GAP (no test) — code correct on inspection |
| MCPUI-02: click navigates to `/mcp-settings` | Route renders `MCPPage` | `internal/dashboard/ui/src/App.tsx:138` — `<Route path="/mcp-settings" element={<MCPPage />} />`. No test clicks the nav item and asserts the URL. | ❌ GAP (no test) — code correct on inspection |
| MCPUI-03: old unlabeled key icon removed from `SidebarFooter`/`MobileNav` | Icon button gone from both | `internal/dashboard/ui/src/components/layout/SidebarFooter.tsx` diff removes `<IconBtn icon="key" ... onClick={onManagePATs} />` and the `onManagePATs` prop; `MobileNav.tsx` diff removes the same prop/passthrough. `grep -rn "onManagePATs\|showManagePATs\|PersonalAccessTokens\b"` across `src/` and `e2e/` returns only a stale code comment (`MCPPage.tsx:337`, prose, not a reference) — no dangling wiring. No test asserts the icon's absence. | ❌ GAP (no test) — code correct on inspection |
| MCPUI-04: mobile viewport has equivalent access | Mobile nav can reach `/mcp-settings` | `internal/dashboard/ui/src/components/layout/MobileNav.tsx:7` imports `NAV_SECTIONS` from the same `nav.ts` the desktop `Sidebar` uses, `MobileNav.tsx:94` maps over it — the new MCP entry is included automatically, no separate mobile nav config to update. No test exercises the mobile breakpoint. | ❌ GAP (no test) — code correct on inspection |

**Status**: ❌ Gaps present — 0/4 criteria have any test evidence; all 4 are implemented correctly per direct code/diff inspection.

### P2: Understand how to connect an MCP client

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-05: endpoint URL = `window.location.origin + '/dashboard/mcp'` | Exact computed value shown | `internal/dashboard/ui/src/pages/MCPPage.tsx:19-22` — `endpointUrl()` returns exactly that string; rendered at `MCPPage.tsx:121-123`. No test asserts the rendered text. | ❌ GAP (no test) — code correct on inspection |
| MCPUI-06: copy icon copies URL + success toast | Clipboard write + `toast.success` | `MCPPage.tsx:24-32` (`copyToClipboard`), wired at `MCPPage.tsx:124` via `<CopyButton value={endpoint} label={t("mcp.copyEndpoint")} />`. No test clicks it. | ❌ GAP (no test) — code correct on inspection |
| MCPUI-07: 4 client sections (Claude Code/Codex/Cursor/OpenCode), snippet matches README, `<host>` replaced, PAT kept as `${ZEEP_ORBIT_PAT}` | Snippet content byte-matches README's blocks modulo host substitution | `MCPPage.tsx:56-113` (`CLIENTS` array) — manually diffed against `README.md:391-441`; JSON/TOML shape is identical for all 4 clients, `<host>` → live `endpoint`, `${ZEEP_ORBIT_PAT}` preserved verbatim. No test asserts snippet content. | ❌ GAP (no test) — code correct on inspection (manual cross-check by Verifier) |
| MCPUI-08: explains PAT vs OAuth 2.1+PKCE, no interactive connect action | Explanatory copy only, no button that drives OAuth | `MCPPage.tsx:130-146` (`AuthExplainer`) — two static text blocks (`mcp.authPatDesc`, `mcp.authOAuthDesc`), no OAuth-triggering control anywhere in the file (`grep -n "authorize\|oauth" MCPPage.tsx` → only the `mcp.authOAuthDesc` prose string). No test asserts this. | ❌ GAP (no test) — code correct on inspection |
| MCPUI-09: copy icon on a client snippet copies that exact snippet + toast | Clipboard write of `snippet`, not endpoint | `MCPPage.tsx:184` — `<CopyButton value={snippet} label={t("mcp.copyConfig")} />` inside the per-client map. No test clicks it. | ❌ GAP (no test) — code correct on inspection |

**Status**: ❌ Gaps present — 0/5 criteria have any test evidence.

### P3: Manage personal access tokens from the same page

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-10: list non-revoked PATs, name + last-used/never-used | `pats?.filter(pat => !pat.revoked_at)` filter; per-row name + date-or-"never used" | Filter: `MCPPage.tsx:201` — `allPats?.filter((pat) => !pat.revoked_at)`. Name/date rendering: `MCPPage.tsx:296-301`. Test: `e2e/personal-access-tokens.spec.ts:59,65` asserts token name appears after create and after reload — **does not** assert the last-used/never-used text specifically. | ⚠️ Partial — filter+name path tested (unexecuted, see Gate), last-used/never-used text untested |
| MCPUI-11: create → reveal raw token once → never again after dismiss | `createdToken` state shown once, cleared on "Done", not persisted | State machine: `MCPPage.tsx:207,215,235-247`. Test: `e2e/personal-access-tokens.spec.ts:50-58` (create, assert reveal + warning text), `:61-66` (reload, assert `revealed-pat-token` has count 0). | ✅ Test present, spec-matched — see Gate Check for execution status |
| MCPUI-12: revoke shows destructive confirm, only revokes after confirm | `ConfirmDialog` gate before `revokePAT.mutate` | `MCPPage.tsx:222-226` (`confirmRevoke` only called from dialog `onConfirm`), `MCPPage.tsx:320-331` (`ConfirmDialog` wiring). Test: `e2e/personal-access-tokens.spec.ts:84-86` clicks row "Revoke" (opens dialog) then clicks confirm dialog's "Revoke" (`exact: true`, `.last()`), then asserts token gone. | ✅ Test present, spec-matched — see Gate Check for execution status |
| MCPUI-13: empty list → empty state, not blank | `EmptyState` render when `!pats?.length` | `MCPPage.tsx:286-287` — `!pats?.length ? <EmptyState icon="key" title=... description={t("pats.emptyDesc")} /> : ...`. No test ever exercises an empty list (the one e2e test always has at least the just-created token present, and never asserts the empty-state copy). | ❌ GAP (no test) — code correct on inspection |
| MCPUI-14: create/revoke failure → `toast.error(error.message)`, no optimistic removal, form stays open | On error, list/form state unchanged | `MCPPage.tsx:210-219` — `handleCreate`'s catch is empty with comment "useCreatePAT's onError already shows toast.error"; `useCreatePAT`/`useRevokePAT` themselves are out of scope (`src/lib/api.ts`, pre-existing, untouched by this diff) and were not re-verified here. No test simulates a failure (mocked 4xx/5xx) for either action. | ❌ GAP (no test); underlying error-toast wiring assumed unchanged from prior feature — not re-verified |
| MCPUI-15: every string (`pats.*`, `common.copyToClipboard`, `mcp.*`, `nav.mcp`) present in **both** `en.json` and `pt-BR.json` | No key falls back to literal display | Key-by-key static check (this Verifier): all 12 new `mcp.*` keys present in both `en.json:889-902` / `pt-BR.json:889-902`; `nav.mcp` in both at line 12; all `pats.*` keys referenced in `MCPPage.tsx` (`pats.title`, `pats.explainer`, `pats.createdWarning`, `pats.done`, `pats.nameLabel`, `pats.namePlaceholder`, `pats.cancel`, `pats.creating`, `pats.create`, `pats.newToken`, `pats.empty`, `pats.emptyDesc`, `pats.lastUsed`, `pats.neverUsed`, `pats.revoke`, `pats.revokeTitle`, `pats.revokeConfirm`) pre-exist in both locale files unchanged by this diff; `common.copyToClipboard` present both at line 862. One unused key found: `mcp.copySuccess` is defined in both locales but never referenced in `MCPPage.tsx` (dead key, not a visible-raw-key bug). | ✅ PASS (static/deterministic check, not a runtime test) |

**Status**: ⚠️ Gaps present — 2/6 criteria (MCPUI-11, -12) have matching test code, but the sandbox could not execute it (see Gate Check); 2/6 (MCPUI-13, -14) have no test at all; MCPUI-10 partially tested; MCPUI-15 verified by direct static file inspection.

---

## Edge Cases

- [x] `usePATs` loading → `LoadingState` instead of blank flash — `MCPPage.tsx:284-285` (`isLoading ? <LoadingState rows={2} /> : ...`). Code-verified, not test-covered.
- [x] Clipboard API unavailable → value stays visible, no silent failure of the *display* — `MCPPage.tsx:24-32` catch swallows only the copy action; the token/snippet was already rendered in the DOM independent of clipboard success. Code-verified, not test-covered.
- [x] Empty/whitespace token name → create disabled — `MCPPage.tsx:271` `disabled={!name.trim() || createPAT.isPending}`. Code-verified, not test-covered.
- [x] Direct navigation to `/mcp-settings` via URL → same page, no modal-open props — `App.tsx:138` mounts `<MCPPage />` with zero props; `e2e/personal-access-tokens.spec.ts:39` `beforeEach` navigates directly via `page.goto('/dashboard/mcp-settings')`, exercising exactly this path (test present, execution blocked — see Gate Check).

---

## Discrimination Sensor

**Attempted live e2e run first** (per task instructions, since this project has no unit-test framework and the only behavior coverage is the e2e file): spun up a throwaway `postgres:16-alpine` container (`zorbit-verify-pg`, port 15499), built `./cmd/zeep` cleanly, and tried to start the server against it 3 times (env var `DATABASE_URL`, then explicit `127.0.0.1`, then `--db` flag). All 3 attempts failed at the DB layer — `error: db: ping failed: context deadline exceeded` and, on the third attempt, `read: connection reset by peer` — despite `docker exec ... pg_isready` and a direct `psql` query against the same container succeeding from the host. This matches the author's own documented finding in spec.md's Success Criteria ("testcontainers `ryuk` reaper... intermittent request hangs... unrelated to this feature's code") — the sandbox's Docker networking is unreliable for this kind of ad-hoc container regardless of which side initiates it. Cleaned up (`docker rm -f zorbit-verify-pg`) and did not spend further time chasing it, per the 1-2-honest-attempts guidance.

**Fell back to static fault-injection** in a scratch git worktree (never the real tree):

1. `git worktree add <scratch> 9efdc83` (detached HEAD, isolated from the real working tree, which had only pre-existing unrelated README diffs — captured as baseline via `git status --porcelain`).
2. Injected 3 mutations into the scratch copy:

| # | File:line | Mutation | Killed? |
| --- | --- | --- | --- |
| 1 | `MCPPage.tsx:201` | `!pat.revoked_at` → `pat.revoked_at` (PAT list would show only revoked tokens, hiding all active ones) | ⚠️ Unable to execute — no runnable test harness reaches this line (e2e blocked, no unit tests) |
| 2 | `nav.ts:46` | `path: '/mcp-settings'` → `path: '/mcp'` (reintroduces the exact route collision with the backend's `/dashboard/mcp` transport handler that commit `6ec4add` fixed) | ⚠️ Unable to execute — same blocker |
| 3 | `MCPPage.tsx:21` | `` `${base}/dashboard/mcp` `` → `` `${base}/dashboard/mcp-x` `` (wrong endpoint URL shown to user, and copied snippets would point at a dead path) | ⚠️ Unable to execute — same blocker |

3. Confirmed none of the 3 mutations produce a TypeScript error (`npx tsc -b` in the scratch worktree still compiles the mutated files — these are runtime/string-value bugs, not type errors, so the build gate structurally cannot catch this class of regression either).
4. Removed the scratch worktree (`git worktree remove --force`) and confirmed `git status --porcelain` on the real tree is byte-identical to the pre-sensor baseline — isolation held.

**Sensor depth**: lightweight (3 targeted mutations attempted, per default tiering)
**Result**: 0/3 executed, 0/3 confirmed killed — FAIL. There is currently no automated mechanism in this repository that would catch a regression in the PAT filter, the route path, or the endpoint URL string. This is a real gap, not an environment artifact to wave away: even outside this sandbox, on a machine with working Docker, none of the 3 targeted lines are covered by the existing `personal-access-tokens.spec.ts` (it never asserts the endpoint URL text, never asserts the route differs from `/mcp`, and — while it does read the PAT list — a filter that shows revoked-only tokens would still pass the existing assertions as written, since the test only checks that the freshly-created token's name is present, not that revoked ones are absent).

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ — mirrors `Webhooks.tsx` copy pattern, `ConfirmDialog` usage, `PageHeader`/`EmptyState`/`LoadingState` patterns as the spec's Assumptions table intended |
| Tests map to acceptance criteria and are non-shallow (spot-check one story) | ⚠️ P3's test is non-shallow where it exists (creates a real token, authenticates a real HTTP request to `/dashboard/mcp`, revokes, re-checks 401) — but P1/P2 have no tests at all, and P3 itself skips empty-state and error-path criteria |
| Spec-anchored outcome check: each test's asserted value matches the spec-defined outcome (or gap flagged) | ⚠️ Where tests exist (MCPUI-11, -12) the assertions match the spec-defined outcome; flagged above where they don't (MCPUI-10 partial, -13/-14 absent) |
| Per-layer Coverage Expectation met: domain logic has 1:1 AC mapping; routes/e2e cover happy + edge + error paths for every route in scope | ❌ — the one route in scope (`/mcp-settings`) has only a happy-path PAT test; no error-path test (create/revoke failure), no empty-state test, no coverage at all for the nav/discovery or client-tutorial routes' behavior |
| Every test in scope maps to a spec AC, listed edge case, or Done-when criterion (no unclaimed tests) | ✅ — the single e2e file's 5 stages all map cleanly to MCPUI-10/11/12 and the "direct navigation" edge case |
| Documented project quality/testing guidelines followed (cite guideline file, or "none - strong defaults applied") | ❌ — `AGENTS.md` §6 requires a `CHANGELOG.md` entry under `## [Unreleased]` "in the same change that ships the fix/feature. Don't defer this to release day." `git diff 28290f5^..9efdc83 -- CHANGELOG.md` is empty — no entry was added across any of the 5 commits. |

---

## Interactive UAT Results

Not performed — the Verifier was scoped to automated/static validation for this pass (frontend build gate + sensor + AC/code-quality checks); no user was available in this session to walk through the flows. This should be run before the feature's status moves past "Implementing" given how much of the AC table above rests on code inspection rather than test evidence.

---

## Gate Check

- **Gate command**: `cd internal/dashboard/ui && npx tsc -b && npm run build`
- **Result**: both commands exited 0. `tsc -b` produced no output (clean). `vite build` succeeded — 489 modules transformed, standard pre-existing "chunk larger than 500kB" advisory warning only (unrelated to this feature, present before it too).
- **Test count before feature**: 10 e2e spec files (`app-members`, `app-users`, `apps`, `auth`, `data-browser`, `enduser-roles`, `personal-access-tokens` [pre-existing, modal-based], `policy-templates`, `users`, `webhooks`)
- **Test count after feature**: 10 e2e spec files (`personal-access-tokens.spec.ts` modified in place, not added/removed)
- **Delta**: 0 new spec files; the existing PAT spec was retargeted from the modal to `/mcp-settings` (same 1 test, same 5 stages, updated navigation and comments) — no new assertions added for the new P1/P2 surface (nav item, endpoint display, client tutorials, auth explainer)
- **Skipped tests**: none skipped in the suite definition; however the entire e2e suite could not be **executed** in this sandbox (Docker/Postgres networking instability — see Discrimination Sensor), so "10 passed" cannot be claimed for this session. Only the static build gate (tsc/vite) ran to completion.
- **Failures**: none observed in the commands that could run (tsc, vite build); the e2e suite's pass/fail status is genuinely unknown in this environment.

---

## Fix Plans

### Fix 1: No automated coverage for P1 (nav discovery) or P2 (client tutorials)

- **Root cause**: the implementation added the nav item, route, endpoint display, and 4 client snippets, but the only test file touched (`personal-access-tokens.spec.ts`) covers exclusively the pre-existing PAT flow it was migrating. No new test asserts the nav item renders as "MCP", that clicking it navigates to `/mcp-settings`, that the old key icon is gone, that the endpoint URL/copy buttons work, or that each of the 4 client snippets renders with the substituted host.
- **Fix task**: Add e2e coverage (new spec file or extend `personal-access-tokens.spec.ts`) asserting: (a) sidebar shows a "MCP" link and no unlabeled key-icon button exists in the footer; (b) clicking it lands on `/mcp-settings`; (c) the endpoint code block shows `<origin>/dashboard/mcp`; (d) each of the 4 client tabs/sections is present and its rendered snippet text contains the live endpoint URL (not `<host>`) and `${ZEEP_ORBIT_PAT}`.
- **Priority**: Major

### Fix 2: P3 empty-state and error-path criteria (MCPUI-13, -14) untested

- **Root cause**: the existing e2e test's happy path always has a token in the list; it never drives the list to zero and asserts `EmptyState` text, and never forces a create/revoke API failure to assert `toast.error` + no optimistic UI change.
- **Fix task**: Add a step (or a second test) that revokes down to zero tokens and asserts the empty-state copy renders; add a mocked-failure test (Playwright route interception on the PAT create/revoke endpoint) asserting the form stays open / the token isn't removed from the list, and that an error toast appears.
- **Priority**: Minor (behavior is implemented correctly per code inspection; this is a coverage gap, not a known defect)

### Fix 3: CHANGELOG.md not updated

- **Root cause**: `AGENTS.md` §6 mandates a `## [Unreleased]` entry in the same change; none of the 5 commits touched `CHANGELOG.md`.
- **Fix task**: Add a `### Changed` (or `### Fixed`, depending on how the team wants to frame "moved PAT management + added MCP discoverability") entry describing the sidebar MCP entry, the `/mcp-settings` page, and the PAT modal removal.
- **Priority**: Minor (process/documentation gap, not a functional defect)

### Fix 4 (informational, not blocking): stale README location text

- **Observation**: `README.md:383` and `:389` still say "generate one in **Dashboard → Settings → Personal Access Tokens**" — that path no longer exists; PAT management now lives at the new "MCP" nav entry (`/mcp-settings`). This feature didn't touch `README.md`, so it wasn't flagged as a hard requirement violation, but the prose is now inaccurate.
- **Fix task**: Update the two README mentions (and the 3 translated READMEs, per `AGENTS.md` §6's translation-parity rule, if they contain the same sentence) to point at the new location.
- **Priority**: Cosmetic

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| MCPUI-01 | Implementing | ⚠️ Implemented, untested |
| MCPUI-02 | Implementing | ⚠️ Implemented, untested |
| MCPUI-03 | Implementing | ⚠️ Implemented, untested |
| MCPUI-04 | Implementing | ⚠️ Implemented, untested |
| MCPUI-05 | Implementing | ⚠️ Implemented, untested |
| MCPUI-06 | Implementing | ⚠️ Implemented, untested |
| MCPUI-07 | Implementing | ⚠️ Implemented, untested |
| MCPUI-08 | Implementing | ⚠️ Implemented, untested |
| MCPUI-09 | Implementing | ⚠️ Implemented, untested |
| MCPUI-10 | Implementing | ⚠️ Partially tested (test present, unexecuted this session) |
| MCPUI-11 | Implementing | ⚠️ Test present, spec-matched, unexecuted this session |
| MCPUI-12 | Implementing | ⚠️ Test present, spec-matched, unexecuted this session |
| MCPUI-13 | Implementing | ❌ Needs Fix (test) |
| MCPUI-14 | Implementing | ❌ Needs Fix (test) |
| MCPUI-15 | Implementing | ✅ Verified (static check) |

---

## Summary

**Overall**: FAIL ❌ (against this skill's evidence-or-zero and mandatory-sensor bar — not a functional failure: direct code inspection shows every AC's behavior is implemented as specified, but 12/15 ACs have no test evidence and the sensor could not confirm the tests that do exist)

**Spec-anchored check**: 1/15 ACs (MCPUI-15) independently confirmed via static evidence this session; 2/15 (MCPUI-11, -12) have spec-matched test code that could not be executed; the remaining 12/15 have no test evidence at all (implementation verified correct by direct code/diff inspection only)
**Sensor**: 0/3 mutations executed/confirmed killed — blocked by sandbox Docker/network instability (matches the author's own documented finding), not by a passing test suite
**Gate**: 2/2 static commands passed (`tsc -b`, `npm run build`); the actual e2e suite's pass/fail is unknown in this session

**What works**: The route-collision reasoning (`/mcp-settings` vs `/mcp`) is sound and independently verified against `internal/server/server.go:171-198` and `src/main.tsx:28` — `/dashboard/mcp` is registered as a sibling route outside the `/dashboard` SPA `chi.Router` group, so `/mcp` would indeed have collided. The old PAT modal is cleanly removed with no dangling references (`onManagePATs`/`showManagePATs`/component import all gone, verified by repo-wide grep). All new `mcp.*`/`nav.mcp` i18n keys exist in both `en.json` and `pt-BR.json`, key-for-key. The 4 client config snippets exactly match `README.md`'s existing content. The build gate is clean.

**Issues found**:
1. Zero automated test coverage for P1 (discovery) and P2 (client tutorials) — Fix 1 above.
2. P3's empty-state and error-handling criteria are untested — Fix 2 above.
3. `CHANGELOG.md` wasn't updated per `AGENTS.md` §6 — Fix 3 above.
4. The discrimination sensor could not confirm the existing tests (where they exist) actually catch regressions, because the sandbox cannot run the live e2e suite — this is an environment limitation, not a code defect, but it means this validation pass cannot claim the tests are proven to discriminate.
5. (Cosmetic) README's PAT location prose is stale — Fix 4 above.

**Next steps**: Route Fixes 1-3 back to an implementer (Fix 4 optional/cosmetic). Re-verify by (a) running the strengthened e2e suite against a Docker environment outside this sandbox's apparent instability, and (b) re-running the discrimination sensor's 3 mutations against that suite to confirm they're actually killed before moving any MCPUI-* status past "Implementing."
