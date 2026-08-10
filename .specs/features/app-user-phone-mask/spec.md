# App User Phone Country Mask Specification

## Problem Statement

`EditUserDrawer` (`internal/dashboard/ui/src/pages/AppUsersPage.tsx:93-96`) edits an app end-user's phone as a free-text `<Input>` with no mask and no format guarantee — any string is accepted and sent as-is to `UpdateAppUser`. The system is Brazil-only in market but individual app end-users can legitimately have a phone number from any country, so a Brazil-only mask isn't sufficient and a free-text field isn't acceptable either. `@zeeptech/toolkit` (published npm package, v1.0.1) ships a country-aware mask (`globalCellphoneMask`), a country data table (`countries`, code+dialCode+mask+flag), and a mask-stripping helper (`clearMask`) that together solve this without a new dependency written in-house.

## Goals

- [ ] Replace the free-text phone `<Input>` in `EditUserDrawer` with a country-select + masked-input pair, so an admin can only enter a phone number in the shape the selected country expects.
- [ ] Persist phone in a country-recoverable format (`+{dialCode}{digits}`) using the existing `phone` text column — no schema migration.
- [ ] Add a loose server-side format check (`UpdateAppUser` handler) so the API rejects a malformed phone even if a caller bypasses the UI.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Superadmin bootstrap onboarding phone field (`OnboardingPage.tsx`) | The superadmin table has no `phone` column today; adding one needs a schema migration — separate feature, explicitly deferred by the user. |
| A "create app user" flow in the dashboard | Doesn't exist today — app end-users are created only via the app's own public signup endpoint (`internal/auth/handler.go`), which is out of scope for this dashboard-only change. |
| CPF/CNPJ/CEP masks or validators from the toolkit | Confirmed no CPF/CNPJ/CEP field exists anywhere in the orbit dashboard today. |
| Email/name validation via the toolkit | Go backend already has its own `isValidEmail`/`normalizeEmail`/`normalizeName` (`internal/dashboard/handler.go`, `internal/auth/handler.go`); the toolkit is a JS/TS-only package and cannot be used from Go. |
| Stronger password policy via `passwordStrongValidator` | Current password check is length-only (8 chars); tightening this is a separate security-policy decision, not part of this ask. |
| Migrating/backfilling existing `phone` values already stored without a `+` dial-code prefix | No bulk migration of historical data; existing rows are handled per AUT-08 below (best-effort parse, not required to be perfect). |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Country select option label | `{flag} {en name} ({dialCode})`, e.g. "🇧🇷 Brazil (+55)" | Toolkit's `countries` array has no `pt` field (only `en`/`ru`/`lt`/`tr`) — using `en` avoids inventing translations for 235 country names; flag+dialCode keeps it scannable without needing the name. | y |
| Country list ordering | `BR` pinned first, remaining 234 alphabetical by `en` name | Starbem's dashboard admins are Brazil-based; BR is the overwhelmingly common case. | y |
| Country select widget | Existing shadcn/Radix `<Select>` (already imported in this file) | Radix `Select` has built-in typeahead (typing jumps to a matching item), which is enough for a 235-item list without building a new searchable combobox component. | y |
| Stored phone format | `+{dialCode}{national digits}`, no separators (e.g. `+5511987654321`) | Round-trips cleanly: the leading `+{dialCode}` lets the drawer re-derive the country on next edit; digits-only avoids ambiguity from re-masking a value that already has punctuation. | y |
| Country detection on drawer open | Match `user.phone`'s prefix against the **longest** matching `dialCode` in `countries` (longest match wins, since some dial codes are prefixes of others, e.g. `+1` vs `+1264`); if no match or `phone` is empty, default to `BR` | Deterministic, no ambiguity from shorter/longer dial-code collisions; BR default matches the country-ordering rationale above. | y |
| Backend validation strictness | Loose E.164 shape check: `^\+[1-9]\d{7,14}$`, applied only when `phone` is non-empty | Full per-country length validation would require duplicating the toolkit's country/mask table in Go; a loose E.164 shape check is enough defense-in-depth without that duplication. Field stays optional (empty string still allowed). | y |
| Backend error message | Plain English 400, e.g. `"invalid phone number"` | AGENTS.md: API error strings are always English; translation is the frontend's job. | y |
| `PhoneInput` component location | `internal/dashboard/ui/src/components/patterns/PhoneInput.tsx`, prop-driven (`value`, `onChange`, no internal fetch/state beyond the country selection) | Matches this session's established pattern for reusable, prop-driven components (`AppMembersList`, `AppUsersTab`). | y |
| Legacy phone values with no `+` prefix (pre-existing data) | Drawer falls back to `BR` as the detected country and shows the raw digits as the national number (best-effort, not guaranteed correct) | No bulk backfill in scope (see Out of Scope); this only affects display/pre-fill on next edit, not data integrity — saving from the drawer always re-normalizes to the correct `+{dialCode}` format going forward. | y |

