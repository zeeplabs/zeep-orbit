# App User Phone Country Mask Validation

**Date**: 2026-08-08
**Spec**: `.specs/features/app-user-phone-mask/spec.md`
**Diff range**: `be15200~1..1daf1a3` (5 commits: `be15200`, `dfb8909`, `0bd29f3`, `701ec8b`, `1daf1a3`)
**Verifier**: independent sub-agent (author ≠ verifier)

**Note on process artifacts**: No `design.md` or `tasks.md` exist for this feature (only `spec.md` was committed). Gate check commands were taken from `AGENTS.md` §3 and the caller's documented local test infra, not from a `tasks.md` "Gate Check Commands" section (none exists).

---

## Task Completion

No `tasks.md` exists for this feature, so there are no discrete tasks to check off. Verification proceeded directly against `spec.md`'s 9 ACs and the diff.

---

## Spec-Anchored Acceptance Criteria

### P1: Country-aware phone entry in the edit drawer (AUT-01..AUT-06)

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AUT-01: recognized `dialCode` prefix (longest match) pre-selects country + pre-fills national digits | Reopening drawer with stored `+5511987654321` shows country=Brazil, input value `(11) 98765-4321` | `internal/dashboard/ui/e2e/app-users.spec.ts:124-128` - `expect(countrySelect).toContainText('Brazil')`; `expect(page.locator('[role="dialog"] input:not([type="email"])')).toHaveValue('(11) 98765-4321')`. Longest-match logic itself: `internal/dashboard/ui/src/components/patterns/PhoneInput.tsx:31-36` (`c.dialCode.length > best.dialCode.length`) | ✅ PASS |
| AUT-02: empty/unmatched `phone` defaults country to `BR` | Country select shows "Brazil" for a new user with no phone | `internal/dashboard/ui/e2e/app-users.spec.ts:109-110` - `await expect(countrySelect).toContainText('Brazil')` (new user, no phone set) | ✅ PASS |
| AUT-03: switching country re-masks currently-typed digits with new country's mask | After switching to Albania, input shows Albania-mask formatting of the same digits (`119 876 5432`) | `internal/dashboard/ui/e2e/app-users.spec.ts:142-146` - `await page.click('[role="option"]:has-text("Albania (+355)")')`; `expect(...).toHaveValue('119 876 5432')` | ✅ PASS |
| AUT-04: live formatting via `globalCellphoneMask(selectedCountryCode, value)` | Typed national digits are masked live in the displayed input value | `internal/dashboard/ui/src/components/patterns/PhoneInput.tsx:66,91-96` - `const masked = globalCellphoneMask(country, national)`; rendered as `<Input value={masked} .../>`. Behaviorally confirmed via `e2e/app-users.spec.ts:128` (`toHaveValue('(11) 98765-4321')` from raw typed `11987654321`) | ✅ PASS |
| AUT-05: Save sends `phone` as `+{dialCode}{clearMask(input)}` (empty string if input empty) | PUT body `phone` field equals `+5511987654321` for BR digits, `+35511987654321` after switching to Albania | `internal/dashboard/ui/e2e/app-users.spec.ts:89` - `expect(putBody).toMatchObject({..., phone: '+5511987654321', ...})`; `:148-149` - `expect(putBody2.phone).toBe('+35511987654321')`. Empty-string case covered at the backend layer only (see AUT-08's `EmptyPhoneAllowed` test) - no explicit e2e assertion that clearing the field sends `phone: ""`. | ✅ PASS (empty-string send-path: ⚠️ Spec-precision gap - no direct frontend test asserts an emptied `PhoneInput` produces `onChange('')` in the PUT body; only backend accepts empty phone) |
| AUT-06: `BR` listed first, remainder alphabetical by `en` name | First option in the country dropdown listbox is Brazil | `internal/dashboard/ui/e2e/app-users.spec.ts:138-140` - `await expect(page.locator('[role="listbox"] [role="option"]').first()).toContainText('Brazil')`. Sort implementation: `internal/dashboard/ui/src/components/patterns/PhoneInput.tsx:13-17` | ✅ PASS |

**Status**: ✅ All ACs covered, 1 minor spec-precision gap noted on AUT-05's empty-input send path (not a failure - the described behavior is implemented per code at `PhoneInput.tsx:72-75`, `onChange(digits ? ... : '')`, just not independently e2e-asserted).

### P2: Backend rejects malformed phone shape (AUT-07..AUT-09)

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AUT-07: non-empty `body.Phone` not matching `^\+[1-9]\d{7,14}$` → `400` + English error, store NOT called | Status `400`, `error: "invalid phone number"`, and the row's phone stays unchanged (empty) | `internal/dashboard/app_users_handler_test.go:259-277` - `TestUpdateAppUserHandler_InvalidPhone`: `w.Code != http.StatusBadRequest` check, `resp["error"] != "invalid phone number"`, then re-reads row and asserts `phone != ""` fails (i.e. confirms it stayed `""`, proving the store update didn't run) | ✅ PASS |
| AUT-08: empty `body.Phone` is valid (optional), proceeds unchanged | Status `200` for an empty phone | `internal/dashboard/app_users_handler_test.go:282-297` - `TestUpdateAppUserHandler_EmptyPhoneAllowed`: `w.Code != http.StatusOK` check on `{"phone":""}` | ✅ PASS |
| AUT-09: `body.Phone` matching pattern persists unchanged | Status `200`, and the stored `phone` column equals the exact input value `+5511987654321` | `internal/dashboard/app_users_handler_test.go:312-323` - `TestUpdateAppUserHandler_ValidE164PhonePersists`: `w.Code != http.StatusOK`, then `SELECT phone ...` and `phone != "+5511987654321"` | ✅ PASS |

