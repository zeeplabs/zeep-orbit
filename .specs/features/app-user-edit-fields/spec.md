# App User Edit Fields Specification

## Problem Statement

The app-user edit drawer (`EditRoleDrawer`, `AppUsersPage.tsx`) only lets an admin change `role`. Support needs to correct an end-user's email or phone (typos at registration, phone porting, etc.) without a database console. Admins can currently only deactivate/reactivate the account or reset its sessions — never fix the contact fields.

## Goals

- [ ] Admin can edit an app user's `email`, `phone`, and `role` from a single drawer in the Dashboard.
- [ ] Changing the email invalidates the user's existing sessions and email-confirmation state, so a corrected/hijacked account can't be used with stale trust.
- [ ] Email uniqueness violations surface as a clear, actionable error instead of a generic 500.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Editing `active`/status from the drawer | Stays a separate table-row toggle (existing Activate/Deactivate actions with their own session-reset side effect) — explicit user decision, not unified into this drawer. |
| Editing `name`, `avatar_url` | Not requested; no reported pain point for these fields today. |
| Phone format validation | No format validation exists anywhere in the project today (confirmed in `internal/auth/handler.go` registration path); introducing one here would be a new, undiscussed product rule. |
| Re-sending an email verification message on change | No email-sending mechanism for app users exists in this codebase today; out of scope for this change. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Endpoint shape | New `PUT /dashboard/api/apps/{id}/users/{userId}` replaces `PUT .../role`; body `{email, phone, role}`, all three keys always sent (merge-on-absent-key would otherwise make "clear phone" indistinguishable from "leave role's key omitted") | Matches AGENTS.md §5's rule that a form which can clear a field must always send that field's key explicitly | y |
| Email change side effects | Sets `email_confirmed_at = NULL` and deletes all sessions (`ResetAppUserSessions`) when the new email differs (case-insensitive, post-normalization) from the current one | An email change is a credential change; stale confirmation/session state for the old address is a security liability | y |
| Email conflict handling | Backend relies on the existing `UNIQUE` constraint on `_auth_users.email`; a Postgres `23505` violation is translated to `409 {"error": "email already in use"}` | Avoids a redundant pre-check `SELECT` race; the constraint is already authoritative | y |
| Email format validation | Reuse `isValidEmail`/`normalizeEmail` (`internal/dashboard/handler.go:64-71`) | Existing helper, already used for other dashboard-authored email fields; avoids a second validation rule | y |
| Role validation | Reuse existing `identRe` (`^[a-z][a-z0-9_]{0,62}$`) | Unchanged from current `/role` endpoint | y |
| Audit action name | `app.user.update` (replaces `app.user.role_update` for this endpoint) | Endpoint now covers more than role; keeps one audit action per endpoint, consistent with existing `app.user.deactivate`/`app.user.activate` naming | n (repo convention, not user-confirmed; low-risk naming choice) |
| Phone field | Free-text, trimmed, no format/uniqueness validation; empty string clears it to `NULL` | Matches current registration behavior; introducing format validation is out of scope (see Out of Scope) | y |
| Concurrency | No optimistic locking; last write wins, consistent with existing `UpdateAppUserRole`/`DeactivateAppUser` | Single-admin-at-a-time editing is the existing assumption across all app-user mutations; no report of concurrent-edit conflicts | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Edit email and phone from the existing role drawer ⭐ MVP

**User Story**: As a dashboard admin with manage access to an app, I want to correct an end-user's email and phone alongside their role, so that I can fix registration mistakes without touching the database directly.

**Why P1**: This is the entire feature — a single vertical slice (UI + endpoint + store) with no meaningful sub-slice.

**Acceptance Criteria**:

1. WHEN an admin with `CanManage()` role opens the edit drawer for an app user THEN the system SHALL pre-fill Email, Phone, and Role with the user's current values.
2. WHEN an admin submits the drawer with a changed email, phone, and/or role THEN the system SHALL send `PUT /dashboard/api/apps/{id}/users/{userId}` with `{"email": <string>, "phone": <string>, "role": <string>}`, all three keys present.
3. WHEN the endpoint receives a valid, non-conflicting email, any phone string, and a role matching `identRe` THEN the system SHALL update `email`, `phone`, and `role` on the matching `_auth_users` row and return `200 {"message": "user updated"}`.
4. IF the submitted email differs from the stored email (after `normalizeEmail`) THEN the system SHALL set `email_confirmed_at = NULL` and delete all of that user's rows in `_auth_sessions` as part of the same update.
5. IF the submitted email is identical to the stored email THEN the system SHALL NOT modify `email_confirmed_at` or delete sessions.
6. The system SHALL record an audit entry `app.user.update` with the app id and `{appName}/{userId}` target on every successful update.

