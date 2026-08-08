# App User Edit Fields Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/app-user-edit-fields/design.md`
**Status**: In Progress (Phase 1 done, Phase 2 pending)

---

## Test Coverage Matrix

> Generated from codebase and spec — confirm before Execute. Guidelines found: `AGENTS.md` §3 (build/test gates), `internal/dashboard/app_users_store_test.go` + `internal/dashboard/apps_handler_test.go` (Go test conventions), `internal/dashboard/ui/e2e/enduser-roles.spec.ts` (Playwright convention for this same feature area).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Store (`app_users_store.go`) | unit (integration-style, real Postgres) | Every branch of AC AUE-03/04/05 + conflict (AUE-09) + not-found; skips via `t.Skip` if `TEST_DATABASE_URL` unset (existing convention) | `internal/dashboard/app_users_store_test.go` | `TEST_DATABASE_URL=... go test ./internal/dashboard/...` |
| Handler (`handler.go` route) | unit (httptest, no real DB required beyond store test coverage) | Happy path + all AUE-07..11 error paths (400 x2, 403, 404, 409) | `internal/dashboard/apps_handler_test.go` or a new `app_users_handler_test.go` alongside it | `go test ./internal/dashboard/...` |
| Frontend drawer + e2e | e2e (Playwright) | Happy path (edit email+phone+role, verify persisted) + one error path (duplicate email → toast) | `internal/dashboard/ui/e2e/app-users.spec.ts` (new, sibling of `enduser-roles.spec.ts`) | `npm run test:e2e` (in `internal/dashboard/ui`) |
| Types/i18n only | none | build gate only | - | `npx tsc -b && npm run build` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After Go-only tasks (store/handler) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./internal/dashboard/...` |
| Full | After frontend tasks (drawer, hook, e2e) | `npx tsc -b && npm run build` (in `internal/dashboard/ui`) `&&` `npm run test:e2e` |
| Build | Phase completion (end of Phase 1 and end of Phase 2) | Full backend gate + full frontend gate together |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend

```
T1 → T2 → T3
```

### Phase 2: Frontend

```
T3 → T4 → T5 → T6 → T7
```

---

## Task Breakdown

### T1: Replace `UpdateAppUserRole` with `store.UpdateAppUser` — ✅ Complete

> SPEC_DEVIATION: T1, T2, and T3 were implemented and committed together in a single commit, not three. Go requires the store function rename and its only caller (the handler) to change atomically for the package to compile — there is no buildable intermediate state between "old symbol only" and "new symbol only" in the same package. Splitting them would have meant committing code that fails `go build`, violating the gate-before-commit rule. All three tasks' "Done when" criteria are satisfied; see the single commit covering this phase.


**What**: Remove `UpdateAppUserRole`. Add `UpdateAppUser(ctx, pool, schema, userID, email, phone, role string) (emailChanged bool, err error)`: `SELECT email FROM _auth_users WHERE id=$1` (return `ErrNotFound` if 0 rows); compare `normalizeEmail(current)` vs `normalizeEmail(email)`; if different, `UPDATE ... SET email=$1, phone=$2, role=$3, email_confirmed_at=NULL, updated_at=now() WHERE id=$4`, else same `UPDATE` without touching `email_confirmed_at`; map Postgres `23505` to a new sentinel `var ErrEmailConflict = errors.New("email already in use")`; keep the existing `isPgRelationNotFound` → `ErrNotFound` mapping.
**Where**: `internal/dashboard/app_users_store.go`
**Depends on**: None
**Reuses**: `isPgRelationNotFound` (existing helper in same file), `normalizeEmail` (`internal/dashboard/handler.go`)
**Requirement**: AUE-03, AUE-04, AUE-05, AUE-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `UpdateAppUserRole` removed, `UpdateAppUser` added with the signature above
- [ ] `ErrEmailConflict` sentinel defined and returned on Postgres `23505`
- [ ] `email_confirmed_at` reset only when normalized email actually changes
- [ ] Unit tests pass (see Tests below)

**Tests**: unit (real Postgres, existing `appUsersTestPool` helper) in `app_users_store_test.go`: (a) update all three fields, email unchanged → `emailChanged=false`, `email_confirmed_at` untouched; (b) email changed → `emailChanged=true`, `email_confirmed_at` becomes `NULL`; (c) email collides with another seeded user → `ErrEmailConflict`; (d) unknown `userID` → `ErrNotFound`.
**Gate**: quick

---

### T2: Add `Handler.UpdateAppUser` and wire the route — ✅ Complete

**What**: New handler on `PUT /dashboard/api/apps/{id}/users/{userId}`: auth via `GetApp` + `role.CanManage()` (403 if not); `h.decodeJSONBody` with `http.MaxBytesReader(w, r.Body, 4*1024)` into `{Email, Phone, Role string}`; `normalizeEmail` + `isValidEmail` → 400 `{"error": "invalid email"}`; `identRe.MatchString(Role)` → 400 `{"error": "role must match ^[a-z][a-z0-9_]{0,62}$"}`; call `store.UpdateAppUser`; on `ErrNotFound` → 404; on `ErrEmailConflict` → 409 `{"error": "email already in use"}`; on other error → `h.writeError(..., "failed to update user", err)`; if `emailChanged`, call `ResetAppUserSessions` (log-and-continue on error, do not fail the request); on success, `writeJSON(200, {"message": "user updated"})` + `h.audit(..., "app.user.update", "app_user", appID, app.Name+"/"+userID, nil, r.RemoteAddr)`.
**Where**: `internal/dashboard/handler.go` (handler) + router registration file where the `/role` route is currently mounted
**Depends on**: T1
**Reuses**: `GetApp`, `role.CanManage()`, `h.decodeJSONBody`, `isValidEmail`/`normalizeEmail`, `identRe`, `ResetAppUserSessions`, `h.audit` (all existing)
**Requirement**: AUE-02, AUE-03, AUE-04, AUE-05, AUE-06, AUE-07, AUE-08, AUE-09, AUE-10, AUE-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Route `PUT /dashboard/api/apps/{id}/users/{userId}` registered and calling the new handler
- [ ] All 5 error branches return the exact status/body from the spec
- [ ] Audit entry `app.user.update` recorded on success
- [ ] Unit tests pass (see Tests below)

**Tests**: unit (httptest) covering happy path + all 5 error branches (AUE-07, AUE-08, AUE-09, AUE-10, AUE-11), following the pattern in `apps_handler_test.go`.
**Gate**: quick

---

### T3: Remove the old `/role` endpoint — ✅ Complete

**What**: Delete `UpdateAppUserRole` handler and its `/role` route registration. Confirm no other caller references `PUT .../role` (grep for `"/role"` and `UpdateAppUserRole` across the repo, including `internal/dashboard/ui` and any SDK/docs) before deleting.
**Where**: `internal/dashboard/handler.go`, router registration file
**Depends on**: T2
**Reuses**: N/A (deletion)
**Requirement**: N/A (cleanup, no new AC)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `UpdateAppUserRole` handler and `/role` route deleted
- [ ] `go build ./...` has no dangling references
- [ ] Grep confirms no remaining caller of the old route

**Tests**: none (deletion); covered by Quick gate (`go build`/`go vet` catching any dangling reference).
**Gate**: quick

---

### T4: Add `useUpdateAppUser`, remove `useUpdateAppUserRole`

**What**: Replace `useUpdateAppUserRole` with `useUpdateAppUser`: same `useMutation` shape, `PUT /dashboard/api/apps/${appId}/users/${userId}` with body `{ email, phone, role }`; invalidates the same query key(s) the old hook did.
**Where**: `internal/dashboard/ui/src/lib/api.ts`
**Depends on**: T3
**Reuses**: existing `useMutation`/query-client wiring pattern in the same file
**Requirement**: AUE-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `useUpdateAppUser` exported with the signature above
- [ ] `useUpdateAppUserRole` removed
- [ ] `npx tsc -b` passes

**Tests**: none (thin hook; exercised via T7 e2e)
**Gate**: quick

---

### T5: Rename/expand `EditRoleDrawer` → `EditUserDrawer`

**What**: Rename component; add `email`/`phone` local state initialized from `user.email`/`user.phone ?? ''`; add two `Input` fields (email required, phone optional) above the existing Role `Select`, same `flex flex-col gap-1.5`/`Label` pattern already used; `save()` calls `useUpdateAppUser().mutate({ appId, userId: user.id, email, phone, role }, { onSuccess: onClose, onError: (e) => toast.error(e.message) })`; update the "Editar" action button's drawer trigger to the renamed component.
**Where**: `internal/dashboard/ui/src/pages/AppUsersPage.tsx`
**Depends on**: T4
**Reuses**: existing `Drawer`/`DrawerContent`/`Label`/`Select` primitives, `sonner` `toast`
**Requirement**: AUE-01, AUE-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Drawer shows pre-filled Email, Phone, Role
- [ ] Save sends all three fields, always including `phone` even when empty
- [ ] `onError` shows `toast.error(error.message)`
- [ ] `npx tsc -b` passes

**Tests**: none directly (covered by e2e in T7)
**Gate**: quick

---

### T6: Add i18n strings

**What**: Add/rename keys for the drawer title, email label, phone label (keep existing `editRoleLabel`/`editRoleCancel`/`editRoleSave` keys if still applicable, or rename to `editUser*` consistently across both files in the same change).
**Where**: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T5
**Reuses**: existing i18n key structure for `appUsers.*`
**Requirement**: N/A (i18n compliance, AGENTS.md §5)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Both locale files have matching keys for every new string
- [ ] No hardcoded string left in `EditUserDrawer`
- [ ] `python3 -c "import json; json.load(open('en.json'))"` and same for `pt-BR.json` pass

**Tests**: none — validated with the JSON-load check above, per AGENTS.md §3.
**Gate**: quick

---

### T7: Playwright e2e coverage

**What**: Two tests: (1) happy path — open drawer for a seeded app user, change email + phone + role, save, assert the table row reflects new values; (2) error path — attempt to change email to one already used by a second seeded user in the same app, assert a toast with "email already in use" appears and the row is unchanged. Follow `enduser-roles.spec.ts`'s setup/helpers conventions (`helpers.ts`).
**Where**: `internal/dashboard/ui/e2e/app-users.spec.ts` (new)
**Depends on**: T6
**Reuses**: `internal/dashboard/ui/e2e/helpers.ts`, seeding conventions from `enduser-roles.spec.ts`
**Requirement**: AUE-01, AUE-02, AUE-09, AUE-12

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Both scenarios pass under `npm run test:e2e`
- [ ] No flaky waits (follow existing helpers' patterns)

**Tests**: this task IS the test task (e2e layer)
**Gate**: full

---

## Requirement Traceability Update

| Requirement ID | Task(s) |
| --- | --- |
| AUE-01 | T5 |
| AUE-02 | T5, T4 |
| AUE-03 | T1, T2 |
| AUE-04 | T1, T2 |
| AUE-05 | T1, T2 |
| AUE-06 | T2 |
| AUE-07 | T2 |
| AUE-08 | T2 |
| AUE-09 | T1, T2 |
| AUE-10 | T2 |
| AUE-11 | T2 |
| AUE-12 | T5 |

**Coverage**: 12 total, 12 mapped to tasks, 0 unmapped.
