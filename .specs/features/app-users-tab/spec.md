# App Users Tab Specification

## Problem Statement

App-user management lives on a standalone page (`/apps/:id/users`), reached only from the app card in the apps list. It sits outside the app detail screen's tab set (`database`, `auth`, `storage`, `api`, `tokens`, `members`, `observability`), forcing a full navigation away from the app's context to manage its users. Moving it into a tab keeps app-user management alongside the rest of the app's configuration, consistent with how `members` is already embedded.

## Goals

- [ ] App-user list, edit (email/phone/role), and activate/deactivate are reachable as a tab inside `AppDetailsPage`, with no loss of existing functionality.
- [ ] The standalone route and its own header/back-link are removed — one place to maintain, one way to reach it.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Redirect from old `/apps/:id/users` route | User explicitly chose no redirect — direct links now 404, acceptable since the route was only ever reached via in-app navigation (the app card), never shared externally. |
| Changes to app-user data/behavior (fields, validation, endpoints) | Pure navigation/composition change — `app-user-edit-fields` feature already covers field behavior. |
| Changes to `members` tab or any other existing tab | Out of scope; only inserting the new tab and its dependencies. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| New tab id/query value | `users` (matches `?tab=users`) | Consistent with existing tab id scheme (`database`, `auth`, ...) which doubles as the `tab` query param value | y |
| Component extraction shape | Rename inline component in `AppUsersPage.tsx` to `AppUsersTab({ appId }: { appId: string })`, drop `useParams`/`PageHeader`/back-link, keep it in the same file (no new file) unless the file becomes unwieldy | Mirrors existing single-file-per-page convention; `AppMembersList` is a separate file only because it's shared across two contexts (backend/`axis` prop) — `AppUsersTab` has one consumer | y |
| Tab position | Immediately after `auth`, before `storage` | User's explicit choice — groups app-user auth config next to app-user management | y |
| i18n key | `appDetails.tabUsers` | Matches existing `appDetails.tabX` naming for all other tabs | y |
| e2e adjustment | Update existing specs (`app-users.spec.ts`, `enduser-roles.spec.ts`) to navigate via `?tab=users` / click the tab trigger instead of the removed route | Direct consequence of route removal — same pattern as the regression fix already done for the `EditUserDrawer` rename in a prior session | y |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: App user management as a tab ⭐ MVP

**User Story**: As a dashboard admin, I want to manage an app's users from a tab on the app detail screen, so that I don't have to leave the app's context to edit a user's role, email, phone, or active status.

**Why P1**: This is the entire feature — there is no smaller demoable slice.

**Acceptance Criteria**:

1. WHEN an admin opens `/apps/:id?tab=users` THEN the system SHALL render the app-user table (email, name, role, status, last sign-in) for that app, identical in content to the current standalone page.
2. WHEN an admin clicks the "Users" tab trigger on the app detail screen THEN the system SHALL update the URL to `?tab=users` and render the app-user table without a full page navigation.
3. WHEN an admin clicks "Edit" on a user row inside the tab THEN the system SHALL open the existing edit drawer (email/phone/role) and save via the existing `PUT /apps/{id}/users/{userId}` endpoint, unchanged.
4. WHEN an admin clicks Activate/Deactivate on a user row inside the tab THEN the system SHALL perform the existing activate/deactivate mutation, unchanged.
5. WHEN the app card's "Users" action is triggered from the apps list THEN the system SHALL navigate to `/apps/:id?tab=users` instead of the removed `/apps/:id/users` route.
6. The system SHALL NOT render a page-level header or "back" link inside the tab content (the app detail screen already provides both).

**Independent Test**: From the apps list, click a card's "Users" action; land on the app detail screen with the Users tab active; edit a user's email and toggle their status; confirm both operations succeed exactly as they did on the old standalone page.

---

### P2: Old route removed cleanly

**User Story**: As a maintainer, I want the standalone `/apps/:id/users` route deleted along with its now-dead code, so that there is exactly one implementation to maintain.

**Why P2**: Cleanup that follows directly from P1's move, not independently valuable to an end user, but required to avoid duplicated logic.

**Acceptance Criteria**:

1. IF a browser navigates directly to `/apps/:id/users` THEN the system SHALL return the app's normal not-found/404 behavior (no route matches).
2. The system SHALL NOT contain a `PageHeader` or standalone back-link for app-users after the change (dead code removed, not left disabled).
3. WHEN existing e2e specs that referenced the old route run THEN the system SHALL pass using the new tab-based navigation, with no reduction in assertion coverage.

**Independent Test**: Run the full frontend e2e suite; confirm `app-users.spec.ts` and `enduser-roles.spec.ts` pass unchanged in assertion count, and confirm `App.tsx` no longer declares the `/apps/:id/users` route.

---

## Edge Cases

- IF the app has zero users THEN the system SHALL show the existing empty state inside the tab (unchanged from today).
- IF `AppUsersTab` is rendered before `useApp`/`useAppUsers` data resolves THEN the system SHALL show the existing loading state (unchanged from today).
- WHEN switching from the Users tab to another tab and back THEN the system SHALL preserve tab-switch behavior identical to the other tabs (state reset on remount, same as `members` today — no new caching requirement).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AUT-01 | P1 | Execute | Implemented |
| AUT-02 | P1 | Execute | Implemented |
| AUT-03 | P1 | Execute | Implemented |
| AUT-04 | P1 | Execute | Implemented |
| AUT-05 | P1 | Execute | Implemented |
| AUT-06 | P1 | Execute | Implemented |
| AUT-07 | P2 | Execute | Implemented |
| AUT-08 | P2 | Execute | Implemented |
| AUT-09 | P2 | Execute | Implemented |

**Coverage:** 9 total, 9 mapped to Execute (Medium scope — no formal tasks.md), 0 unmapped.

---

## Success Criteria

- [ ] Users tab renders and fully replaces the standalone page's functionality (list, edit, activate/deactivate).
- [ ] `/apps/:id/users` route, its handler component, `PageHeader`, and back-link are deleted — zero dead code left behind.
- [ ] `go build ./...` (n/a, frontend-only) — `npx tsc -b` and `npm run build` clean in `internal/dashboard/ui`.
- [ ] All pre-existing and updated e2e specs pass.
