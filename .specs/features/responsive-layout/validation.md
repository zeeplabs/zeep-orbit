# Responsive Layout Validation

**Date**: 2026-08-28
**Spec**: `.specs/features/responsive-layout/spec.md`
**Diff range**: `d7e2d53^..HEAD` (branch `develop`, 10 commits, HEAD = `0455591`)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 — `MOBILE_TABS` +SDKs | ✅ Done | `src/components/layout/nav.ts:66` — `{ icon: 'code', labelKey: 'SDKs', path: '/sdks' }`, mirrors the `NAV_SECTIONS` entry (`nav.ts:46`). |
| T2 — Playwright projects | ✅ Done | `playwright.config.ts:16-42` — 4 projects. SPEC_DEVIATION (declared): `testMatch`/`testIgnore` scoping means the 3 new projects run only `responsive-nav.spec.ts`. Justified. |
| T3 — `Sidebar.tsx` tablet rail | ⚠️ Partial | Rail implemented (`Sidebar.tsx:87`), but the "no visible text labels" half of RESP-02 AC1 has no assertion (see AC table). 3 declared SPEC_DEVIATIONs are legitimate and documented in-code (`Sidebar.tsx:82-85`, `DashboardShell.tsx:132-135`, `Sidebar.tsx:23-28`). |
| T4 — MobileNav 5-slot bar | ⚠️ Partial | Bar renders 5 slots; RESP-01 AC4 (role omission) and AC5 (safe-area) have no real assertion. |
| T5 — Ultra-wide cap | ⚠️ Partial | Cap works and is mutation-verified (`DashboardShell.tsx:141`), but RESP-03 AC3 (≤1920px unchanged) has no test. |
| T6 — `min-w-[420px]` Brand Settings | ❌ Test invalid | Source fix is correct (`BrandSettingsPage.tsx:151,241,387,540,661` → `min-w-0`), but its test passes with the fix fully reverted (surviving mutant, see Sensor). |
| T7 — `min-w-[420px]` GitHub Integration | ❌ Test invalid | Same: source fix correct (`GitHubIntegrationPage.tsx:334,1037,1292`), test non-discriminating. |
| T8 — Data Browser tablet parity | ✅ Done | `DataBrowserPage.tsx:291-292` (`max-md:`→`max-lg:`), asserted at `responsive-nav.spec.ts:172-175`. |
| T9 — CHANGELOG | ✅ Done | `CHANGELOG.md:16-22` — Added + Fixed entries under `[Unreleased]`, per AGENTS.md §6. |

Every "Done when" box in `tasks.md` is rendered unchecked (`- [ ]`) despite tasks being marked ✅ Complete — cosmetic bookkeeping drift, not a functional gap.

---

## Spec-Anchored Acceptance Criteria

All test citations are `internal/dashboard/ui/e2e/responsive-nav.spec.ts` unless noted.

