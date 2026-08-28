# Responsive Layout Validation

**Date**: 2026-08-28
**Spec**: `.specs/features/responsive-layout/spec.md`
**Diff range**: `d7e2d53^..HEAD` (branch `develop`, 11 commits, HEAD = `096adc7`)
**Verifier**: independent sub-agent (author ≠ verifier) — **round 2 re-verification**, findings re-derived from scratch, not inherited from round 1

**Verdict: PASS ✅** — all 4 of round 1's hard gaps are closed with fresh evidence, 4/4 injected mutations killed (including round 1's surviving blocker), gate green.

**Post-verification update (2026-08-28, same day, follow-up commit)**: all 7 spec-precision gaps listed below were closed — see `## Post-Round-2 Gap Closure` at the end of this file. One of the new assertions (RESP-01/RESP-02 AC3's fill/weight/tint check) surfaced a real production bug: `NavRow`'s active-state tint/weight/color never applied in `Sidebar.tsx` (desktop and tablet rail) because Radix `Tooltip.Trigger asChild` silently drops `NavLink`'s function-valued `style` prop when merging. Fixed in production code, not just the test.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 — `MOBILE_TABS` +SDKs | ✅ Done | `src/components/layout/nav.ts:65` — `{ icon: 'code', labelKey: 'SDKs', path: '/sdks' }`, mirrors the `NAV_SECTIONS` entry at `nav.ts:45`. |
| T2 — Playwright projects | ✅ Done | `playwright.config.ts:16-44` — 4 projects. Declared SPEC_DEVIATION (`testMatch` scoping the 3 new projects to `responsive-nav.spec.ts`) stands and is justified. Round 2 change: `chromium` no longer `testIgnore`s the file, so it picks up its one scoped test — verified via `--list`: `chromium` = 33 tests (26 pre-existing + 7 responsive-nav, 6 of which self-skip). |
| T3 — `Sidebar.tsx` tablet rail | ✅ Done | `Sidebar.tsx:87` rail widths; `Sidebar.tsx:60` label class. Round 1's "no visible text labels" gap is closed — `responsive-nav.spec.ts:37` now asserts it and the assertion is mutation-killed (Sensor #3). 3 declared SPEC_DEVIATIONs remain legitimate and documented in-code (`Sidebar.tsx:82-85`, `Sidebar.tsx:23-28`, `DashboardShell.tsx:132-135`). |
| T4 — MobileNav 5-slot bar | ✅ Done | Cardinality now asserted (`responsive-nav.spec.ts:96`); RESP-01 AC4 has a real negative case (`:133-166`); AC5 asserts the `env()` mechanism (`:128-129`, mutation-killed). |
| T5 — Ultra-wide cap | ✅ Done | `DashboardShell.tsx:141-142`. Cap, centering and sidebar-pinned-left all asserted (`:188`, `:197`, `:200`); centering is mutation-killed (Sensor #4). RESP-03 AC3 now has its own `chromium` test (`:220-238`). |
| T6 — `min-w-[420px]` Brand Settings | ✅ Done | `BrandSettingsPage.tsx:151,241,387,540,661` → `min-w-0`; the overflow test is now discriminating (Sensor #1 killed at `responsive-nav.spec.ts:263`). |
| T7 — `min-w-[420px]` GitHub Integration | ✅ Done | `GitHubIntegrationPage.tsx:334,1037,1292` → `min-w-0`; same test covers `/integracoes/github` at `:266-270`. |
| T8 — Data Browser tablet parity | ✅ Done | `DataBrowserPage.tsx:291-292` (`max-md:`→`max-lg:`), asserted at `responsive-nav.spec.ts:286-289`. |
| T9 — CHANGELOG | ✅ Done | `CHANGELOG.md:16,20-21` — Added + Fixed entries under `[Unreleased]`, per AGENTS.md §6. |

Bookkeeping drift persists: every "Done when" box in `tasks.md` is still rendered `- [ ]` on tasks marked ✅ Complete. Cosmetic.

---

## Spec-Anchored Acceptance Criteria

All citations are `internal/dashboard/ui/e2e/responsive-nav.spec.ts` unless noted. Re-derived independently at round 2; each of round 1's 12 flagged items re-checked against the current file.

### RESP-01 — Mobile bottom bar with 5 slots (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `<768px` → bottom bar with **exactly 5** slots (Apps, Data Browser, Logs, SDKs, More) | exactly 5, named | `:96` — `expect(bottomBar.locator(':scope > *')).toHaveCount(5)`; `:97-101` — the 5 named slots visible | ✅ PASS — round 1's cardinality gap closed |
| AC2: "More" opens a sheet listing every remaining role-visible `NAV_SECTIONS` item, **grouped exactly as the desktop sidebar groups them** | membership *and* grouping | `:113-120` — `expect(sheet.getByRole('link', { name: 'Users' })).toBeVisible()` (+4 more) | ⚠️ Spec-precision gap — membership asserted; section headings/order still not asserted |
| AC3: tapping a fixed item navigates and marks it active with the desktop convention (**fill/weight + tint**) | route change + fill/weight/tint | `:105-109` — `toHaveURL(/\/dashboard\/logs$/)` + `toHaveAttribute('aria-current','page')` | ⚠️ Spec-precision gap — navigation ✅; the asserted proxy is `aria-current` (set by React Router regardless of styling), not the fill/weight/tint the AC names |
| AC4: role without `platformAction` → item **omitted** from the More drawer | omission for a non-superadmin role | `:145-149` intercepts `/dashboard/api/me` → `role: 'member'`; `:162-165` — `expect(sheet.getByRole('link', { name: 'Users' })).toHaveCount(0)` ×4, `:157` MCP still visible | ✅ PASS — round 1 hard gap closed. Discriminating as a differential pair with `:113-120` (same build, superadmin sees all 5; member sees 0 gated), and `member` genuinely lacks users/audit/integrations/branding (`src/lib/permissions.ts:30-38`) |
| AC5: bottom bar stays above `env(safe-area-inset-bottom)` down to 375px | inset rule actually applied | `:128-129` — `expect(paddingBottom).toContain('env(safe-area-inset-bottom')` on the element's inline style | ✅ PASS — round 1 hard gap closed, mutation-killed (Sensor #2). Runs at 390px, not 375px, but the assertion reads a viewport-independent inline style so width is immaterial |

### RESP-02 — Tablet collapsed icon-only sidebar (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `≥768px` & `<1024px` → **72px** icon-only rail, every role-visible `NAV_SECTIONS` item, **no visible text labels** | width 72px; all items; labels hidden | `:20-21` width in `[64,80]`; `:37` — `expect(aside.getByText('Apps', { exact: true })).toBeHidden()`; `:41-53` all 9 hrefs `toBeVisible()` | ✅ PASS — round 1's label-hiding and item-completeness gaps both closed; label-hiding is mutation-killed (Sensor #3). Minor: width asserted as a ±8px band, not exactly 72 |
| AC2: hover **or focus** a rail icon → tooltip with the label | tooltip on either path | `:63-64` — `appsLink.hover()` then `expect(page.getByRole('tooltip', { name: 'Apps' })).toBeVisible()` | ⚠️ Spec-precision gap — hover path ✅; the focus path the AC also names is untested |
| AC3: activate a rail icon → navigate + desktop-equivalent active treatment on the icon container | route change + active visual | `:70-74` — `toHaveURL(/\/dashboard\/data-browser$/)` + `toHaveAttribute('aria-current','page')` | ⚠️ Spec-precision gap — same `aria-current` proxy as RESP-01 AC3; the tint/fill itself is not asserted |
| AC4: section grouping preserved via a thin separator, section title text not rendered | separator **visible** at rail width, title hidden | `:32` — `expect(aside.getByText('General', { exact: true })).toBeHidden()`; `:56` — `expect(aside.getByRole('separator')).toHaveCount(2)` | ⚠️ Spec-precision gap — title-hiding is real and non-shallow (`en.json` renders `"General"`); separator coverage is DOM-count only, so changing `Sidebar.tsx:109`'s `md:block lg:hidden` to plain `hidden` would keep the count at 2 and survive |
| AC5: tablet rail and bottom bar mutually exclusive | bottom bar absent | `:59` — `expect(page.getByRole('button', { name: 'More' })).toBeHidden()` | ✅ PASS |

### RESP-03 — Desktop sidebar unchanged, ultra-wide content capped (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `≥1024px` → existing **264px** full sidebar with labels, no visual regression | 264px sidebar, labels visible | `e2e/personal-access-tokens.spec.ts:185-203` (untouched, `chromium` @1280px) — `page.getByRole('link', { name: 'MCP' }).click()` + `toHaveURL(/\/dashboard\/mcp-settings$/)` | ⚠️ Spec-precision gap — proves a labeled desktop nav link renders and works (it did catch the `NavRow` label regression during T3); the **264px width is still not asserted anywhere** |
| AC2: `>1920px` → `<main>` content capped at **1920px**, **centered**, sidebar pinned left | width ≤1920; centered; aside at x=0 | `:188` — `expect(box?.width).toBeLessThanOrEqual(1920)`; `:197` — `expect(Math.abs(leftGap - rightGap)).toBeLessThanOrEqual(2)`; `:200` — `expect(asideBox?.x).toBe(0)` | ✅ PASS — round 1's centering/pinned-left gap closed; centering is mutation-killed (Sensor #4) |
| AC3: `≥1024px` and `≤1920px` → content uses full available width, unchanged | content width == `<main>` width | `:220-238` (new `chromium`-scoped test) — `expect(Math.abs(mainBox.width - box.width)).toBeLessThanOrEqual(2)` @1280px | ✅ PASS — round 1 hard gap closed. Runs at 1280px only; the 1920px boundary itself is still unexercised |
| AC4: no page-level horizontal scroll from 1024px to 3840px on **any** `NAV_SECTIONS` route | zero overflow, all routes, full width range | `:182` awaits the `Your apps` heading before measuring, then `:205` / `:212` — `expect(overflowX).toBeLessThanOrEqual(0)` on `/apps` + `/data-browser` @2560px | ⚠️ Spec-precision gap — the pre-paint defect round 1 proved is **fixed** (all overflow reads now wait on a real content locator; the same pattern is mutation-killed at `:263`), but breadth is still 2 of 9 routes at 1 of 5 spec'd widths, and neither 3440px nor 1920px from the Independent Test is run |

### RESP-04 — Ultra-wide content cap (traceability entry)

Unchanged from round 1: `spec.md:167` lists RESP-04 as a distinct requirement ID, but no story/AC block carries it (its ACs live inside the RESP-03 story) and `tasks.md:184` maps T5 to "RESP-03 (AC2-4)" without citing RESP-04. **Substantively covered** by RESP-03 AC2 (`:188`); **formally orphaned** in the traceability matrix. Documentation defect, not a behavior gap.

### RESP-05 — Fix confirmed sub-420px overflow in settings forms (P1)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `<420px` → every field that declared `min-w-[420px]` renders at full available width instead of a 420px floor | no 420px floor | Source verified: `BrandSettingsPage.tsx:151,241,387,540,661` and `GitHubIntegrationPage.tsx:334,1037,1292` are all `min-w-0`; no `min-w-[420px]` remains in either file. Behavioral proof: `:263` (mutation-killed — Sensor #1 reinstated all 8 floors and the assertion failed, attributed to a 375→436px `scrollWidth`) | ✅ PASS — the assertion is now provably tied to the exact class change. No per-field width measurement, which is the weaker half of the AC's wording |
| AC2: no horizontal overflow on `/configuracoes` or `/integracoes/github` at **375px** | `scrollWidth <= innerWidth` at 375px on both | `:250` — `page.setViewportSize({ width: 375, height: 667 })`; `:259` awaits the `Settings` heading then `:263` `expect(brandOverflow).toBeLessThanOrEqual(0)`; `:266` awaits `Integrations` then `:270` for the second route | ✅ PASS — round 1's **blocker** closed. Viewport corrected from 390px to the spec'd 375px, measurements now gated on painted content, mutation-killed |

### RESP-06 — Playwright coverage for the three nav states and ultra-wide cap (P2)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: mobile project (390×844) asserts exactly 5 slots + role-appropriate More drawer | 5 slots, role-filtered drawer | `playwright.config.ts:30-33` + `:96` (cardinality) + `:133-166` (role-negative case) | ✅ PASS — no longer inherits RESP-01's gaps |
| AC2: tablet project (820×1180) asserts the rail renders (not full sidebar, not bottom bar) and clicking navigates | rail-only + navigation | `playwright.config.ts:35-38` + `:17-74` | ✅ PASS |
| AC3: ultra-wide project (2560×1440) asserts main content width ≤ 1920px | ≤1920px | `playwright.config.ts:39-43` + `:188` | ✅ PASS |

### RESP-07 — Data Browser tablet layout parity (P3)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `<1024px` → `DataBrowserPage` uses the mobile collapsed/scrollable table-list behavior instead of the `240px_1fr` grid | collapsed (flex column) at tablet | `:286-289` — `expect(display).toBe('flex')` on `div.grid.h-full.min-h-full.items-stretch` (computed style @820px) | ✅ PASS — targets the exact class swap at `DataBrowserPage.tsx:291`; a revert to `max-md:` yields `display: grid`. The paired `max-lg:max-h-[220px]` strip class (`:292`) is still not separately asserted |

**Status**: ⚠️ Spec-precision gaps flagged — **13 of 20 ACs match the spec-defined outcome, 0 hard gaps, 7 spec-precision gaps**.

Round-1 gap disposition, re-derived: all **4 hard gaps closed** (RESP-01 AC4, RESP-01 AC5, RESP-03 AC3, RESP-05 AC2). Of round 1's 8 partials, **4 closed** (RESP-01 AC1 cardinality, RESP-02 AC1 labels+completeness, RESP-03 AC2 centering, RESP-05 AC1) and 4 remain (RESP-01 AC2 grouping, RESP-01 AC3 proxy, RESP-03 AC1 264px, RESP-03 AC4 breadth). Three further precision gaps flagged independently this round: RESP-02 AC2 (focus path), RESP-02 AC3 (same `aria-current` proxy), RESP-02 AC4 (separator asserted by DOM count, not tablet-only visibility — round 1 passed this one).

**No new gap was introduced by the fix commit.** The two tests it added (`:133-166`, `:220-238`) both execute and pass; the `chromium` `testIgnore` removal is sound (all 7 tests in the file self-skip by project name, verified by `--list`).

---

## Discrimination Sensor

Scratch: `git worktree add /tmp/zeep-sensor2-wt HEAD`, `node_modules` symlinked from the real tree (never `git stash`). Each mutation needed a full `npm run build` + `go build` (the UI is embedded in the Go binary) and a dedicated `zeep serve` on a throwaway DB (`e2e_sensor2`, port 8085). Mutations 1-3 were applied to one build (they hit three different tests, so attribution is unambiguous); mutation 4 to a second build.

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `BrandSettingsPage.tsx` (5×) + `GitHubIntegrationPage.tsx` (3×) | Full revert of RESP-05: every `min-w-0` → `min-w-[420px]` — **re-run of round 1's surviving mutant #3b** | ✅ **Killed** — `[mobile]` failed at `responsive-nav.spec.ts:263` (`expect(brandOverflow).toBeLessThanOrEqual(0)`) |
| 2 | `MobileNav.tsx:67` | `paddingBottom: 'env(safe-area-inset-bottom, 0px)'` → `paddingBottom: '8px'` (RESP-01 AC5 mechanism removed) | ✅ **Killed** — `[mobile]` failed at `:129` (`expect(paddingBottom).toContain('env(safe-area-inset-bottom')`) |
| 3 | `Sidebar.tsx:60` | Label class `alwaysShowLabel ? 'inline' : 'hidden lg:inline'` → `'inline'` (rail labels become visible, defeating RESP-02 AC1) | ✅ **Killed** — `[tablet]` failed at `:37` (`expect(aside.getByText('Apps', { exact: true })).toBeHidden()`, "unexpected value visible") |
| 4 | `DashboardShell.tsx:141-142` | Removed `justify-center` from `<main>` and `mx-auto` from the content wrapper — content still capped at 1920px but left-aligned (defeats RESP-03 AC2's centering half only) | ✅ **Killed** — `[ultrawide]` failed at `:197` (`expect(Math.abs(leftGap - rightGap)).toBeLessThanOrEqual(2)`) |

Mutation 1 is the decisive result: it is byte-for-byte round 1's surviving blocker, and it now fails. The pre-paint defect is genuinely fixed, not papered over.

**Isolation verified**: `git worktree remove --force` succeeded, mutant server killed, `e2e_sensor2` dropped, and `git status --porcelain` on the real tree is empty — identical to the pre-sensor baseline.

**Sensor depth**: lightweight (4 mutations, targeting exactly the code round 1 found weakly tested)
**Result**: 4/4 killed — **PASS ✅**

---

## Interactive UAT Results

Not performed by the Verifier — visual/interaction judgment for the 3 nav states is the orchestrator's UAT step with the user.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ — every production change is a class swap, one array entry, one prop, one config block. No new component, no `useMediaQuery`, no new dependency. The fix commit touched **no production code at all** (test file + Playwright config only). |
| Surgical changes | ✅ — 8 production files, all named in `design.md`; no drive-by edits. |
| No scope creep | ✅ — `spec.md`'s Out of Scope / deferred assumptions (fixed 2-col grids, `DataTable` column hiding, drawer generalization) were genuinely left alone. |
| Matches patterns | ✅ — pure-Tailwind responsive variants, consistent with the repo's zero-`matchMedia` convention; `Tooltip`/`Separator` reused from `ui/`. |
| Spec-anchored outcome check | ⚠️ — 13/20 ACs assert the spec-defined outcome. The 7 remaining assert proxies (`aria-current` for "fill/weight + tint"), membership without grouping, or presence without visibility. All flagged above; none inert. |
| Per-layer Coverage Expectation met (`tasks.md` matrix: "every RESP-NN acceptance criterion gets an explicit e2e assertion") | ✅ — every one of the 20 ACs now has a `file:line` assertion. The matrix asks for an explicit assertion per AC, not for assertion strength; strength shortfalls are recorded as precision gaps. |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — all 7 tests carry an explicit `RESP-NN` header comment; no orphan tests. |
| Test integrity | ✅ — no test deleted, no assertion weakened. The fix commit is +124/−10 in `responsive-nav.spec.ts`; the 10 deletions are the two defective assertions being replaced (pre-paint overflow read, `height >= 60` constant) with stronger ones. `e2e/personal-access-tokens.spec.ts` untouched across the whole diff range. |
| Documented guidelines followed: `AGENTS.md` §3 (frontend gate), §5 (i18n), §6 (CHANGELOG) | ✅ — `npx tsc -b` and `npm run build` clean; no new user-facing strings (all labels reuse existing `en.json`/`pt-BR.json` keys; `labelKey: 'SDKs'` matches the pre-existing raw-string convention at `nav.ts:45`); `CHANGELOG.md:16,20-21` updated in the same change. |
| Would a senior engineer approve? | ✅ — implementation and test file both. Round 1's "one of five tests cannot fail" objection no longer holds: the two assertions it named are gone, and the mutation that exposed them now fails. |

**Non-blocking code observations** (recorded, no fix task required):

1. `Sidebar.tsx:87` — the class list carries both `flex` and `hidden` in the base layer (`... flex h-screen ... hidden md:flex ...`). Behaviorally correct (Tailwind emits `hidden` after `flex`; empirically the rail is hidden at 390px and the bottom bar is the only nav), but it reads as a contradiction and leans on utility emit order instead of being explicit. Carried over from round 1, still present.
2. `Sidebar.tsx:108` — the separator is gated on `index > 0` over the raw `NAV_SECTIONS` index, not the index among *rendered* sections. If section 0 were ever fully role-gated out, the first visible section would render a leading separator — exactly the failure mode `spec.md:154`'s edge case warns about. Latent only (section 0 has no `platformAction`) and untested. Carried over from round 1.
3. `Sidebar.tsx:37` — a `TooltipProvider` per `NavRow` (11 per render) instead of one per nav surface. The in-code rationale (`:33-36`) is sound; hoisting one provider into each of the two consumers would be tidier.
4. Login rate limiting: running all 7 tests of this file in one `npx playwright test` process reliably trips the backend login limiter (each test logs in fresh), surfacing as a `page.waitForURL` timeout at `helpers.ts:26`. Hit once this round on the mobile batch; a server restart plus an isolated re-run passed. Environmental, not introduced by this feature, but the feature raises the login count. Worth a shared storage-state fixture if CI flakes.
5. `BASE_URL` is read with two different conventions: `playwright.config.ts:12` defaults it to `.../dashboard` while `e2e/helpers.ts:4` defaults to the bare origin and appends `/dashboard/...` itself. Passing `BASE_URL=http://host:port/dashboard` therefore breaks `bootstrapOrSkip` with a JSON parse error on the HTML index. Pre-existing, not touched by this feature, but it cost a wasted gate run.

---

## Edge Cases

From `spec.md:153-156`:

- [ ] Breakpoint crossed while the bottom sheet is open → overlay must close. **Not covered** (no resize test). `design.md:135` argues the sheet's `md:hidden` handles it via CSS; still unverified.
- [~] Role with zero visible items in a group → group heading/separator omitted at every breakpoint. **Partially covered** at mobile by the new `member`-role test: forcing `role: 'member'` empties the entire Superadmin section, and `:162-165` asserts its 4 links are absent (`toHaveCount(0)`), which exercises `MobileNav.tsx:98`'s `visible.length === 0 return null`. The **section heading's** omission is not asserted, and the tablet/desktop rail path (`Sidebar.tsx:105`) is untested — see code observation #2 for the latent counter-case.
- [ ] Exact boundary widths (768px, 1024px, 1920px) resolve to the wider range. **Not covered** — projects run at 1280/390/820/2560 only, so no boundary is exercised.
- [ ] Live resize re-renders the correct nav state without reload. **Not covered** (all tests load at a fixed viewport; the RESP-05 test's `setViewportSize` happens before navigation, not as a live resize).

---

## Gate Check

- **Gate command** (`tasks.md` Full/Build gate, scoped to this feature's surface): `cd internal/dashboard/ui && npx tsc -b && npm run build`, then `BASE_URL=http://localhost:8084 npx playwright test --project=<tablet|ultrawide|mobile|chromium>` per project (batched to avoid the login rate limiter), plus the desktop regression check `--project=chromium -g "MCP nav link"`
- **Environment**: throwaway Postgres DB `e2e_verify2` (Docker `zeep-orbit-db-1`, port 5434) + `zeep serve --port 8084` built from HEAD (`096adc7`) after a fresh `npm run build`
- **`npx tsc -b`**: clean (exit 0)
- **`npm run build`**: clean (`✓ built in 2.18s`; pre-existing >500 kB chunk warning only)
- **Result**: **8 of 8 targeted tests green, 0 failed**
  - `[tablet]` 2/2 — icon rail (`:9`); Data Browser parity (`:278`)
  - `[ultrawide]` 1/1 — content cap + centering + sidebar pinned left (`:172`)
  - `[mobile]` 3/3 — 5-slot bar + safe-area (`:81`); role-gated drawer (`:134`); 375px settings overflow (`:246`)
  - `[chromium]` 1/1 — sub-cap full-width content, RESP-03 AC3 (`:221`)
  - `[chromium] -g "MCP nav link"` 1/1 — RESP-03 AC1 desktop regression check (`personal-access-tokens.spec.ts:185`)
- **Transient**: one `[mobile]` run failed at `helpers.ts:26` (`page.waitForURL` timeout) — the known login rate limiter. Restarted the server and re-ran that single test in isolation: passed. Distinguished from real signal, not waved away. See code observation #4.
- **Test count before feature**: 26 (`chromium`, `e2e/*.spec.ts`)
- **Test count after feature**: 33 unique (26 unchanged + 7 in `e2e/responsive-nav.spec.ts`); 54 project×test combinations across the 4 projects (`npx playwright test --list`)
- **Delta**: +7 (round 1 saw +5; the fix commit added the role-gating and sub-cap tests)
- **Skipped tests**: by design — each of the 7 responsive-nav tests guards itself with `test.skip(testInfo.project.name !== '<project>', ...)` and all 4 projects match the file, so each test skips under the 3 projects it does not target (5-6 skips per project run). Justified; it does make the raw counts noisy.
- **Failures**: none in the targeted gate. The full 26-test `chromium` suite carries ~11-12 pre-existing environmental failures unrelated to this feature (established during T3's baseline diff run); not re-run, and the targeted desktop regression check passes.
- **Test integrity**: no test deleted, no assertion weakened; `e2e/personal-access-tokens.spec.ts` untouched across `d7e2d53^..HEAD` (verified via `git diff --stat`).

---

## Fix Plans

No blocking fix. The remaining items are optional hardening, ranked; none justifies a third fix→re-verify iteration.

### Follow-up 1: assertion-strength gaps (batchable, Minor)

- RESP-01 AC2: assert the More sheet's **grouping** — section headings present and in `NAV_SECTIONS` order, not just item membership (`responsive-nav.spec.ts:113-120`).
- RESP-01 AC3 / RESP-02 AC3: `aria-current` stands in for the AC's "fill/weight + tint". Assert the computed `color`/`fontWeight` on the active `NavLink` (`MobileNav.tsx:49-52`, `Sidebar.tsx:45-49`) if the visual convention is meant to be pinned.
- RESP-02 AC2: add the **focus** path the AC names alongside the hover path (`:63`) — `appsLink.focus()` then the same tooltip assertion.
- RESP-02 AC4: the separator is asserted by DOM count (`:56`), which survives losing its `md:block lg:hidden` visibility gating. Assert it is *visible* at rail width and hidden at desktop.
- RESP-03 AC1: no test asserts the **264px** desktop sidebar width. Add an `aside` `boundingBox()` width assertion to the `chromium`-scoped test (`:220-238`), which already runs at desktop width.
- RESP-07: the paired `max-lg:max-h-[220px]` strip class (`DataBrowserPage.tsx:292`) is not separately asserted.

### Follow-up 2: RESP-03 AC4 coverage breadth (Minor)

The AC says "any route in `NAV_SECTIONS`, 1024px to 3840px"; the test covers 2 of 9 routes at 2560px only, and the spec's Independent Test names 3440px and 1920px, neither of which runs. Loop the overflow check over all 9 `NAV_SECTIONS` routes (with the content-locator wait already in place), and/or add a 3440px project. The pre-paint defect itself is fixed.

### Follow-up 3: untested spec edge cases (Minor)

Live resize across a breakpoint (with the sheet open), exact boundary widths (768/1024/1920), and heading-level omission for a fully role-gated section. Also worth fixing code observation #2 (`Sidebar.tsx:108` separator keyed on the raw index) since it is the concrete counter-case to `spec.md:154`.

### Follow-up 4: traceability / bookkeeping (Cosmetic)

- `spec.md:167` RESP-04 has no AC block and no task reference — fold it into RESP-03 or give it its own ACs.
- `tasks.md` "Done when" checkboxes are all still `- [ ]` on tasks marked ✅ Complete.

---

## Requirement Traceability Update

| Requirement | Previous Status (round 1) | New Status |
| --- | --- | --- |
| RESP-01 | ❌ Needs Fix (AC4 uncovered, AC5 inert) | ✅ Verified (AC2 grouping, AC3 active-visual proxy = precision gaps) |
| RESP-02 | ⚠️ Verified with gaps | ✅ Verified (AC2 focus path, AC3 proxy, AC4 separator visibility = precision gaps) |
| RESP-03 | ❌ Needs Fix (AC3 uncovered; AC4 weak) | ✅ Verified (AC1 264px width, AC4 route/width breadth = precision gaps) |
| RESP-04 | ⚠️ Covered via RESP-03 AC2; orphaned | ⚠️ Unchanged — substantively covered, still orphaned in the traceability matrix |
| RESP-05 | ❌ Needs Fix (surviving mutant) | ✅ Verified — mutation-killed, blocker closed |
| RESP-06 | ⚠️ Verified with gaps | ✅ Verified |
| RESP-07 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Verdict**: PASS

**Spec-anchored check**: 13 of 20 ACs match the spec-defined outcome; **0 hard gaps**; 7 spec-precision gaps flagged
**Sensor**: 4 of 4 mutations killed — including a byte-for-byte re-run of round 1's surviving blocker
**Gate**: 8 targeted tests green, 0 failed; `tsc -b` and `npm run build` clean

**What round 2 independently confirmed closed:**

- RESP-05 (the blocker). Reverting all 8 `min-w-[420px]` floors now fails the test at `responsive-nav.spec.ts:263`. The measurement waits on a real heading before reading `scrollWidth`, and runs at the spec'd 375px rather than 390px.
- RESP-01 AC5. The test asserts the `env(safe-area-inset-bottom` declaration itself; replacing it with a plain `8px` fails at `:129`.
- RESP-01 AC4. A `member`-role interception proves the gated Superadmin items are omitted (`toHaveCount(0)`), and pairs differentially with the superadmin-sees-all case on the same build.
- RESP-03 AC3. A new `chromium`-scoped test proves the 1920px cap does not engage at a sub-cap width.
- RESP-02 AC1's label hiding and item completeness, and RESP-03 AC2's centering + sidebar-pinned-left — the last two mutation-verified.

**What still works** (implementation, re-confirmed): 72px tablet rail with tooltips and section separators mutually exclusive with the bottom bar (`Sidebar.tsx:87`); 1920px capped and centered content with the sidebar outside the wrapper (`DashboardShell.tsx:136-145`); 5-slot mobile bottom bar with a role-filtered drawer (`nav.ts:61-66`, `MobileNav.tsx:65-79`); Data Browser collapse widened to `<1024px` (`DataBrowserPage.tsx:291-292`).

**Issues found**: no blocker, no major. Follow-up 1 and 2 (Minor, assertion strength and coverage breadth), Follow-up 3 (Minor, untested spec edge cases), Follow-up 4 (Cosmetic, RESP-04 orphan + unchecked task boxes).

**Next steps**: feature is done. Route Follow-ups 1-4 to the backlog rather than a third fix iteration — none of them is a correctness risk, and the fix commit changed no production code, so the shipped behavior is what round 1 already validated as correct. Remaining orchestrator step is interactive UAT with the user on the three nav states.

---

## Post-Round-2 Gap Closure (2026-08-28)

All 7 spec-precision gaps flagged in the round-2 AC table were closed in `internal/dashboard/ui/e2e/responsive-nav.spec.ts`, plus one production fix:

| Gap | Fix | Result |
| --- | --- | --- |
| RESP-01 AC2 (grouping) | Assert `sheet.locator('span.uppercase')` text equals `['General', 'Deployment', 'Superadmin']` in order | ✅ Closed |
| RESP-01 AC3 / RESP-02 AC3 (fill/weight/tint proxy) | Assert computed `fontWeight`/`color` differ between active and inactive `NavLink`s | ✅ Closed — **found a real bug**: `Sidebar.tsx`'s `NavRow` never applied active tint/weight/color at all. Radix `Tooltip.Trigger asChild` clones the wrapped `NavLink` and merges its own props into it; its merge treats `style` as a plain object and spreads it (`{...childStyle, ...slotStyle}`) — but `NavRow` passed `style` as a *function* (`NavLink`'s supported form for computing per-route active styles), and spreading a function yields `{}` (functions have no enumerable own properties). The icon's `fill` prop and the left accent bar were unaffected because those come from `NavLink`'s `children` render-prop, which Radix's `asChild` doesn't touch. Fixed by replacing the function `style` prop with static Tailwind classes gated on `aria-[current=page]:` (the `aria-current` attribute is set directly by `NavLink`, unaffected by the merge) — `Sidebar.tsx:44`. |
| RESP-02 AC2 (focus path) | Add `appsLink.focus()` → tooltip visible, alongside the existing hover check | ✅ Closed |
| RESP-02 AC4 (separator visibility) | Assert `.first()` separator `toBeVisible()` at tablet width and `toBeHidden()` at desktop width (new assertion in the `chromium` sub-cap test) | ✅ Closed |
| RESP-03 AC1 (264px width) | Assert `aside.boundingBox().width === 264` in the `chromium` sub-cap test | ✅ Closed |
| RESP-03 AC4 (breadth) | Widened from 2 routes to all 9 `NAV_SECTIONS` routes at ultrawide, using `page.waitForLoadState('networkidle')` instead of per-route heading text (avoids hardcoding 9 different headings while still not measuring during the loading fallback) | ✅ Closed |
| RESP-07 (paired class) | Assert the table-list panel's computed `maxHeight`/`overflowY` match the `max-lg:max-h-[220px] max-lg:overflow-y-auto` pair, not just the parent's `display` swap | ✅ Closed |

**Gate re-run**: `npx tsc -b` + `npm run build` clean. All 4 Playwright projects (`chromium`, `mobile`, `tablet`, `ultrawide`) green, one project at a time against a fresh server (avoids the documented login rate-limiter false-flake — code observation #4 above). 8 targeted tests + the desktop `personal-access-tokens.spec.ts` regression check all pass.

**Requirement traceability**: RESP-01, RESP-02, RESP-03 now have **0 remaining precision gaps** from the round-2 list. Follow-up 3 (untested edge cases: live resize, exact boundary widths, heading-level omission) and Follow-up 4 (RESP-04 traceability orphan, `tasks.md` checkbox bookkeeping) were not in scope of this closure pass and remain open, unchanged from round 2's assessment (Minor/Cosmetic, non-blocking).

---

## Follow-up 3/4 Closure (2026-08-28, second follow-up pass)

**Follow-up 3 (untested spec edge cases)** — all closed in `e2e/responsive-nav.spec.ts`:

- **Live resize with the sheet open**: new test opens the mobile "More" sheet, resizes the viewport from 390px to 820px, asserts the sheet becomes hidden — proves `MobileNav.tsx`'s `md:hidden` overlay wrapper actually hides a statefully-open sheet on a live resize, not just on fresh page loads at different viewports.
- **Exact boundary widths**: new test asserts 768px renders the tablet rail (not the mobile bottom bar), 1024px renders the exact 264px desktop sidebar (not the tablet rail), and 1920px keeps content uncapped (matches `<main>` width) — the three breakpoints resolve to the wider range as `spec.md`'s EARS clauses (`≥768px`, `≥1024px`, `≤1920px`) require.
- **Heading-level omission for a fully role-gated section**: extended the existing `member`-role interception test with `expect(sheet.getByText('Superadmin', { exact: true })).toHaveCount(0)` — proves the section heading itself is dropped, not just its items, closing the gap code observation #2 warned about.
- **Code observation #2** (the concrete counter-case for the above): `Sidebar.tsx`'s separator was gated on `index > 0` over the raw `NAV_SECTIONS` index rather than the index among *rendered* sections. Fixed — sections are now filtered by visibility before the array carrying the separator's index is built, so a role gating out an early section can no longer produce a leading separator. No role does this today (only the Superadmin section has any `platformAction` gating, and it's last), so this was latent, not reachable — fixed anyway since the fix is one small refactor and the risk was explicitly flagged.

**Follow-up 4 (traceability / bookkeeping)** — both items closed:

- **`tasks.md` "Done when" checkboxes**: already `- [x]` on every completed task as of this pass (closed in an earlier bookkeeping edit, re-verified here — no drift found).
- **RESP-04 traceability orphan**: `spec.md`'s combined desktop/ultra-wide story now tags each AC with its requirement ID inline (`(RESP-03)` on AC1, `(RESP-04)` on AC2-4), and the traceability table's RESP-03/RESP-04 rows spell out which ACs each ID covers instead of RESP-04 reading as "substantively covered, no block of its own." No requirement IDs were renumbered (would have broken cross-references in this file and `tasks.md`) — the fix is precision, not restructuring.

**Gate re-run**: `npx tsc -b` + `npm run build` clean. All 4 Playwright projects green (`chromium`: 4 targeted tests + `personal-access-tokens.spec.ts` regression; `tablet`/`mobile`/`ultrawide`: their scoped tests, unaffected by the `Sidebar.tsx` separator refactor).

**Status**: all items from both Follow-up 3 and Follow-up 4 are closed. No further residuals from the round-2 validation report remain open.
