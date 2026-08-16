# MCP Settings Page Validation

## Validation: MCP Settings Page — Round 3 (final iteration) — FAIL ❌

**Date**: 2026-08-15
**Spec**: `.specs/features/mcp-settings-page/spec.md`
**Diff range (full feature)**: `28290f5^..879c199` (11 commits, `develop`)
**Fix-round commits reviewed this round**: `e931f3a` (test fix), `879c199` (docs, round-2 report)
**Verifier**: independent sub-agent (author ≠ verifier), round 3 of a max-3 fix→re-verify bound — **this is the final allowed iteration; any remaining gap must be escalated to the human, not looped again.**

---

## Headline finding (read this first)

For the first time across all three Verifier rounds, **live e2e execution against a real Postgres-backed server actually worked in this sandbox** (see Discrimination Sensor section for how). This let round 3 replace static/reasoned coverage claims with a real, repeatable, deterministic result — and it overturns round 2's belief that MCPUI-06/-09 were closed.

**The current, unpatched, merged test suite fails one test every time it runs, and that failure happens *before* the very assertions round 2 added to close MCPUI-06/-09 — so those assertions have never actually executed, in CI or anywhere else, since they were written.**

Root cause: `e2e/personal-access-tokens.spec.ts:165` (introduced in `eb0db4f`, round 1's fix, unchanged through `e931f3a`, round 2's fix) asserts `expect(snippet).toContain('${ZEEP_ORBIT_PAT}')` inside a loop over all 4 clients. This is correct for Claude Code, Cursor, and OpenCode (their snippets are JSON with `"Authorization": "Bearer ${ZEEP_ORBIT_PAT}"`), but **wrong for Codex**, whose snippet is TOML and correctly has no `${}` wrapper at all — `bearer_token_env_var = "ZEEP_ORBIT_PAT"` (`MCPPage.tsx:87`). This exactly matches `README.md:406-411`'s own Codex block, which is the spec's declared single source of truth for these snippets (spec.md's Assumptions table: "Reuse the four snippets already drafted in README.md"). **The product code is correct. The test's blanket per-client assertion is the bug**, and it has been there since round 1, invisible because no prior round achieved live execution.

Consequence: `e2e/personal-access-tokens.spec.ts` test 2 (`renders MCP discovery and per-client connection tutorials`) throws at line 165 on the Codex iteration, before ever reaching lines 173-177 — the exact copy-button-click + toast assertions round 2 added to close MCPUI-06/-09. Confirmed twice, deterministically (not a flake): both live runs failed at the identical line with the identical message.

This is a **new gap for round 3**, invisible to rounds 1-2 because they never got live execution to work. It does not undo round 2's real progress (the click-through nav test and the toast-assertion code are both correctly written and the discrimination sensor confirms they'd catch a regression *if reached*) — but as merged today, MCPUI-06/-09 are not actually verified by a passing test; they're dead code downstream of an unrelated, pre-existing test bug.

---

## Task Completion