### RESP-01 — Mobile bottom bar with 5 slots (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `<768px` → fixed bottom bar with **exactly 5** slots (Apps, Data Browser, Logs, SDKs, More) | exactly 5 named slots | `e2e/responsive-nav.spec.ts:71-77` — `expect(bottomBar.getByRole('link', { name: 'Apps' })).toBeVisible()` ×4 + `getByRole('button', { name: 'More' })` | ⚠️ Spec-precision gap — presence of 5 named slots asserted; **cardinality is not** (no `toHaveCount(5)` on the bar's children, so a 6th slot passes) |
| AC2: "More" opens a bottom sheet listing every remaining role-visible `NAV_SECTIONS` item, **grouped exactly as the desktop sidebar groups them** | Users, Audit, Integrations, Settings, MCP present *and* grouped | `e2e/responsive-nav.spec.ts:89-96` — `expect(sheet.getByRole('link', { name: 'MCP' })).toBeVisible()` (+4 more) | ⚠️ Spec-precision gap — membership asserted, grouping (section headings/order) not asserted |
| AC3: tapping a fixed item navigates and marks it active with the desktop active-indicator convention (**fill/weight + tint**) | route change + active visual (fill/weight/tint) | `e2e/responsive-nav.spec.ts:80-85` — `expect(page).toHaveURL(/\/dashboard\/logs$/)`, `toHaveAttribute('aria-current','page')` | ⚠️ Spec-precision gap — navigation ✅; the asserted proxy is `aria-current`, not the fill/weight/tint the AC names |
| AC4: role without `platformAction` → item **omitted** from the More drawer | omission for a non-superadmin role | *no citation* — only the superadmin-sees-all direction is tested (`:89-96`) | ❌ GAP (evidence-or-zero) |
| AC5: bottom bar stays above `env(safe-area-inset-bottom)` down to 375px | bar respects the safe-area inset | `e2e/responsive-nav.spec.ts:100-101` — `expect(box?.height).toBeGreaterThanOrEqual(60)` | ❌ GAP — tautological. `MobileNav.tsx:67` sets `height: 60` inline and `env(safe-area-inset-bottom, 0px)` resolves to `0` under Playwright's Desktop Chrome emulation, so the value is *always exactly 60*; deleting the `paddingBottom` rule cannot fail this assertion |

### RESP-02 — Tablet collapsed icon-only sidebar (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `≥768px` & `<1024px` → fixed **72px** icon-only rail showing every role-visible `NAV_SECTIONS` item, **no visible text labels** | width 72px; all items; labels hidden | `e2e/responsive-nav.spec.ts:19-21` — `expect(box?.width).toBeGreaterThanOrEqual(64)` / `toBeLessThanOrEqual(80)`; `:25` wordmark hidden | ⚠️ Partial — width ✅ (mutation-killed); **item-completeness not asserted** (only 2 of 11 links referenced); **nav-item label hiding not asserted** (only the wordmark `:25` and the *section title* `:32` are) — reverting `Sidebar.tsx:60`'s `hidden lg:inline` would not fail any test |
| AC2: hover/focus a rail icon → tooltip with the item's label | tooltip containing the label | `e2e/responsive-nav.spec.ts:41-43` — `expect(page.getByRole('tooltip', { name: 'Apps' })).toBeVisible()` | ✅ PASS (hover path; focus path untested) |
| AC3: activate a rail icon → navigate + desktop-equivalent active treatment on the icon container | route change + active state | `e2e/responsive-nav.spec.ts:48-53` — `toHaveURL(/\/dashboard\/data-browser$/)` + `toHaveAttribute('aria-current','page')` | ✅ PASS |
| AC4: section grouping preserved via a thin separator, section title text not rendered | separator present, title hidden | `e2e/responsive-nav.spec.ts:32` — `expect(aside.getByText('General', { exact: true })).toBeHidden()`; `:35` — `expect(aside.getByRole('separator')).toHaveCount(2)` | ✅ PASS — and non-shallow: `en.json` really renders `"General"`, so the hidden-check targets present-but-hidden text |
| AC5: tablet rail and bottom bar mutually exclusive | bottom bar absent | `e2e/responsive-nav.spec.ts:38` — `expect(page.getByRole('button', { name: 'More' })).toBeHidden()` | ✅ PASS |