**Status**: ✅ All ACs covered, precise spec outcomes matched exactly (status codes, exact error string, exact stored value).

**Overall spec-anchored check**: 9/9 ACs covered with evidence; 8/9 fully precise, 1 minor spec-precision gap (AUT-05 empty-input path, frontend-side only, not a functional failure).

---

## Discrimination Sensor

Ran in an isolated `git worktree` (`git worktree add /tmp/zeep-orbit-sensor HEAD`), never `git stash`. Real working tree confirmed clean (`git status --porcelain` empty) both before and after; `git worktree list` shows no lingering worktree after `git worktree remove --force`.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/dashboard/handler.go:2435` (scratch copy) | Flipped condition `!phoneE164Re.MatchString(body.Phone)` → `phoneE164Re.MatchString(body.Phone)` (inverts the validation, rejecting valid phones and accepting invalid ones) | ✅ Killed - `TestUpdateAppUserHandler_Success`, `TestUpdateAppUserHandler_UnchangedEmailKeepsSessions`, `TestUpdateAppUserHandler_InvalidPhone`, `TestUpdateAppUserHandler_ValidE164PhonePersists` all failed against the mutant (`go test ./internal/dashboard/... -run TestUpdateAppUserHandler`) |
| 2 | `internal/dashboard/ui/src/components/patterns/PhoneInput.tsx:72-75` (scratch copy) | Removed the dial-code prefix from `setNational`'s `onChange` payload (`onChange(digits ? \`${dialCodeFor(country)}${digits}\` : '')` → `onChange(digits ? digits : '')`), breaking AUT-05 | ✅ Killed - rebuilt frontend + Go binary in the scratch worktree, served on a scratch port (8099) against a scratch DB truncate, ran `npx playwright test e2e/app-users.spec.ts`: both phone-asserting tests failed (`edits email, phone, and role from the drawer` and `defaults phone country to Brazil...`), expected `+5511987654321` got `11987654321` |

**Sensor depth**: lightweight (2 targeted mutations, within the 1-3 default tier for a standard feature; not P0/critical-path)
**Result**: 2/2 killed - PASS ✅

---

## Interactive UAT Results

Not performed. This is a user-facing feature, but the caller's task scope was automated verification only (build gates, spec-anchored coverage, discrimination sensor). No interactive UAT session was requested or conducted in this pass.

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ - scope matches spec's 3 goals exactly (country-select+mask UI, E.164-ish persisted format, loose backend check) |
| No abstractions for single-use code | ✅ - `PhoneInput` is a single prop-driven component, no premature generalization |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ - diff touches exactly `PhoneInput.tsx`, `patterns/index.ts` (export), `AppUsersPage.tsx` (wiring), `handler.go` (validation), test files, `CHANGELOG.md`, `package.json`/`package-lock.json` (new dep) |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ - reuses existing shadcn `Select`, follows the prop-driven pattern cited in spec's Assumptions table (`AppMembersList`, `AppUsersTab`) |
| Tests map to acceptance criteria and are non-shallow | ✅ - spot-checked P2 story: each Go test asserts exact status code, exact error string, and follow-up DB read to confirm side-effect (not just "no error") |
| Spec-anchored outcome check (asserted values match spec) | ✅ - see AC table above; 8/9 exact match, 1 minor gap noted |
| Per-layer Coverage Expectation met | ✅ - backend: 1:1 AC mapping (AUT-07/08/09 each have a dedicated test); frontend/e2e: happy path + country-switch + round-trip + default covered |
| Every test in scope maps to a spec AC | ✅ - no unclaimed tests found; all new/modified tests reference AUT-0X in comments |
| Documented project quality/testing guidelines followed | ✅ - AGENTS.md §4 (API error strings in English: `"invalid phone number"` confirmed English) and §5 (i18n not applicable here since no new user-facing string was added beyond existing `appUsers.*` keys) |