**Open questions:** none — all resolved above. Remaining implicit-requirement dimensions (idempotency, concurrency, observability, data lifecycle, auth boundaries) are N/A for this scope — this is a single synchronous PUT already covered by existing auth/session middleware, unchanged by this feature.

---

## User Stories

### P1: Country-aware phone entry in the edit drawer ⭐ MVP

**User Story**: As a dashboard admin editing an app end-user, I want to pick the user's country and type their phone number in that country's format, so that I can't accidentally save a malformed or free-text phone number.

**Why P1**: This is the entire feature — without it, nothing changes for the admin.

**Acceptance Criteria**:

1. WHEN the admin opens `EditUserDrawer` for a user whose `phone` starts with a recognized `dialCode` (longest match) THEN the system SHALL pre-select that country and pre-fill the masked input with the remaining national digits.
2. WHEN the admin opens `EditUserDrawer` for a user with an empty `phone` or a `phone` that matches no known `dialCode` THEN the system SHALL default the country selection to `BR`.
3. WHEN the admin selects a different country from the select THEN the system SHALL re-mask the currently-typed digits using that country's mask.
4. WHEN the admin types digits into the phone input THEN the system SHALL format them live using `globalCellphoneMask(selectedCountryCode, value)`.
5. WHEN the admin clicks Save THEN the system SHALL send `phone` as `+{dialCode}{clearMask(input)}` in the `PUT` request body (empty string if the input is empty).
6. The system SHALL list `BR` first in the country select, followed by the remaining countries sorted alphabetically by their `en` name.

**Independent Test**: Open the edit drawer for a user with `phone = "+5511987654321"`, confirm the country select shows Brazil and the input shows `(11) 98765-4321`; switch country, retype digits, save, and confirm the outgoing PUT body's `phone` matches the new country's dial code + digits.

---

### P2: Backend rejects malformed phone shape ⭐

**User Story**: As the system, I want to reject an obviously malformed `phone` value on `UpdateAppUser` even if it didn't come from the drawer, so that the API has its own guarantee independent of the UI.

**Why P2**: Defense-in-depth — the UI already prevents this, but the endpoint is also callable directly.

**Acceptance Criteria**:

1. IF `body.Phone` is non-empty and does not match `^\+[1-9]\d{7,14}$` THEN the system SHALL respond `400` with a plain-English error message and SHALL NOT call `UpdateAppUser` in the store.
2. WHERE `body.Phone` is an empty string THEN the system SHALL treat it as valid (phone remains optional) and proceed unchanged.
3. WHEN `body.Phone` matches the pattern THEN the system SHALL persist it unchanged (store behavior is otherwise untouched).

**Independent Test**: `PUT` a user with `phone: "not-a-phone"` and confirm `400` + English error body; `PUT` the same user with `phone: "+5511987654321"` and confirm `200` + the stored value round-trips on the next `GET`.

---

## Edge Cases

- IF the toolkit's `countries` list has no entry for a given `dialCode` prefix at all (never observed, but defensive) THEN the system SHALL fall back to `BR` per AUT-02's default rule.
- IF the admin clears the phone input entirely THEN the system SHALL send `phone: ""` (not send the previous value, not omit the key) — consistent with the existing partial-update contract already used by this same endpoint for email/role.
- WHEN two dial codes are prefixes of each other (e.g. `+1` vs `+1264`) THEN the system SHALL pick the **longest** matching `dialCode` when detecting the country from a stored value.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AUT-01 | P1: Country-aware phone entry | Execute | Verified |
| AUT-02 | P1: Country-aware phone entry | Execute | Verified |
| AUT-03 | P1: Country-aware phone entry | Execute | Verified |
| AUT-04 | P1: Country-aware phone entry | Execute | Verified |
| AUT-05 | P1: Country-aware phone entry | Execute | Verified |
| AUT-06 | P1: Country-aware phone entry | Execute | Verified |
| AUT-07 | P2: Backend rejects malformed phone | Execute | Verified |
| AUT-08 | P2: Backend rejects malformed phone | Execute | Verified |
| AUT-09 | P2: Backend rejects malformed phone | Execute | Verified |

**ID format:** `AUT-[NUMBER]` — this spec is the sole owner of the `AUT-NN` namespace. The prefix was originally reused from `app-users-tab`, which caused a hard ID collision (both specs defined `AUT-01..AUT-09` for unrelated requirements). Resolved 2026-08-10: `app-users-tab` was renamed to `AUTAB-NN`; `app-user-edit-fields` already uses its own `AUE-NN`.

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 9 total, 9 mapped to Execute steps, 0 unmapped.

---

## Success Criteria

- [ ] `EditUserDrawer`'s phone field can no longer accept an arbitrary unmasked string — every keystroke is passed through the selected country's mask.
- [ ] A phone saved for any of the 235 supported countries round-trips correctly on the next edit (country pre-selects, national digits pre-fill).
- [ ] `UpdateAppUser` returns `400` with an English message for a non-empty, non-E.164-shaped phone, and `200` for a valid or empty one.
- [ ] `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`, `npx tsc -b`, and `npm run build` all pass clean.
