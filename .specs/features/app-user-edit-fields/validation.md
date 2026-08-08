# Validation Report — app-user-edit-fields

**Diff range**: `f228cf7..e68eec5` (`f228cf7` feat(dashboard): allow editing app user email and phone; `e68eec5` feat(dashboard-ui): edit app user email and phone from the drawer)

**Verdict: FAIL** (gates and mutation sensor pass; 4 of 12 acceptance criteria have no test evidence, despite spec.md's traceability table marking all 12 "Verified")

---

## 1. Per-AC evidence table

| AC | Expected outcome | Evidence (file:line) | Verdict |
|---|---|---|---|
| AUE-01 | Drawer pre-fills Email, Phone, Role | `AppUsersPage.tsx` `useState(user.email)`/`useState(user.phone ?? '')`/`useState(user.role)` (code only). Role-only prefill asserted for the orphan case at `enduser-roles.spec.ts:188`, but that assertion is pre-existing (predates this feature) and covers Role only. | **NOT COVERED** — no `toHaveValue`/equivalent assertion anywhere for Email or Phone prefill. |
| AUE-02 | Frontend sends `{email,phone,role}` with all 3 keys always present | `api.ts:436` `body: JSON.stringify({ email, phone, role })` (code only). `enduser-roles.spec.ts:157` intercepts PUT by URL only, never inspects body. `app-users.spec.ts` never intercepts/asserts the request body. | **NOT COVERED** — no test asserts the request body shape. |
| AUE-03 | Valid update → 200 `{"message":"user updated"}`, row updated | `app_users_handler_test.go:87-104` `TestUpdateAppUserHandler_Success` — asserts `w.Code==200`, `resp["message"]=="user updated"`, then re-reads row and checks email/phone/role match. | **PASS** |
| AUE-04 | Email change → `email_confirmed_at=NULL` **and** sessions deleted | `app_users_store_test.go:118-136` `TestUpdateAppUserEmailChangeResetsConfirmation` asserts `emailChanged==true` and `confirmedAt==nil`. Session deletion (`ResetAppUserSessions`, called in `handler.go:2452` when `emailChanged`) has **zero** test references — `grep "_auth_sessions\|ResetAppUserSessions"` across both test files returns nothing. | **PARTIALLY COVERED** — confirmation-reset half proven, session-deletion half NOT COVERED. |
| AUE-05 | Same email → no reset | `app_users_store_test.go:89-95` `TestUpdateAppUserChangesRoleFromDefault` asserts `emailChanged==false` when email resubmitted unchanged. | **PASS** (gates the reset call in handler; test doesn't re-check `email_confirmed_at` value but the boolean gate is the mechanism under test). |
| AUE-06 | Audit entry `app.user.update` recorded on success | `app_users_handler_test.go:74` comment claims "plus an audit entry" but the test body (lines 75-105) never queries an audit table/log. `grep "audit"` in the handler test file matches only that comment. | **NOT COVERED** — no assertion exists despite the doc comment implying one. |
| AUE-07 | Invalid email → 400 `"invalid email"`, row untouched | `app_users_handler_test.go:119-129` `TestUpdateAppUserHandler_InvalidEmail` asserts status+message. No re-read of the row, but validation runs before any DB write (code-verified). | **PASS** (status/message proven; "row untouched" inferred from code path, not directly queried). |
| AUE-08 | Invalid role → 400 with `identRe` message, row untouched | `app_users_handler_test.go:144-154` `TestUpdateAppUserHandler_InvalidRole`. Same row-untouched caveat as AUE-07. | **PASS** |
| AUE-09 | Email conflict → 409 `"email already in use"`, row untouched | `app_users_handler_test.go:170-188` `TestUpdateAppUserHandler_EmailConflict` (status, message, and explicit re-read confirming email unchanged) + `app_users_store_test.go:145-156` `TestUpdateAppUserEmailConflictReturnsErrEmailConflict` (store-level, same checks). | **PASS** |
| AUE-10 | Non-`CanManage()` actor → 403 | `app_users_handler_test.go:205-215` `TestUpdateAppUserHandler_Forbidden`. | **PASS** |
| AUE-11 | Unknown user/app → 404 `"user not found"` | `app_users_handler_test.go:229-239` `TestUpdateAppUserHandler_UserNotFound` + `app_users_store_test.go:162-165` `TestUpdateAppUserUnknownUserReturnsNotFound`. (The `"app not found"` alternate path is not separately tested, but spec phrases it as an "or" alternative.) | **PASS** |
| AUE-12 | Errors surfaced via `toast.error(error.message)`, untranslated | `api.ts:441-443` `onError: (error) => toast.error(error.message)` + `app-users.spec.ts:98` `expect(page.getByText('email already in use')).toBeVisible()`. | **PASS** |

**Score: 8/12 PASS, 1/12 partially covered, 3/12 not covered** (AUE-01, AUE-02, AUE-06 fully uncovered; AUE-04's session-deletion half uncovered). spec.md's Requirement Traceability table (lines 94-106) marks all 12 "Verified" — that claim is inaccurate for AUE-01, AUE-02, AUE-04, AUE-06.

## 2. enduser-roles.spec.ts regression check

Diff (`HEAD~2..HEAD`) only touches selectors/copy (`[title="Edit role"]`→`[title="Edit user"]`, `text=Edit role`→`text=Edit user`) and the direct API call's URL/body (`/role` endpoint → new `PUT .../users/{id}` with all 3 keys). The underlying assertions (ROLECFG-09/10/11/12/13/14: no inline inputs, cancel fires no PUT, orphan role shown pre-selected, switching role updates the table) are unchanged in substance — not weakened. Confirmed by reading the full diff hunk and the surrounding unchanged test body.

## 3. Gate results

**Backend** (`TEST_DATABASE_URL="postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable"`, Postgres confirmed running via `docker ps` → `zeep-orbit-db-1 0.0.0.0:5434->5432/tcp`):
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l` on the 5 changed files — no output (clean)
- `go test ./internal/dashboard/...` — **PASS**, 12 tests in scope all green (`TestUpdateAppUserHandler_Success/InvalidEmail/InvalidRole/EmailConflict/Forbidden/UserNotFound`, `TestUpdateAppUserChangesRoleFromDefault/EmailChangeResetsConfirmation/EmailConflictReturnsErrEmailConflict/UnknownUserReturnsNotFound`, `TestListAppUsersIncludesRole`, `TestCountAppUsersByRole`), full package: `ok  internal/dashboard  2.579s`

**Frontend** (`internal/dashboard/ui`):
- `npx tsc -b` — clean, no errors
- `npm run build` — succeeded (`✓ built in 1.16s`); only pre-existing chunk-size warning, unrelated to this change

Both gates: **PASS**, no non-zero exits.

## 4. Discrimination sensor

- Isolated worktree: `git worktree add /tmp/au-sensor-scratch HEAD`
- Mutation: in `internal/dashboard/app_users_store.go:180`, flipped `emailChanged = normalizeEmail(currentEmail) != normalizeEmail(email)` → `== ` (misreports a real email change as unchanged, and vice versa).
- Ran `TEST_DATABASE_URL=... go test ./internal/dashboard/... -run TestUpdateAppUser -v` against the mutant (after copying the pre-built `internal/dashboard/static` into the worktree to satisfy the `embed.go` directive).
- Result: **mutant killed** — `TestUpdateAppUserChangesRoleFromDefault` and `TestUpdateAppUserEmailChangeResetsConfirmation` both failed with the expected assertion messages (`expected emailChanged=false...`, `expected emailChanged=true...`).
- Cleanup: `git worktree remove --force /tmp/au-sensor-scratch`; `git status --porcelain` in the real repo is empty before and after — no leakage.

## 5. Ranked gaps

1. **AUE-06 (audit entry)** — no test queries the audit log/table after a successful update; the handler test's own comment claims coverage it doesn't have.
2. **AUE-04 (session deletion)** — `ResetAppUserSessions` is invoked on email change but no test ever seeds a session row and confirms it's deleted; only the `email_confirmed_at` reset half is proven.
3. **AUE-01 (Email/Phone prefill)** — no `toHaveValue`-style e2e assertion confirms the drawer opens with the user's actual email/phone; only Role prefill (pre-existing) is checked.
4. **AUE-02 (request body always sends all 3 keys)** — no e2e intercepts the frontend's own PUT body to confirm `email`/`phone`/`role` are always present, including when only one field changes.