No `tasks.md` (Tasks phase skipped, Medium scope, per spec.md's traceability note). Execute proceeded via an inline task list across `28290f5`→`879c199`. All implementation + 3 fix-round commits are present and match their stated content (see commit audit below).

| Commit | Content | Verified |
| --- | --- | --- |
| `28290f5`..`ca44ee7` | Nav item + MCP page (initial feature) | ✅ |
| `7595428`, `6ec4add` | PAT modal removal, route-collision fix | ✅ |
| `9efdc83` | Round-1 e2e test (moved PAT coverage to MCP page) | ✅ |
| `aecece1` | Copy-toast bug fix (`label`/`successMessage` split) | ✅ Confirmed again this round, still correct |
| `c8765a6` | CHANGELOG + README (+ 3 translations) | ✅ |
| `eb0db4f` | Round-1 fix: MCP discovery/tutorials test, empty-state assertion | ⚠️ **Introduced the `${ZEEP_ORBIT_PAT}` loop bug that survives to this round** |
| `cdce3e5` | Round-1 validation report + lessons | ✅ Present |
| `e931f3a` | Round-2 fix: nav click-through test, copy-toast click test, mobile-nav test, MCPUI-08 negative clause, MCPUI-10 last-used text | ✅ Test code is correct in isolation, but appended after the pre-existing broken loop in the same test function (see headline finding) |
| `879c199` | Round-2 validation report + lessons | ✅ Present |

---

## Spec-Anchored Acceptance Criteria (re-derived from scratch, round 3)

### P1: Discover and open MCP setup

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-01: nav item labeled `nav.mcp` | Sidebar renders "MCP" text | `e2e/personal-access-tokens.spec.ts:140` — `expect(page.getByRole('link', { name: 'MCP' })).toBeVisible()` | ✅ PASS — confirmed live (test 2 passes up to this line every run) |
| MCPUI-02: click navigates to `/mcp-settings` | Route renders `MCPPage` after a nav click | `spec.ts:184-188` — `page.goto('/dashboard/apps')` then `.getByRole('link',{name:'MCP'}).click()`, `expect(page).toHaveURL(/\/dashboard\/mcp-settings$/)`, heading assertion | ✅ PASS — **live-executed, passed in both live runs**; discrimination sensor confirms a route-path regression (`nav.ts` `/mcp-settings`→`/mcp`) is killed by this exact test (see Sensor) |
| MCPUI-03: old key-icon button gone | No button with that identity remains | `spec.ts:141` — `expect(page.getByRole('button', { name: 'Personal Access Tokens' })).toHaveCount(0)` | ✅ PASS — live-executed, passes |
| MCPUI-04: mobile nav equivalent access | `/mcp-settings` reachable from mobile nav | `spec.ts:190-197` — sets 390×844 viewport, opens "More" sheet, asserts `page.getByTestId('mobile-nav-sheet').getByRole('link',{name:'MCP'})` visible | ✅ PASS — live-executed, passes. Testid scoping avoids any strict-mode ambiguity with the desktop sidebar's link (which in any case has `display:none` via `max-md:hidden` at this viewport and so is absent from the accessibility tree Playwright's `getByRole` walks — verified in `Sidebar.tsx:53`) |

**Status**: ✅ 4/4 closed, all now live-executed (up from 2/4 static-only in round 2).

### P2: Understand how to connect an MCP client

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-05: endpoint URL = `origin + '/dashboard/mcp'` | Exact computed value shown | `spec.ts:145-146` — `expect(endpoint).toBe(`${origin}/dashboard/mcp`)` | ✅ PASS — live-executed, passes |
| MCPUI-06: copy icon on endpoint → clipboard + success toast | Clipboard write + `toast.success` | `spec.ts:173-174` — `page.getByTitle('Copy endpoint URL').click()`, `expect(page.getByText('Copied to clipboard')).toBeVisible()` | ❌ **GAP — assertion code is correct but UNREACHED.** The same test throws at line 165 (Codex snippet, see Headline Finding) before ever executing line 173. Confirmed by 2 identical live failures. Isolated in a scratch worktree (bypassing the line-165 defect only) this exact assertion correctly catches a reintroduced toast bug — but that isolation was necessary precisely because the real, merged test never reaches it |
| MCPUI-07: 4 client sections, snippet = README content with host substituted, PAT var kept | Snippet text matches spec | `spec.ts:160-167` loop, all 4 clients | ⚠️ **Spec-precision gap, test defect** — 3/4 clients (Claude Code, Cursor, OpenCode) pass; Codex fails deterministically because the test's blanket `toContain('${ZEEP_ORBIT_PAT}')` doesn't hold for TOML's `bearer_token_env_var = "ZEEP_ORBIT_PAT"` syntax (no `${}`). The **implementation itself is correct** — verified byte-for-byte equivalent to `README.md:406-411`'s Codex block, which is the spec's stated source of truth. This is a bug in the test, not the product, but it fails the live suite every run |
| MCPUI-08: explains PAT vs OAuth 2.1+PKCE, no interactive connect action | Explanatory text present; no OAuth-driving control | `spec.ts:152-155` — both explainer strings + `expect(page.getByRole('button',{name:/connect/i})).toHaveCount(0)` + same for link | ✅ PASS — live-executed, passes (these lines run before the line-165 crash) |
| MCPUI-09: copy icon on client snippet → clipboard + toast | Clipboard write of exact snippet + toast | `spec.ts:176-177` — clicks Claude Code card's `CopyButton`, asserts toast | ❌ **GAP — same as MCPUI-06: correct assertion, unreached** (lines 176-177 are after the line-165 crash point) |

**Status**: ⚠️ 3/5 fully closed and live-passing (MCPUI-05, -07's non-Codex part, -08), **2/5 gap** (MCPUI-06, -09 — written correctly but dead code in the current test), **1/5 new spec-precision/test-defect gap** (MCPUI-07's Codex case).