### RESP-03 — Desktop sidebar unchanged, ultra-wide content capped (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `≥1024px` → existing **264px** full sidebar with labels, no visual regression | 264px sidebar, labels visible | `e2e/personal-access-tokens.spec.ts:185-203` (unchanged, `chromium` @1280px) — `page.getByRole('link', { name: 'MCP' }).click()` + `toHaveURL(/\/dashboard\/mcp-settings$/)` | ⚠️ Spec-precision gap — proves a labeled desktop nav link is present and clickable (real regression value: it caught the `NavRow` label regression during T3); does **not** assert the 264px width |
| AC2: `>1920px` → `<main>` content capped at **max-width 1920px** and **centered**, sidebar pinned left | rendered width ≤ 1920px; centered; sidebar at viewport left edge | `e2e/responsive-nav.spec.ts:119-120` — `expect(box?.width).toBeLessThanOrEqual(1920)` | ⚠️ Partial — cap ✅ (mutation-killed); **centering** and **sidebar-pinned-left** not asserted |
| AC3: `≥1024px` and `≤1920px` → content uses full available width, unchanged | width == available width at e.g. 1440px/1920px | *no citation* — no project runs between 1024px and 1920px with a content-width assertion | ❌ GAP (evidence-or-zero) |
| AC4: no page-level horizontal scroll from 1024px to 3840px on **any** `NAV_SECTIONS` route | zero overflow, all routes, full width range | `e2e/responsive-nav.spec.ts:122-131` — `expect(overflowX).toBeLessThanOrEqual(0)` on `/apps` + `/data-browser` @2560px | ⚠️ Weak — 2 of 9 routes, 1 of 5 spec'd widths, and the `:127-131` measurement shares the pre-paint defect proven below (measured immediately after `goto`, before content renders) |

### RESP-04 — Ultra-wide content cap (traceability entry)

`spec.md:167` lists RESP-04 as a distinct requirement ID, but no story/AC block carries that ID (its ACs live inside the RESP-03 story) and `tasks.md` maps T5 to "RESP-03 (AC2-4)" without ever citing RESP-04. **Substantively covered** by RESP-03 AC2 (`e2e/responsive-nav.spec.ts:120`); **formally orphaned** in the traceability matrix. Documentation defect, not a behavior gap.

