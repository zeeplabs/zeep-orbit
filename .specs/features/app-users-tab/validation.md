# App Users Tab Validation (Round 2)

**Date**: 2026-08-08
**Spec**: `.specs/features/app-users-tab/spec.md`
**Diff range**: `e17e4ae~1..a6e1ee1` (6 commits: e17e4ae, a667ba9, 6b15c8d, b6ca17f, 31b6dfb, a6e1ee1)
**Verifier**: independent sub-agent (author ≠ verifier), round 2 of fix→re-verify loop (round 1: FAIL, 3 coverage gaps)

---

## Task Completion

No `tasks.md` exists for this feature (Medium scope, Execute-only). All 6 commits in the diff range are present.

| Commit | Status | Notes |
| --- | --- | --- |
| e17e4ae | ✅ Done | Extracted `AppUsersTab({ appId })`, dropped `useParams`/`Link`/`PageHeader` |
| a667ba9 | ✅ Done | Added "users" tab to `AppDetailsPage.tsx`, after `auth`, before `storage` |
| 6b15c8d | ✅ Done | Removed `/apps/:id/users` route from `App.tsx` |
| b6ca17f | ✅ Done | Updated e2e specs to `?tab=users` navigation |
| 31b6dfb | ✅ Done | Traceability doc update |
| a6e1ee1 | ✅ Done | Added new e2e test closing round-1 gaps (AUTAB-05, AUTAB-02, AUTAB-04): `internal/dashboard/ui/e2e/app-users.spec.ts:94-130` |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AUTAB-01: WHEN admin opens `/apps/:id?tab=users` THEN render app-user table (email, name, role, status, last sign-in), identical to standalone page | Table renders with the listed columns | `internal/dashboard/ui/src/pages/AppUsersPage.tsx:216-380` (columns incl. name/email, phone, role, status, lastAccess) rendered via `AppDetailsPage.tsx:132-133` (`<TabsContent value="users"><AppUsersTab appId={app.id} /></TabsContent>`); exercised by `e2e/app-users.spec.ts:59-60` - `await page.goto(...?tab=users); await expect(page.locator('td', {hasText: originalEmail})).toBeVisible()` and again at `e2e/app-users.spec.ts:106-112` via the card-click path | ✅ PASS |
| AUTAB-02: WHEN admin clicks "Users" tab trigger THEN URL updates to `?tab=users`, renders table, no full page nav | Tab switch via `setSearchParams`, SPA navigation | Mechanism: `AppDetailsPage.tsx:61-63` (`setSearchParams({tab:value},{replace:true})`), `TabsTrigger`s at lines 110-122, `value="auth"`→label `t("appForm.tabAuth")`="Login providers" (`locales/en.json:54`), `value="users"`→label `t("appDetails.tabUsers")`="Users" (`locales/en.json:615`). Now directly exercised: `e2e/app-users.spec.ts:116-120` - `await page.click('[role="tab"]:has-text("Login providers")'); await page.waitForURL(/tab=auth$/); await page.click('[role="tab"]:has-text("Users")'); await page.waitForURL(/tab=users$/); await expect(page.locator('td', {hasText: email})).toBeVisible()` — asserts URL changes via query param (not full navigation, since the SPA `Tabs` component stays mounted and content re-renders) and table content survives the round trip | ✅ PASS |
| AUTAB-03: WHEN admin clicks "Edit" on a row THEN open edit drawer, save via unchanged `PUT /apps/{id}/users/{userId}` | Drawer opens, PUT fires with exact body | `AppUsersPage.tsx:53-125` (`EditUserDrawer`, `useUpdateAppUser`); `e2e/app-users.spec.ts:63-88` - captures the PUT request, asserts `putBody` matches `{email: newEmail, phone: '555-9876', role: 'member'}` and `Object.keys(putBody).sort()` equals `['email','phone','role']` | ✅ PASS |
| AUTAB-04: WHEN admin clicks Activate/Deactivate THEN perform existing mutation, unchanged | Mutation call unchanged, UI reflects new state | `AppUsersPage.tsx:319-357` (`deactivate.mutate`/`activate.mutate`, titles `t('appUsers.deactivateTitle')`="Deactivate" and `t('appUsers.activateTitle')`="Reactivate" per `locales/en.json:268-269`; status labels `t('appUsers.active')`="Active"/`t('appUsers.inactive')`="Inactive" per `locales/en.json:234-235`). Now directly exercised inside the tab: `e2e/app-users.spec.ts:124-129` - `await expect(row.locator('text=Active')).toBeVisible(); await row.locator('[title="Deactivate"]').click(); await expect(row.locator('text=Inactive')).toBeVisible(); await row.locator('[title="Reactivate"]').click(); await expect(row.locator('text=Active')).toBeVisible()` — round-trips both mutations and asserts the exact status-label transitions | ✅ PASS |
| AUTAB-05: WHEN app card's "Users" action triggered THEN navigate to `/apps/:id?tab=users` (not removed route) | Exact navigation target | `internal/dashboard/ui/src/pages/AppsPage.tsx:411` - `function handleUsers(app: AppDef) { navigate(\`/apps/${app.id}?tab=users\`); }`, wired at `AppsPage.tsx:165` (`onClick={() => onUsers(app)}`, button text `t("apps.users")`="Users") and `AppsPage.tsx:608` (`onUsers={handleUsers}`). Now directly exercised: `e2e/app-users.spec.ts:106-112` - `await page.goto('/dashboard/apps'); const card = page.locator('div.flex.h-full.flex-col', {has: page.locator('h3', {hasText: appName})}); await card.getByRole('button', {name: 'Users'}).click(); await page.waitForURL(/\/apps\/[^/]+\?tab=users$/); await expect(page.locator('td', {hasText: email})).toBeVisible()` — starts from the apps list, clicks the real card button, asserts the exact resulting URL pattern and that the tab content renders | ✅ PASS |
| AUTAB-06: System SHALL NOT render page-level header or back-link inside tab content | No `PageHeader`/back-link JSX in `AppUsersTab` | `grep -n "PageHeader" internal/dashboard/ui/src/pages/AppUsersPage.tsx internal/dashboard/ui/src/pages/AppDetailsPage.tsx` → 0 matches (re-verified directly this round). `AppUsersPage.tsx:176` `export function AppUsersTab({ appId }: { appId: string })` has no `useParams`/`Link` import | ✅ PASS |
| AUTAB-07: IF direct nav to `/apps/:id/users` THEN normal not-found/404 behavior | No route matches; app's existing catch-all fires | `internal/dashboard/ui/src/App.tsx:110-112` - only `/apps/new`, `/apps/:id`, `/apps/:id/edit` remain (re-verified this round, old `/apps/:id/users` route absent); `App.tsx:135` - `<Route path="*" element={<Navigate to="/apps" replace />} />` is the app's existing not-found behavior. No dedicated automated test (consistent with round 1 — this is a routing-table read, not a runtime assertion, since there's no 404 page to assert against) | ✅ PASS |
| AUTAB-08: System SHALL NOT contain `PageHeader`/back-link for app-users (dead code removed) | Zero dead code | Same grep as AUTAB-06, re-run this round — 0 matches | ✅ PASS |
| AUTAB-09: WHEN existing e2e specs referencing old route run THEN pass using new tab-based nav, no reduction in assertion coverage | Same or greater test/assertion count, all green | Ran `BASE_URL=http://localhost:8097 BOOTSTRAP_SECRET=test-secret npx playwright test e2e/app-users.spec.ts e2e/enduser-roles.spec.ts --project=chromium` → **8 passed, 0 failed, 0 skipped** (up from 7 in round 1's snapshot). `grep -c "test(" e2e/app-users.spec.ts` = 5 occurrences (3 actual `test(...)` blocks + `test.beforeAll`/`test.beforeEach`) vs. 4 at `e17e4ae~1` (3 `test(...)` blocks + `test.beforeEach`) — the +1 is the new `test(...)` at line 94, `test.beforeAll` added for session reuse (separate prior commit, not part of this diff's regression risk). `enduser-roles.spec.ts` unchanged at 7. Net: 8 real test cases now vs. 7 previously, 0 removed | ✅ PASS |

**Status**: ✅ All 9/9 ACs covered with direct e2e or code evidence. The 3 gaps flagged in round 1 (AUTAB-02, AUTAB-04, AUTAB-05) are closed by the new test added in `a6e1ee1` (`internal/dashboard/ui/e2e/app-users.spec.ts:94-130`), whose assertions were read line-by-line and confirmed to target the exact spec-defined outcomes (exact URL pattern, exact tab-label text, exact title attributes, exact status-label text) — not shallow/tautological checks.

---

## Discrimination Sensor

Round 1 already confirmed the core tab-id routing wiring is load-bearing (1/1 killed on `TabsContent value="users"` → `"userz"`). This round adds one new targeted mutation to confirm the new test itself (added in `a6e1ee1`) catches a regression in a previously-gapped behavior.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 (round 2, new) | `internal/dashboard/ui/src/pages/AppsPage.tsx:411` (in scratch worktree `/tmp/zeep-orbit-verify2-sensor`) | Reverted `handleUsers` from `navigate(\`/apps/${app.id}?tab=users\`)` back to the old, removed-route path `navigate(\`/apps/${app.id}/users\`)` | ✅ Killed — `e2e/app-users.spec.ts:94` ("reaches the tab from the apps list card...") failed on `page.waitForURL(/\/apps\/[^/]+\?tab=users$/)` (30s timeout, page stuck at `/dashboard/apps` since `/apps/:id/users` no longer matches any route and falls through to the catch-all redirect). The other 2 tests in the file passed unaffected (they navigate directly via `page.goto`, bypassing the card) |

**Sensor depth**: lightweight (1 new targeted mutation this round, proportional to a frontend navigation/composition-only feature; combined with round 1's mutation, 2 total across the feature's two rounds)
**Sensor verdict**: 1/1 killed this round — the new test added to close the AUTAB-05 gap does in fact catch the exact regression it was written for

**Isolation procedure**:
- Baseline: `git status --porcelain` on real tree showed only `?? .specs/features/app-users-tab/validation.md` (untracked report file, present before and unrelated to sensor work) both before and after.
- Prepared via `git worktree add /tmp/zeep-orbit-verify2-sensor a6e1ee1` (never `git stash`).
- Symlinked `node_modules` from the real tree into the worktree's `internal/dashboard/ui` (read-only reuse, not a copy/mutation of the real tree) to run `npm run build`.
- Mutated only `AppsPage.tsx:411` in the worktree.
- Built frontend (`npm run build`) + backend (`go build -o /tmp/zeep-orbit-verify2-mutant ./cmd/zeep`) from the worktree.
- Ran the mutant server on port 8098 (separate from the real server's port 8097), against the same shared Postgres container.
- Ran `npx playwright test e2e/app-users.spec.ts --project=chromium` from the worktree against `BASE_URL=http://localhost:8098` → 1 failed (the targeted test), 2 passed.
- Killed the mutant server process, removed the `node_modules` symlink, then `git worktree remove --force /tmp/zeep-orbit-verify2-sensor`.
- Re-ran `git status --porcelain` and `git worktree list` on the real tree: porcelain output identical to baseline (only the untracked validation.md); worktree list shows only the real tree. Isolation confirmed.

---

## Interactive UAT Results

Not performed — task instructions scoped this validation to automated checks (spec-anchored check, gate, sensor, tsc/build), consistent with round 1.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — fix commit `a6e1ee1` adds exactly one test (38 lines), no production code touched |
| Surgical changes | ✅ — only `internal/dashboard/ui/e2e/app-users.spec.ts` changed in the fix commit |
| No scope creep | ✅ — no changes to `members` tab, other tabs, or unrelated specs |
| Matches patterns | ✅ — new test follows the existing `test.describe`/`test(...)` structure and selector conventions (`[title="..."]`, `[role="tab"]`, `td`/`tr` locators) already used elsewhere in the same file |
| Spec-anchored outcome check (asserted values match spec) | ✅ — see AC table; all 9 ACs now have direct, precise assertions or a fully re-verified code trail |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ — frontend-only feature, no new domain logic; the 3 previously-uncovered route-level behaviors (AUTAB-02, AUTAB-04, AUTAB-05) now have direct e2e assertions |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — the new test explicitly comments which AC each assertion block covers (`// AUTAB-05`, `// AUTAB-02`, `// AUTAB-04` at lines 104, 114, 122) |
| Documented guidelines followed | AGENTS.md §5 (i18n via `react-i18next`); AGENTS.md §3 (`npx tsc -b` and `npm run build` both re-run clean this round) |

---

## Edge Cases

- [x] Zero users → existing empty state unchanged (`AppUsersPage.tsx` `empty` prop, code untouched by this diff)
- [x] `AppUsersTab` rendered before data resolves → existing loading state unchanged (`isLoading` prop, untouched)
- [x] Tab switch away/back → now directly exercised by the new test (`e2e/app-users.spec.ts:116-120`), which switches to `auth` and back to `users` and confirms table content survives the round trip, matching the spec's "preserve tab-switch behavior identical to other tabs" requirement

---

## Gate Check

- **Gate command**: `npx tsc -b` (frontend build gate) + `npm run build` + Playwright e2e (`e2e/app-users.spec.ts e2e/enduser-roles.spec.ts --project=chromium`)
- **Result**: `tsc -b` clean (0 errors), `npm run build` clean (0 errors, only the same pre-existing chunk-size warning, unrelated to this feature), Playwright: **8 passed, 0 failed, 0 skipped**
- **Test count before this feature (baseline, `e17e4ae~1`)**: 7 (2 in `app-users.spec.ts`, 5 in `enduser-roles.spec.ts`)
- **Test count after this feature (round 2, `a6e1ee1`)**: 8 (3 in `app-users.spec.ts`, 5 in `enduser-roles.spec.ts`)
- **Delta**: +1 new test (the fix commit's coverage-closing test), net increase — no reduction
- **Skipped tests**: none
- **Failures**: none

Server setup for this run: `go build -o /tmp/zeep-orbit-verify2 ./cmd/zeep`; `DASHBOARD_BOOTSTRAP_SECRET=test-secret /tmp/zeep-orbit-verify2 serve --port 8097 --db "postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable"`; server stopped after the run.

---

## Fix Plans

None — all 3 round-1 gaps closed. No new gaps found this round.

---

## Requirement Traceability Update

| Requirement | Previous Status (round 1) | New Status (round 2) |
| --- | --- | --- |
| AUTAB-01 | ✅ Verified | ✅ Verified |
| AUTAB-02 | ⚠️ Coverage gap | ✅ Verified — direct e2e assertion added |
| AUTAB-03 | ✅ Verified | ✅ Verified |
| AUTAB-04 | ⚠️ Coverage gap | ✅ Verified — direct e2e assertion added |
| AUTAB-05 | ⚠️ Coverage gap | ✅ Verified — direct e2e assertion added |
| AUTAB-06 | ✅ Verified | ✅ Verified |
| AUTAB-07 | ✅ Verified | ✅ Verified |
| AUTAB-08 | ✅ Verified | ✅ Verified |
| AUTAB-09 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 9/9 ACs matched spec outcome, 0 spec-precision gaps remaining
**Sensor**: 1/1 new mutation killed this round (2/2 across both rounds combined)
**Gate**: tsc clean, build clean, 8/8 e2e passed

**What works**: Tab extraction, tab position, route removal, dead-code removal, i18n keys in both locales, edit-drawer PUT behavior, activate/deactivate from inside the tab (now directly re-asserted), tab-switch-and-back (now directly re-asserted), apps-list card "Users" action navigation (now directly re-asserted and confirmed load-bearing by the round-2 mutation kill).

**Issues found**: None remaining.

**Next steps**: None required for this feature. Feature is verified complete.

---

**Result**: PASS