### P3: Manage personal access tokens from the same page

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| MCPUI-10: list non-revoked PATs, name + last-used/never-used | Filter `!pat.revoked_at`; name + date-or-"never used" | Filter: `MCPPage.tsx:215`. `spec.ts:89` — `expect(page.getByText('Never used')).toBeVisible()` (fresh token); `spec.ts:108` — `expect(page.getByText(/^Last used /)).toBeVisible()` after an authenticated request bumps `last_used_at` | ✅ PASS — live-executed, passes; closes round 2's last remaining partial |
| MCPUI-11: create → reveal once → never again | `createdToken` shown once, cleared, never re-shown | `spec.ts:69-85` | ✅ PASS — live-executed, passes |
| MCPUI-12: revoke requires confirm dialog | Confirm gate before mutation | `spec.ts:112-113`, `235-236` | ✅ PASS — live-executed, passes |
| MCPUI-13: empty list → `EmptyState` | Render `EmptyState`, not blank | `spec.ts:132` — `expect(page.getByText('No personal access tokens')).toBeVisible()` | ✅ PASS — live-executed, passes |
| MCPUI-14: create/revoke failure → `toast.error(error.message)`, no optimistic removal, form stays open | Exact error text shown; state unchanged | `spec.ts:215-217` (create), `spec.ts:240-241` (revoke) — mocked 500 `{error:'internal error'}`, asserts `'internal error'` text + no reveal / still listed + name input retains value | ✅ PASS — live-executed, both mocked-failure tests pass |
| MCPUI-15: all strings in both locales | No raw key rendered | Static check (this session): all 31 `mcp.*`/`pats.*` keys present in both `en.json` and `pt-BR.json` with matching key sets (`python3 -c "import json; ..."` diff = empty set both directions) | ✅ PASS (static, unchanged) |