**Independent Test**: Open the drawer for a seeded app user, change the email to a new unused address, save, and confirm (a) the row shows the new email, (b) `email_confirmed_at` is `NULL` in the DB, (c) the user's existing session is rejected on next request, (d) an `app.user.update` audit row exists.

---

### P2: Reject invalid or conflicting input with actionable errors

**User Story**: As a dashboard admin, I want a clear error when my edit is invalid, so that I know what to fix instead of seeing a generic failure.

**Why P2**: Correctness safeguard, not required for the core happy-path demo, but required before shipping to avoid silent data corruption or confusing 500s.

**Acceptance Criteria**:

1. IF the submitted email fails `isValidEmail` THEN the system SHALL respond `400 {"error": "invalid email"}` and SHALL NOT modify the row.
2. IF the submitted role fails `identRe` THEN the system SHALL respond `400 {"error": "role must match ^[a-z][a-z0-9_]{0,62}$"}` and SHALL NOT modify the row.
3. IF the update violates the `_auth_users.email` UNIQUE constraint (Postgres code `23505`) THEN the system SHALL respond `409 {"error": "email already in use"}` and SHALL NOT modify the row.
4. IF the caller's role does not satisfy `CanManage()` THEN the system SHALL respond `403 {"error": "forbidden"}`.
5. IF the target app or user id does not exist THEN the system SHALL respond `404 {"error": "user not found"}` (or `"app not found"` for a missing app).
6. WHEN any of the above errors is returned THEN the dashboard SHALL show it via `toast.error(error.message)` without translation.

**Independent Test**: Submit the drawer with a malformed email, an out-of-pattern role, and an email already used by another user in the same app (three separate attempts); confirm each yields the specific status/message above and the row is unchanged in the DB.

---

## Edge Cases

- IF `phone` is submitted as an empty string THEN the system SHALL store it as `NULL` (clearing it), consistent with the "always send the key" contract.
- IF only `role` changes (email/phone resubmitted unchanged) THEN the system SHALL NOT reset `email_confirmed_at` or sessions (covered by AC P1-5).
- WHEN the request body exceeds the existing `4*1024`-byte cap (`http.MaxBytesReader`, same limit as the current `/role` endpoint) THEN the system SHALL reject it via the existing `decodeJSONBody` behavior.
- The old route `PUT /dashboard/api/apps/{id}/users/{userId}/role` and `UpdateAppUserRole` handler/store function are removed; no backward-compatibility shim is kept (AGENTS.md: no compat shims — change the callers instead), since the only caller is the first-party dashboard frontend shipped in the same change.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AUE-01 | P1: Edit email/phone/role | Design | Pending |
| AUE-01 | P1: Edit email/phone/role | Verified | Verified by e2e (T5/T7) |
| AUE-02 | P1: Edit email/phone/role | Verified | Verified by e2e (T4/T7) |
| AUE-03 | P1: Edit email/phone/role | Verified | Verified (T1/T2 unit) |
| AUE-04 | P1: Edit email/phone/role | Verified | Verified (T1/T2 unit) |
| AUE-05 | P1: Edit email/phone/role | Verified | Verified (T1/T2 unit) |
| AUE-06 | P1: Edit email/phone/role | Verified | Verified (T2 unit) |
| AUE-07 | P2: Error handling | Verified | Verified (T2 unit) |
| AUE-08 | P2: Error handling | Verified | Verified (T2 unit) |
| AUE-09 | P2: Error handling | Verified | Verified (T1/T2 unit + T7 e2e) |
| AUE-10 | P2: Error handling | Verified | Verified (T2 unit) |
| AUE-11 | P2: Error handling | Verified | Verified (T2 unit) |
| AUE-12 | P2: Error handling | Verified | Verified by e2e (T5/T7) |

**ID format:** `AUE-NN`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 12 total, 12 mapped to tasks, 0 unmapped, all 12 Verified (round 2 PASS). See `validation.md` for both Verifier rounds.

---

## Success Criteria

- [ ] Admin can change an app user's email, phone, and role from one drawer, in one request.
- [ ] Email change forces re-authentication (sessions cleared, confirmation reset) with zero exceptions.
- [ ] Every rejected edit (invalid email, invalid role, email conflict) returns a specific, translatable-by-toast message — never a raw 500.
- [ ] `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, `npm run build` all pass (AGENTS.md §3).