### RESP-05 — Fix confirmed sub-420px overflow in settings forms (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `<420px` → every field that declared `min-w-[420px]` renders at full available width instead of a 420px floor | no 420px floor; field fills available width | Source fix verified present: `BrandSettingsPage.tsx:151,241,387,540,661` and `GitHubIntegrationPage.tsx:334,1037,1292` all now `min-w-0`; no `min-w-[420px]` remains in either file | ⚠️ Source correct, **no width assertion** in any test |
| AC2: no horizontal overflow on `/configuracoes` or `/integracoes/github` at **375px** | `scrollWidth <= innerWidth` at 375px on both routes | `e2e/responsive-nav.spec.ts:146-156` — `expect(brandOverflow).toBeLessThanOrEqual(0)` / `expect(githubOverflow).toBeLessThanOrEqual(0)` | ❌ GAP — assertion is **non-discriminating** (surviving mutant #3/#3b, root cause proven below). Also runs at **390px**, not the 375px the AC/Independent Test/Success Criteria all specify, while the in-file comment claims 375px (`responsive-nav.spec.ts:137,139`) |

### RESP-06 — Playwright coverage for the three nav states and ultra-wide cap (P2)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: mobile project (390×844) asserts exactly 5 slots + role-appropriate More drawer | 5 slots, role-filtered drawer | `playwright.config.ts:29-33` (project `mobile`, 390×844) + `e2e/responsive-nav.spec.ts:71-96` | ⚠️ Partial — inherits RESP-01 AC1/AC4 gaps (no cardinality, no role-negative case) |
| AC2: tablet project (820×1180) asserts the rail renders (not full sidebar, not bottom bar) and clicking navigates | rail-only + navigation | `playwright.config.ts:35-38` + `e2e/responsive-nav.spec.ts:17-53` | ✅ PASS |
| AC3: ultra-wide project (2560×1440) asserts main content width ≤ 1920px | ≤1920px | `playwright.config.ts:39-42` + `e2e/responsive-nav.spec.ts:120` | ✅ PASS |

### RESP-07 — Data Browser tablet layout parity (P3)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `<1024px` → `DataBrowserPage` uses the mobile collapsed/scrollable table-list behavior instead of the `240px_1fr` grid | collapsed (flex column) layout at tablet | `e2e/responsive-nav.spec.ts:172-175` — `expect(display).toBe('flex')` on `div.grid.h-full.min-h-full.items-stretch` (computed style @820px) | ✅ PASS — targets the exact class swap at `DataBrowserPage.tsx:291`; a revert to `max-md:` yields `display: grid` and fails. The paired `max-lg:max-h-[220px]` strip class (`:292`) is not separately asserted |

**Status**: ❌ Gaps present — 4 hard gaps (RESP-01 AC4, RESP-01 AC5, RESP-03 AC3, RESP-05 AC2), 8 spec-precision/partial gaps.

---

## Discrimination Sensor

Scratch: `git worktree add /tmp/zeep-sensor-wt HEAD` (never `git stash`). Each mutation required a full `npm run build` + `go build` (assets are embedded in the Go binary) and a dedicated `zeep serve` instance.

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/dashboard/ui/src/components/layout/Sidebar.tsx:87` | `md:w-[72px]` → `md:w-[264px]` (tablet rail renders at full width, defeating RESP-02 AC1) | ✅ Killed — `[tablet]` failed at `responsive-nav.spec.ts:21` (`expect(box?.width).toBeLessThanOrEqual(80)`) |
| 2 | `internal/dashboard/ui/src/pages/DashboardShell.tsx:141` | Removed ` max-w-[1920px]` (content stretches edge-to-edge, defeating RESP-03 AC2) | ✅ Killed — `[ultrawide]` failed at `responsive-nav.spec.ts:120` (`expect(box?.width).toBeLessThanOrEqual(1920)`) |
| 3 | `internal/dashboard/ui/src/pages/BrandSettingsPage.tsx:151` | `min-w-0` → `min-w-[420px]` on one Brand Settings field (reinstates the RESP-05 overflow bug) | ❌ **Survived** — `[mobile]` 2 passed, 0 failed |
| 3b | `BrandSettingsPage.tsx` (5×) + `GitHubIntegrationPage.tsx` (3×) | Full revert of RESP-05: every `min-w-0` → `min-w-[420px]` in both files | ❌ **Survived** — `[mobile] -g "No overflow below 420px"` → 1 passed |

**Root cause of the surviving mutant** (diagnosed empirically in the scratch worktree, against the mutant build at 390px):

```
IMMEDIATE {"sw":390,"iw":390,"txt":"Loading..."}
LATER     {"sw":436,"iw":390,"wide":["min-w-[420px] flex-1 rounded-[14px] border border-[var(--border)] bg-["]}
```

`responsive-nav.spec.ts:146-156` calls `page.evaluate(...)` immediately after `page.goto(...)` with **no wait on any page content**. At that moment the route is still rendering its `Loading...` fallback, so `document.documentElement.scrollWidth === window.innerWidth` unconditionally. Four seconds later the real content is painted and the overflow appears (`scrollWidth` 436 vs `innerWidth` 390), attributed to the exact `min-w-[420px]` element. The assertion therefore proves nothing about RESP-05 — it would pass on a codebase where the bug was never fixed.

The same immediate-measure pattern appears at `responsive-nav.spec.ts:122-131` (RESP-03 AC4), so those overflow assertions carry the same defect; mutation #2 was killed by the `boundingBox()` assertion (which auto-waits), not by them.

**Sensor depth**: lightweight (4 mutations, 3 planned + 1 escalation to isolate the survivor)
**Result**: 2/4 killed — FAIL ❌

---

## Interactive UAT Results

Not performed by the Verifier — visual/interaction judgment for the 3 nav states is the orchestrator's UAT step with the user.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ — every change is a class swap, one array entry, one prop, one config block. No new component, no `useMediaQuery`, no new dependency. |
| Surgical changes | ✅ — 8 source files, all named in `design.md`; no drive-by edits to unrelated code. |
| No scope creep | ✅ — the deferred items in `spec.md`'s Out of Scope / Assumptions tables (fixed 2-col grids, `DataTable` column hiding, drawer generalization) were genuinely left alone. |
| Matches patterns | ✅ — pure-Tailwind responsive variants, consistent with the repo's zero-`matchMedia` convention; `Tooltip`/`Separator` reused from `ui/`. |
| Spec-anchored outcome check | ❌ — RESP-01 AC5 asserts a constant (`height >= 60`); RESP-05 AC2 asserts a pre-paint measurement; RESP-01 AC3 / RESP-02 AC1 assert proxies (`aria-current`, rail width) rather than the visual outcomes the ACs name. |
| Per-layer Coverage Expectation met (`tasks.md` matrix: "every RESP-NN acceptance criterion gets an explicit e2e assertion") | ❌ — RESP-01 AC4, RESP-01 AC5, RESP-03 AC3 have no assertion; RESP-05 AC2's assertion is inert. |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — all 5 tests carry an explicit `RESP-NN` header comment; no orphan tests. |
| Documented guidelines followed: `AGENTS.md` §3 (frontend gate), §5 (i18n), §6 (CHANGELOG) | ✅ — `npx tsc -b` and `npm run build` clean; no new user-facing strings introduced (all labels reuse existing `en.json`/`pt-BR.json` keys, and `labelKey: 'SDKs'` matches the pre-existing raw-string convention already used at `nav.ts:46`); `CHANGELOG.md` updated in the same change. |
| Would a senior engineer approve? | ⚠️ — the implementation, yes. The test file, not as-is: one of its five tests cannot fail. |

**Non-blocking code observations** (no fix task required, recorded for the record):

1. `Sidebar.tsx:87` — the class list carries both `flex` and `hidden` in the base layer (`... flex h-screen ... hidden md:flex ...`). It behaves correctly (Tailwind emits `hidden` after `flex`, empirically confirmed: the rail is hidden at 390px and the bottom bar is the only nav), but it reads as a contradiction and depends on utility emit order rather than being explicit.
2. `Sidebar.tsx:108` — the separator is gated on `index > 0` over the raw `NAV_SECTIONS` index, not the index among *rendered* sections. If section 0 were ever fully role-gated out, the first visible section would render a leading separator — the failure mode `spec.md:154`'s edge case warns about. Latent only (section 0 has no `platformAction`), and untested.
3. `Sidebar.tsx:37` — a `TooltipProvider` is instantiated per `NavRow` (11 providers per render) instead of one per nav surface. The in-code rationale (`:33-36`) is sound (`MobileNav`'s sheet has no provider ancestor); hoisting one provider into each of the two consumers would be the tidier form.
4. `playwright.config.ts` — running `npx playwright test` (the `tasks.md` **Build** gate, all projects) makes every test log in fresh and reliably trips the login rate limiter: my first combined 3-project run produced one spurious `too many requests` login failure at `helpers.ts:26`, which passed on a re-run 70s later. CI fragility worth knowing about; not introduced by this feature, but the feature multiplies the login count.

---

## Edge Cases

From `spec.md:153-156` — none of the four has a test:

- [ ] Breakpoint crossed while the bottom sheet is open → overlay must close. **Not covered** (no resize test). `design.md:135` argues the sheet's `md:hidden` handles it via CSS; unverified.
- [ ] Role with zero visible items in a group → group heading/separator omitted at every breakpoint. **Not covered**; see code observation #2 for the latent counter-case.
- [ ] Exact boundary widths (768px, 1024px, 1920px) resolve to the wider range. **Not covered** — projects run at 390/820/2560 only, so no boundary is exercised.
- [ ] Live resize re-renders the correct nav state without reload. **Not covered** (all tests load at a fixed viewport).

---

## Gate Check

- **Gate command** (`tasks.md` Full/Build gate, scoped to this feature's surface): `cd internal/dashboard/ui && npx tsc -b && npm run build && BASE_URL=http://localhost:8081 npx playwright test --project=mobile --project=tablet --project=ultrawide` plus the desktop regression check `--project=chromium -g "MCP nav link"`
- **Environment**: throwaway Postgres DB `e2e_verify` (Docker `zeep-orbit-db-1`, port 5434) + `zeep serve --port 8081` built from HEAD after a fresh `npm run build`
- **`npx tsc -b`**: clean
- **`npm run build`**: clean (`✓ built in 1.65s`; pre-existing >500 kB chunk warning only)
- **Result**: 6 of 6 targeted tests green, 0 failed
  - `[mobile]` 2/2 — bottom bar; settings overflow
  - `[tablet]` 2/2 — icon rail; Data Browser parity
  - `[ultrawide]` 1/1 — content cap
  - `[chromium] -g "MCP nav link"` 1/1 — RESP-03 AC1 desktop regression check
- **Transient**: the first combined 3-project run showed 1 login failure (`too many requests` at `helpers.ts:26`) — server-side login rate limiting from repeated runs, reproduced and cleared by waiting 70s; not a code defect. See code observation #4.
- **Test count before feature**: 26 (`chromium`, `e2e/*.spec.ts`)
- **Test count after feature**: 31 (26 unchanged + 5 new in `e2e/responsive-nav.spec.ts`)
- **Delta**: +5
- **Skipped tests**: 10 per full 3-project run — by design: each test guards itself with `test.skip(testInfo.project.name !== '<project>', ...)` and all 3 new projects `testMatch` the same file, so each of the 5 tests is skipped under the 2 projects it does not target. Justified, though it makes the reported counts noisy.
- **Failures**: none in the targeted gate. Per the orchestrator's known context, the full 26-test `chromium` suite carries ~11-12 pre-existing environmental failures unrelated to this feature; not re-run, and the targeted desktop regression check passes.
- **Test integrity**: no test deleted, no assertion weakened. `e2e/personal-access-tokens.spec.ts` is untouched in the diff (verified via `git diff --stat`).

---

## Fix Plans

### Fix 1: RESP-05's overflow test cannot fail — assertion runs before the page paints

- **Root cause**: `e2e/responsive-nav.spec.ts:146-156` measures `document.documentElement.scrollWidth` immediately after `page.goto()`, while the route still renders its `Loading...` fallback. Proven: full revert of the RESP-05 fix (mutation #3b) leaves the test green; the overflow only materializes ~seconds later (`scrollWidth` 436 vs `innerWidth` 390).
- **Fix task**: before each measurement, await a real content locator on the loaded page (e.g. `await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()` for `/configuracoes`, and the equivalent for `/integracoes/github`), then measure. Apply the same wait to `:122-131` (RESP-03 AC4). Additionally set the viewport to the spec'd **375×667** for this test (`page.setViewportSize`) or correct the in-file comments at `:137,139` that claim 375px while the project runs 390px.
- **Verify**: re-run mutation #3b — the test must fail with the fix reverted and pass with it in place.
- **Done when**: mutation #3b is killed.
- **Priority**: **Blocker** — this is the only coverage for the one confirmed pre-existing production bug the feature set out to fix.

### Fix 2: RESP-01 AC5 (safe-area inset) assertion is a constant

- **Root cause**: `responsive-nav.spec.ts:100-101` asserts `height >= 60` while `MobileNav.tsx:67` hardcodes `height: 60` and `env(safe-area-inset-bottom, 0px)` resolves to 0 under Desktop Chrome emulation.
- **Fix task**: either assert the mechanism directly (computed `padding-bottom` of the bar resolves from `env(safe-area-inset-bottom, 0px)`, i.e. the declaration is present in the rendered style) and that the bar's bottom edge sits at `window.innerHeight`; or drop the assertion and record AC5 as untestable in this harness rather than leaving a green check that proves nothing.
- **Priority**: Major.

### Fix 3: RESP-01 AC4 — role-gated omission in the More drawer is untested

- **Root cause**: only the superadmin-sees-everything direction is asserted (`:89-96`).
- **Fix task**: add a mobile-project case using a non-superadmin account (or an intercepted `/dashboard/api/me` role) asserting the gated entries are **absent** from `mobile-nav-sheet` (`toHaveCount(0)`), mirroring the existing desktop gating expectation.
- **Priority**: Major.

### Fix 4: RESP-03 AC3 — "≤1920px unchanged, full width" has no test

- **Root cause**: no Playwright project runs between 1024px and 1920px.
- **Fix task**: add a content-width assertion at a sub-cap desktop width (reuse the `chromium` project @1280px, or add a `desktop-wide` 1920×1080 project) asserting the content wrapper width equals the available width right of the sidebar.
- **Priority**: Minor.

### Fix 5: Assertion-strength gaps (batchable)

- RESP-01 AC1: assert slot **cardinality** (`expect(bottomBar.locator(':scope > *')).toHaveCount(5)`).
- RESP-02 AC1: assert nav-item **labels are hidden** at rail width (e.g. `expect(aside.getByText('Apps', { exact: true })).toBeHidden()`) and that all role-visible items are present in the rail — currently reverting `Sidebar.tsx:60` breaks nothing.
- RESP-03 AC2: assert the content wrapper is **centered** and the `<aside>` starts at `x === 0`.
- **Priority**: Minor.

### Fix 6: Traceability / bookkeeping

- `spec.md:167` RESP-04 has no AC block and no task reference — either fold it into RESP-03 or give it its own ACs.
- `tasks.md` "Done when" checkboxes are all still `- [ ]` on tasks marked ✅ Complete.
- **Priority**: Cosmetic.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| RESP-01 | Implementing | ❌ Needs Fix (AC4 uncovered, AC5 inert) |
| RESP-02 | Implementing | ⚠️ Verified with gaps (AC1 label-hiding + item-completeness unasserted) |
| RESP-03 | Implementing | ❌ Needs Fix (AC3 uncovered; AC4 weak) |
| RESP-04 | Implementing | ⚠️ Covered via RESP-03 AC2; orphaned in the traceability matrix |
| RESP-05 | Implementing | ❌ Needs Fix (source correct, test non-discriminating — surviving mutant) |
| RESP-06 | Implementing | ⚠️ Verified with gaps (AC1 inherits RESP-01 gaps) |
| RESP-07 | Implementing | ✅ Verified |

---

## Summary

**Overall**: ❌ Not Ready

**Verdict**: FAIL

**Spec-anchored check**: 8 of 20 ACs match the spec-defined outcome; 4 hard gaps (no evidence or inert evidence); 8 spec-precision/partial gaps
**Sensor**: 2 of 4 mutations killed — one surviving mutant (RESP-05), root-caused
**Gate**: 6 targeted tests green, 0 failed; `tsc -b` and `npm run build` clean

**What works** (implementation, independently confirmed):

- Tablet 72px icon-only rail with tooltips and section separators, mutually exclusive with the bottom bar — `Sidebar.tsx:87,109`, mutation-verified.
- 1920px content cap on ultra-wide, sidebar outside the capped wrapper — `DashboardShell.tsx:141`, mutation-verified.
- Mobile 5-slot bottom bar (Apps/Data Browser/Logs/SDKs/More) with a role-filtered More drawer — `nav.ts:66`, `MobileNav.tsx:69-78`.
- Data Browser two-pane collapse widened to `<1024px` — `DataBrowserPage.tsx:291-292`.
- The `min-w-[420px]` floors really are gone from both pages, and the grid-track SPEC_DEVIATION in `DashboardShell.tsx:132-141` was a genuine catch without which the rail would have left a 192px gap.

**Issues found**: Fix 1 (Blocker — RESP-05 test cannot fail), Fix 2 and Fix 3 (Major), Fix 4 and Fix 5 (Minor), Fix 6 (Cosmetic).

**Next steps**: route Fix 1 → Fix 3 to an implementer as fix tasks, re-run the sensor (mutation #3b must be killed), then re-verify. No production-code change is required for Fix 1 — the defect is entirely in the test.