---

## Edge Cases

- [x] Toolkit `countries` list has no entry for a given `dialCode` prefix → falls back to `BR`: code path exists (`PhoneInput.tsx:38-40`, `if (!best) return { country: DEFAULT_COUNTRY, ... }`); not independently tested (toolkit's real data always has entries), acceptable as defensive-only per spec's own framing ("never observed, but defensive").
- [x] Admin clears phone input entirely → sends `phone: ""` (not omitted): implemented at `PhoneInput.tsx:74` (`onChange(digits ? ... : '')`) and backend allows empty (`TestUpdateAppUserHandler_EmptyPhoneAllowed`). No direct e2e test clears an already-populated field and asserts the PUT body key is present with `""` (see AUT-05 gap above).
- [x] Two dial codes are prefixes of each other (`+1` vs `+1264`) → longest match wins: implemented at `PhoneInput.tsx:31-36`. Not directly exercised by a test using a colliding pair (tests use BR `+55` and Albania `+355`, which don't collide with another prefix in a currently-tested way). Logic reviewed by inspection; consistent with the mutation-1 test category, but no dedicated unit test targets this exact collision case.

---

## Gate Check

- **Gate commands** (per AGENTS.md §3, no `tasks.md` override present): `go build ./...`, `go vet ./...`, `gofmt -l <changed files>`, Go tests (`internal/dashboard/...`), `npx tsc -b`, `npm run build`, plus e2e (`app-users.spec.ts`, `enduser-roles.spec.ts`) per the caller's documented local test infra.
- **Result**:
  - `go build ./...` - pass
  - `go vet ./...` - pass
  - `gofmt -l internal/dashboard/handler.go internal/dashboard/app_users_handler_test.go` - clean (no output)
  - `go test ./internal/dashboard/...` (full package) - pass
  - `npx tsc -b` - pass
  - `npm run build` - pass
  - Playwright `e2e/app-users.spec.ts e2e/enduser-roles.spec.ts` - 9/9 passed
- **Test count before feature**: baseline (`be15200~1`) Go tests in this file: 8 (`Success`, `EmailChangeResetsSessions`, `UnchangedEmailKeepsSessions`, `InvalidEmail`, `InvalidRole`, `EmailConflict`, `Forbidden`, `UserNotFound`)
- **Test count after feature**: 11 (+3 new: `InvalidPhone`, `EmptyPhoneAllowed`, `ValidE164PhonePersists`)
- **Delta**: +3 Go tests; +1 new e2e test (`defaults phone country to Brazil...`), +1 existing e2e test updated in place (phone assertions), +2 existing e2e tests in `enduser-roles.spec.ts` updated (combobox selector disambiguation, not new coverage)
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

None required - PASS with only a minor spec-precision note (AUT-05 empty-input send path lacks a dedicated e2e assertion; behavior is implemented correctly by code inspection, and is indirectly covered by backend acceptance of empty phone).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| AUT-01 | Implementing | ✅ Verified |
| AUT-02 | Implementing | ✅ Verified |
| AUT-03 | Implementing | ✅ Verified |
| AUT-04 | Implementing | ✅ Verified |
| AUT-05 | Implementing | ✅ Verified (minor precision gap noted, not blocking) |
| AUT-06 | Implementing | ✅ Verified |
| AUT-07 | Implementing | ✅ Verified |
| AUT-08 | Implementing | ✅ Verified |
| AUT-09 | Implementing | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 9/9 ACs matched spec outcome (1 minor precision gap, non-blocking)
**Sensor**: 2/2 mutations killed
**Gate**: all build/test/lint gates passed, 9/9 e2e tests passed

**What works**: Country-aware phone entry (select + live mask), correct `BR`-first alphabetical ordering, longest-dial-code-prefix country detection on reopen, correct `+{dialCode}{digits}` persisted format, backend E.164-shape validation rejecting malformed non-empty phones with an English 400 while keeping empty phone optional and valid phones persisting unchanged.

**Issues found**: None blocking. Minor: no dedicated e2e test explicitly clears a populated phone field and asserts the resulting PUT body carries `phone: ""` (the key behavior is implemented and indirectly covered by the backend's `EmptyPhoneAllowed` test).

**Next steps**: None required to consider this feature done. Optional follow-up (not a gap, a suggestion): add one e2e assertion for the "clear phone field → PUT sends phone: ''" path if this surface changes again.