**Status**: ✅ 6/6 closed, all live-executed except MCPUI-15 (static-only, appropriately — it's an i18n completeness check, not a runtime behavior).

### Aggregate

- **11/15 ACs fully closed with live-executed, spec-matched test evidence** (MCPUI-01, -02, -03, -04, -05, -08, -10, -11, -12, -13, -14).
- **1/15 static-only, appropriately so** (MCPUI-15 — i18n key parity, not a runtime assertion).
- **1/15 spec-precision/test-defect gap** (MCPUI-07 — implementation correct, test assertion wrong for 1 of 4 clients).
- **2/15 gap — written but unreached** (MCPUI-06, -09 — correct code, blocked by MCPUI-07's defect in the same test).

This is real forward progress from round 2's 9/15 (now 11/15 solidly closed, and for the first time *proven* by actual execution rather than static reading) — but round 3 also **discovered a new, concrete defect that both prior rounds missed**: the test suite as merged does not currently pass, and two of the ACs round 2 believed it closed do not actually execute.

---

## Edge Cases

- [x] `usePATs` loading → `LoadingState` — code-verified (`MCPPage.tsx:297-298`), not directly test-covered (unchanged from round 2; not a new gap, pre-existing minor coverage note).
- [x] Clipboard API unavailable → value stays visible — code-verified (`MCPPage.tsx:24-32` catch), not test-covered (unchanged).
- [x] Empty/whitespace token name → create disabled — code-verified (`MCPPage.tsx:284` `disabled={!name.trim()...}`), not test-covered (unchanged).
- [x] Direct navigation to `/mcp-settings` via URL → same page — exercised by every test's `beforeEach` (`spec.ts:58`) and now confirmed live (all `beforeEach` navigations succeeded across every live run this session).

---

## Discrimination Sensor

**This round achieved live e2e execution — a first across all 3 Verifier rounds.**

### How it was unblocked

Prior rounds hit `connection reset by peer` connecting from the host to a Docker-networked Postgres. This round used the identical pattern (fresh `postgres:16-alpine`, host-mapped port) and it worked cleanly on the first attempt — internal `pg_isready`/`psql` checks succeeded, and so did the Go binary's connection from the host. No configuration change was made versus prior attempts; this appears to be sandbox-networking variance across sessions rather than a fixed root cause. Recorded as a lesson: retry the live-execution attempt every round rather than assuming a prior round's failure is permanent.

### Real suite run (unmutated code, twice, for determinism)

```
BOOTSTRAP_SECRET=test-secret npx playwright test e2e/personal-access-tokens.spec.ts --reporter=list
```
Run against `go build ./cmd/zeep` (this exact commit, `879c199`) + fresh `postgres:16-alpine`, `DASHBOARD_BOOTSTRAP_SECRET=test-secret`.

**Result (both runs, identical)**: 4 passed, 1 failed — test 2 (`renders MCP discovery and per-client connection tutorials`) fails at `spec.ts:165` on the Codex snippet (see Headline Finding). Tests 1, 3, 4, 5 pass.

### Mutation sensor (executed live in a scratch git worktree, not reasoned)

Baseline: `git status --porcelain` on the real tree was empty before sensor work and confirmed empty after `git worktree remove --force` (see cleanup log below).

| # | File:line | Mutation | Method | Result |
| --- | --- | --- | --- | --- |
| 1 | `nav.ts:46` | `path: '/mcp-settings'` → `path: '/mcp'` (reintroduces the exact route collision `6ec4add` fixed) | Scratch worktree, rebuilt frontend (`npm run build`) + Go binary, ran against a fresh throwaway Postgres/server on a new port, ran the **real, unpatched** test file | ✅ **Killed live** — `MCP nav link navigates...` test fails: `expect(page).toHaveURL(...)` times out, URL stays `/dashboard/apps` (click did not navigate to the mutated `/mcp` path under the SPA router) |
| 2 | `MCPPage.tsx:27` | `toast.success(successMessage)` → `toast.success('done')` (reintroduces a wrong-toast-text class of bug, proxy for the `aecece1` regression) | Same method. Because test 2 crashes at line 165 before reaching the toast-click assertions, this mutation was verified by additionally patching the **scratch copy only** of line 165's assertion (`toContain('${ZEEP_ORBIT_PAT}')` → `toContain('ZEEP_ORBIT_PAT')`) to unblock reachability, then rebuilding/rerunning | ✅ **Killed live** (once reachable) — `expect(page.getByText('Copied to clipboard')).toBeVisible()` fails, times out. **Important caveat**: this confirms the assertion *would* catch a regression *if it ran* — it does NOT contradict the Headline Finding that in the real, unpatched suite this code never runs today |
| 3 | `MCPPage.tsx:215` | `!pat.revoked_at` → `pat.revoked_at` (list shows only revoked tokens, hides active ones) | Same method, real unpatched test, targeted single test via `-g "create, reload"` | ✅ **Killed live** — `expect(page.getByText(tokenName)).toBeVisible()` fails after creating a token (fresh token has `revoked_at = null`, excluded by the mutated filter) |

**Sensor depth**: lightweight, 3 targeted mutations, **all 3 executed live** (not reasoned) for the first time in this feature's validation history.
**Result**: 3/3 killed. (Mutation 2 required an unrelated one-line patch in the scratch copy only to become reachable — the real, unmutated suite currently can't reach that code path at all, which is the round's core finding, tracked separately as a gap above, not as a sensor failure.)

### Cleanup verification

```
git worktree remove --force /tmp/zorbit-verify-scratch3
rm -f /tmp/zeep-verify3 /tmp/zeep-verify-mut /tmp/zeep-verify-mut2 /tmp/zeep-verify-mut4
docker rm -f zorbit-verify-pg3 zorbit-verify-pg-mut zorbit-verify-pg-mut2 zorbit-verify-pg-mut3 zorbit-verify-pg-mut4
```
Real tree `git status --porcelain` confirmed empty before sensor work and empty after cleanup. `git worktree list` shows only the real tree. `docker ps` shows only the repo's own pre-existing `zeep-orbit-db-1` (untouched, not used by this session).

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ — `e931f3a` touches only the spec file + 1 testid attribute; `879c199` touches only the validation/lessons docs |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ |
| Tests map to acceptance criteria and are non-shallow (spot-check one story) | ⚠️ — spot-checked P2: the per-client loop (MCPUI-07) is well-intentioned but applies one assertion shape to 4 structurally different config formats without accounting for TOML's different PAT-reference syntax; this is exactly the kind of over-generalized assertion the "non-shallow" check exists to catch, and it slipped through 2 prior rounds because neither executed it |
| Spec-anchored outcome check: each test's asserted value matches the spec-defined outcome (or gap flagged) | ❌ — flagged above: MCPUI-07's Codex case doesn't match spec-defined outcome as literally asserted (though the underlying implementation does match the spec's true intent) |
| Per-layer Coverage Expectation met: domain logic has 1:1 AC mapping; routes/e2e cover happy + edge + error paths for every route in scope | ⚠️ — happy/error paths for the PAT route are fully covered and live-passing; the one MCP-page-content route has a live-reachable defect blocking 2 of its ACs |
| Every test in scope maps to a spec AC, listed edge case, or Done-when criterion (no unclaimed tests) | ✅ — every assertion traces to a named MCPUI-* in code comments |
| Documented project quality/testing guidelines followed (cite guideline file, or "none - strong defaults applied") | ✅ — `AGENTS.md` §6 CHANGELOG + translation-parity rules satisfied, confirmed again this round |

---

## Gate Check

- **Gate command** (per tasks/spec, mandatory): `cd internal/dashboard/ui && npx tsc -b && npm run build`
- **Result**: both exited 0 this session. `tsc -b`: clean, no output. `vite build`: 489 modules transformed, 0 errors, same pre-existing chunk-size advisory (unrelated to this feature).
- **Supplementary evidence (not the mandatory gate, but the first real execution ever obtained for this feature)**: `npx playwright test e2e/personal-access-tokens.spec.ts` against a real server + fresh Postgres — **4 passed, 1 failed**, reproduced identically across 2 independent runs. See Discrimination Sensor for full detail and root cause.
- **Test count before this round**: 4 tests (round 2's count) — but actually 5 as re-examined this round (round 2's count of "4 tests" undercounted; the file has 5 `test(...)` blocks as of `e931f3a`: lifecycle, MCP discovery, nav click-through, create-failure, revoke-failure. `e931f3a`'s commit message describes adding assertions to the discovery test plus a *new* nav click-through test — consistent with 4→5).
- **Test count after this round**: unchanged, 5 (no test files added/removed by the Verifier; this round is read-only over test code except for the throwaway scratch-worktree patch used only to unblock the sensor, discarded with the worktree).
- **Skipped tests**: none.
- **Failures**: `tsc -b`/`vite build` — none. Live e2e — 1/5 (test 2, `spec.ts:165`, root-caused above).

---

## Fix Plans (routed for human decision — 3-round bound reached)

### Fix 1 (new this round, highest priority): MCPUI-07's Codex assertion is wrong, and it blocks MCPUI-06/-09 from ever running

- **Root cause**: `e2e/personal-access-tokens.spec.ts:165` applies `expect(snippet).toContain('${ZEEP_ORBIT_PAT}')` uniformly to all 4 clients inside the `for` loop at `spec.ts:160-167`. Codex's TOML snippet (`MCPPage.tsx:85-87`) correctly has no `${}` wrapper — it's `bearer_token_env_var = "ZEEP_ORBIT_PAT"`, matching `README.md:406-411` exactly.
- **Fix task**: change the loop assertion to something format-agnostic, e.g. `expect(snippet).toContain('ZEEP_ORBIT_PAT')` (drop the `${}`), which is true for all 4 clients (the JSON clients' literal snippets contain `${ZEEP_ORBIT_PAT}`, which itself contains the substring `ZEEP_ORBIT_PAT`). This one-line change would let the test reach lines 173-177 and, per the sensor, those lines already correctly guard MCPUI-06/-09.
- **Priority**: Blocker — this is the one item standing between "11/15 live-verified" and "13/15 live-verified," and it's a one-line fix with a already-confirmed-correct downstream assertion waiting behind it.
- **Note for whoever picks this up**: do not weaken this to a vaguer check like "snippet is non-empty" — the point of MCPUI-07 is to catch exactly this kind of format-specific regression; a substring check without the `${}` still discriminates a client with the wrong/missing env var name.

### Fix 2 (carried recommendation, not new): none of round 2's remaining minor items resurfaced as new problems

Round 2's Fix 3 items (MCPUI-10 text, MCPUI-04 mobile, MCPUI-08 negative clause) are all now confirmed ✅ PASS and live-executed this round — no further action needed on those three.

---

## Requirement Traceability Update

| Requirement | Round 2 Status | Round 3 Status |
| --- | --- | --- |
| MCPUI-01 | ✅ Verified | ✅ Verified (now live-executed) |
| MCPUI-02 | ❌ Needs Fix | ✅ Verified (live-executed + sensor-confirmed) |
| MCPUI-03 | ✅ Verified | ✅ Verified (live-executed) |
| MCPUI-04 | ❌ Needs Fix | ✅ Verified (live-executed) |
| MCPUI-05 | ✅ Verified | ✅ Verified (live-executed) |
| MCPUI-06 | ❌ Needs Fix | ❌ **Needs Fix — code correct but unreached; blocked by MCPUI-07's test defect** |
| MCPUI-07 | ✅ Verified | ⚠️ **New gap this round — test defect on Codex case (product code is correct)** |
| MCPUI-08 | ⚠️ Partially verified | ✅ Verified (live-executed) |
| MCPUI-09 | ❌ Needs Fix | ❌ **Needs Fix — same as MCPUI-06** |
| MCPUI-10 | ⚠️ Partially verified | ✅ Verified (live-executed) |
| MCPUI-11 | ✅ Verified | ✅ Verified (live-executed) |
| MCPUI-12 | ✅ Verified | ✅ Verified (live-executed) |
| MCPUI-13 | ✅ Verified | ✅ Verified (live-executed) |
| MCPUI-14 | ✅ Verified | ✅ Verified (live-executed) |
| MCPUI-15 | ✅ Verified (static) | ✅ Verified (static, unchanged) |

---

## Summary

**Overall**: FAIL ❌ — this is the 3rd and final allowed fix→re-verify iteration. Per the bound, this gap set is escalated to the human rather than routed to a 4th round.

**Spec-anchored check**: 11/15 ACs fully closed with live-executed, spec-matched evidence; 1/15 static-only appropriately (MCPUI-15); 1/15 new spec-precision/test-defect gap (MCPUI-07); 2/15 gap (MCPUI-06, -09 — correct code, unreached).
**Gate**: 2/2 mandatory static commands passed (`tsc -b`, `npm run build`). Supplementary live e2e: 4/5 passed, 1/5 failed (deterministic, reproduced twice).
**Sensor**: 3/3 mutations, **all executed live this round** (not reasoned) — 3/3 killed.

**What works**: This round obtained the first real, repeatable live-execution evidence in this feature's entire validation history, both for the base suite and for the discrimination sensor. 11 of 15 ACs are now genuinely proven, not inferred — including the two hardest-to-fake ones from round 2's gap list (MCPUI-02's click-through navigation and the mobile-nav reachability test), both confirmed to actually kill a real regression when mutated. The copy-toast bug fix (`aecece1`) remains correctly implemented in product code, and its dedicated regression-guard assertion is correctly written and would catch a regression — it simply isn't being run today.

**Issues found** (ranked):
1. **(Blocker, newly discovered by live execution)** `e2e/personal-access-tokens.spec.ts:165`'s per-client `${ZEEP_ORBIT_PAT}` assertion is wrong for the Codex/TOML case, fails the live suite deterministically, and as a side effect prevents MCPUI-06/-09's copy-toast assertions (lines 173-177, in the same test) from ever executing. The product code for all of MCPUI-06, -07, -09 is correct; only the test assertion at line 165 is wrong. One-line fix identified (Fix 1 above).
2. **(Process note)** This defect existed since round 1 (`eb0db4f`) and survived round 2 unnoticed because neither round achieved live execution — both relied on static/reasoned analysis, which cannot see a same-test ordering dependency like this. Recorded as a lesson: static reasoning about "does an assertion exist and look right" cannot substitute for actually running the test once test bodies get long enough to have multiple sequential assertions: an earlier failure silently masks whether later assertions would have passed.
3. **(Resolved, no action)** All 3 of round 2's other flagged gaps (MCPUI-02, -04, and the partials on -08/-10) are now confirmed closed by live execution.

**Next steps**: This is the 3rd and final Verifier iteration per the tlc-spec-driven bound — **escalate to the human** rather than route back to a 4th automated fix round. Recommended human decision: apply Fix 1 (a one-line, low-risk test change: drop the `${}` wrapper from the loop's substring check, or assert per-client-format-aware strings) and re-run the live suite once to confirm all 5 tests pass — at that point MCPUI-06, -07, and -09 would all be genuinely closed and the feature would be a clean PASS. Given the fix's size and the fact its correctness is already sensor-confirmed, this is likely a 10-minute fix, but it is being handed to the human rather than auto-applied because the 3-round bound has been reached.
