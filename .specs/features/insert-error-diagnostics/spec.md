# Insert Error Diagnostics & Native Upsert Specification

## Problem Statement

`HandleCreate` (`internal/server/handler.go`) collapses every Postgres insert failure that isn't a check violation or statement timeout into a generic `{"error":"failed to insert row"}` HTTP 500. This was hit in production on a Starbem app integrated via n8n consuming GitHub webhooks: at-least-once delivery caused duplicate inserts on a unique constraint, and an empty string `""` was sent for a `timestamptz` column. Both are ordinary integration conditions, not internal failures, but the client had to inspect Orbit server logs to find out why. The insert path is shared code for every app/table on the platform (schema-per-app isolates data, not logic), so this is a platform-wide reliability gap, not an app-specific bug.

## Goals

- [ ] Callers can distinguish "you sent something invalid" (400), "this already exists" (409), and "we broke" (500) from the insert response alone, without reading server logs.
- [ ] A `""` sent for a nullable `timestamptz` column succeeds as `NULL` instead of failing.
- [ ] Callers doing idempotent webhook-style inserts can request upsert behavior natively instead of building client-side dedup.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Composite unique constraints modeled in the registry/dashboard schema builder | Registry only tracks single-column `Unique`; modeling composite uniques as a schema concept is a separate, larger feature. This spec treats `conflict_columns` as caller-supplied, not registry-derived. |
| `on_conflict` support on `HandleUpdate`/bulk endpoints | Report and reproduction are insert-only (`HandleCreate`). Update conflict semantics are a different problem (a target row already resolved by `id`). |
| Normalizing `""` for any type other than nullable `timestamptz` | `date`/`time` are not distinct column types in this schema (only `timestamptz` exists per `internal/dashboard/handler.go` `allowedTypes`); other types (`text`, `integer`, etc.) accepting `""` is either valid (`text`) or already a clear type-mismatch error from the driver that doesn't need special-casing. |
| Retrying on 409/500 automatically server-side | Retry policy is a client concern; the server's job here is to report accurately, not to paper over conflicts. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Conflict target for `on_conflict` | Caller supplies `conflict_columns: string[]` in the request body when needed (see below); server never infers it from the registry | Registry only models single-column uniques; the reported bug (tags/branches/issues) uses composite uniques created outside the dashboard's column model, so registry-inference would miss exactly the cases that motivated this feature | y (user) |
| Response when `on_conflict:"ignore"` no-ops | 200 with the existing row, fetched by a follow-up `SELECT ... WHERE <conflict_columns match>` when `conflict_columns` was supplied; 204 with no body when `conflict_columns` was omitted (bare `DO NOTHING`, no way to identify which row it collided with) | Keeps the endpoint idempotent from the caller's point of view when it can, without inventing an identity it doesn't have | y (user) |
| `conflict_columns` requirement for `on_conflict:"update"` | Required; 400 if missing, since Postgres's `DO UPDATE` needs an explicit inference target | Same registry gap as above — `update` cannot proceed without knowing what "conflict" means for this insert | y (user) |
| `conflict_columns` requirement for `on_conflict:"ignore"` | Optional | Bare `ON CONFLICT DO NOTHING` (no target) is valid Postgres and catches a violation on *any* unique/exclusion constraint on the table, which is exactly what "ignore duplicates" means | n (agent default; low-risk, matches Postgres semantics) |
| Columns updated on `on_conflict:"update"` | All columns present in the request body except `conflict_columns` themselves and system fields (`id`, `created_at`, `owner_id`) | Standard upsert semantics — update what the caller sent, don't touch what they didn't | n (agent default, standard pattern) |
| `updated_at` on `on_conflict:"update"` | Set to `now()` as part of the `DO UPDATE SET` | Matches existing `updated_at`-on-write convention elsewhere in the codebase; an updated row with a stale `updated_at` would be a regression | n (agent default, consistent with existing convention) |
| Default `on_conflict` value when the field is omitted | `"error"` (today's behavior: unique violation surfaces as an error, now 409 instead of 500) | Explicit instruction from the user's original report; must not change behavior for existing callers who don't opt in | y (user, prior turn) |
| Postgres error codes classified as 400 ("invalid input") | `22007` (invalid_datetime_format), `22008` (datetime_field_overflow), `22P02` (invalid_text_representation) | These are pgx's standard codes for "value doesn't parse as the column's type," which is what an empty string into `timestamptz` produces; `23514` (check_violation) is already handled separately and untouched by this spec | n (agent default, verified against reported error class) |
| 400 message format for invalid input | `"invalid value for column \"<column>\": <reason>"`, reason derived from the Postgres error class (e.g. `"not a valid timestamp"`), never `pgErr.Message`/`pgErr.Detail` verbatim | Matches existing `checkViolationMessage` pattern of never echoing raw driver text (can leak attempted values); AGENTS.md forbids leaking raw `err.Error()` to clients | n (agent default, following existing `checkViolationMessage` precedent in the same file) |
| 409 message format for unique violation | `"row already exists: unique constraint %q"` naming `pgErr.ConstraintName` when present, else `"row already exists"` | Constraint name is safe to expose (it's schema structure, not data); mirrors `checkViolationMessage`'s existing column/constraint-name pattern | n (agent default, following existing precedent) |
| `""` → `NULL` normalization scope | Only columns where `Type == "timestamptz"` and `Required == false`; a `""` sent for a `Required` (`NOT NULL`) `timestamptz` column still fails, but now with a 400 naming the column instead of a raw Postgres error | Normalizing a required field's empty string would silently accept data that violates the schema's own contract — the fix must not narrow validation, only stop leaking driver internals for cases that are genuinely valid | y (user, prior turn: "NÃO for coluna NOT NULL, aquelas devem continuar dando 400") |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Insert errors are classified, not collapsed ⭐ MVP

**User Story**: As an integrator posting rows into an Orbit app table (e.g. from an n8n webhook), I want insert failures to come back with a status code and message that tell me what was wrong, so that I can fix my request without pulling Orbit server logs.

**Why P1**: This is the reported production issue. Without it, every insert failure looks identical and unfixable from the client side.

**Acceptance Criteria**:

1. IF an insert fails with Postgres error code `22007`, `22008`, or `22P02` (invalid type/format for a column's declared type) THEN the system SHALL respond HTTP 400 with a message naming the offending column and a reason that does not include the raw attempted value.
2. IF an insert fails with Postgres error code `23505` (unique_violation) AND the request's `on_conflict` is absent or `"error"` THEN the system SHALL respond HTTP 409 with a message naming the violated constraint when `pgErr.ConstraintName` is non-empty.
3. IF an insert fails with any Postgres error code other than `22007`, `22008`, `22P02`, `23505`, and `23514` (already handled) THEN the system SHALL respond HTTP 500 with the existing generic `"failed to insert row"` message, and SHALL log the real underlying error server-side only.
4. The system SHALL NOT include `pgErr.Message` or `pgErr.Detail` verbatim in any HTTP response body for any insert error path.
5. WHILE the existing `23514` (check_violation) and statement-timeout handling remain unchanged, the system SHALL preserve their current status codes and message format.

**Independent Test**: POST a row with `""` for a nullable `timestamptz` column → expect 400 naming that column. POST a row that collides with an existing unique value → expect 409 naming the constraint. Force an unrelated DB error (e.g. stop the DB mid-request in a test double) → expect 500 with the generic message, and confirm the real error appears only in server logs.

---

### P2: Empty string normalizes to NULL for nullable timestamptz columns

**User Story**: As an integrator whose upstream system sends `""` instead of omitting an optional date/time field, I want that to be accepted as "no value" rather than rejected, so that I don't have to special-case empty strings before every insert.

**Why P2**: Directly fixes the second half of the reported production error, but is independent of the error-classification story (P1 already makes today's failure legible even without this normalization).

**Acceptance Criteria**:

1. WHEN the request body contains `""` for a column where `Type == "timestamptz"` AND `Required == false` THEN the system SHALL insert `NULL` for that column instead of passing `""` to Postgres.
2. IF the request body contains `""` for a column where `Type == "timestamptz"` AND `Required == true` THEN the system SHALL respond HTTP 400 naming that column (per P1's AC1 classification — Postgres still rejects `NULL`/`""` for a NOT NULL column, and that rejection is now legible instead of raw).
3. The system SHALL NOT apply `""` → `NULL` normalization to any column type other than `timestamptz`.

**Independent Test**: POST `{"closed_at": ""}` where `closed_at` is nullable `timestamptz` → 201, row has `closed_at: null`. POST the same for a `Required` `timestamptz` column → 400 naming the column.

---

### P3: Native upsert via `on_conflict`

**User Story**: As an integrator receiving at-least-once webhook deliveries, I want to tell the insert endpoint how to behave on a duplicate (ignore or update) so that retries are idempotent without me tracking which rows I've already sent.

**Why P3**: Solves the root cause of the reported duplicate-insert failures, but P1 alone (409 instead of 500) already gives callers enough to build their own client-side idempotency; this story is the more complete fix.

**Acceptance Criteria**:

1. WHERE the request body includes `on_conflict: "ignore"` THEN the system SHALL build the insert as `ON CONFLICT DO NOTHING` when `conflict_columns` is absent, or `ON CONFLICT (<conflict_columns>) DO NOTHING` when present.
2. WHEN `on_conflict: "ignore"` results in zero rows inserted (conflict occurred) AND `conflict_columns` was supplied THEN the system SHALL respond HTTP 200 with the existing row fetched by matching `conflict_columns` values.
3. WHEN `on_conflict: "ignore"` results in zero rows inserted (conflict occurred) AND `conflict_columns` was absent THEN the system SHALL respond HTTP 204 with no body.
4. WHEN `on_conflict: "ignore"` results in a row being inserted (no conflict) THEN the system SHALL respond HTTP 201 with the inserted row, unchanged from today's success path.
5. WHERE the request body includes `on_conflict: "update"` THEN the system SHALL require `conflict_columns` to be a non-empty array; IF it is absent or empty THEN the system SHALL respond HTTP 400.
6. WHERE the request body includes `on_conflict: "update"` with valid `conflict_columns` THEN the system SHALL build `ON CONFLICT (<conflict_columns>) DO UPDATE SET <every body column except conflict_columns and system fields>, updated_at = now()` and respond HTTP 200 with the resulting row (200, not 201, since the row may not be new).
7. IF the request body includes `on_conflict` with any value other than `"ignore"`, `"update"`, or `"error"` THEN the system SHALL respond HTTP 400.
8. WHERE `on_conflict` is absent from the request body THEN the system SHALL behave exactly as `on_conflict: "error"` (today's behavior, now surfaced as 409 per P1 instead of 500).
9. IF `conflict_columns` names a column not present in the table's schema THEN the system SHALL respond HTTP 400 naming the invalid column.

**Independent Test**: POST the same row twice with `on_conflict: "ignore"` and `conflict_columns: ["commit_sha"]` → first is 201, second is 200 with the same row. POST twice with `on_conflict: "update"` and a changed field → second is 200 with the updated value and a newer `updated_at`. POST with `on_conflict: "update"` and no `conflict_columns` → 400.

---

## Edge Cases

- IF `on_conflict: "update"` is combined with a table that has zero unique constraints on `conflict_columns` (no matching index) THEN Postgres itself rejects the query (`42P10`) — the system SHALL classify this the same as other unhandled-code paths (500, generic message, real error logged), since it's a caller-configuration error not covered by P1's specific 400/409 cases. *(Documented here rather than as a new P1 AC because it is a `42P10`, outside the classified-code list — falls through to the existing catch-all.)*
- IF the request body sends `""` for a nullable `timestamptz` column AND `on_conflict: "update"` targets that same column in `conflict_columns` THEN normalization (P2) SHALL still apply before the conflict resolution runs — order is: normalize body → build query (including `ON CONFLICT`) → execute.
- WHEN `conflict_columns` overlaps with columns not present in the current request body (e.g. relying on a previously-set value) THEN the system SHALL still build a valid `DO UPDATE SET` from whatever body columns are present; `conflict_columns` are excluded from the SET list regardless of whether they were re-sent.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| INSERTERR-01 | P1: Insert errors are classified | Design | Pending |
| INSERTERR-02 | P1: Insert errors are classified | Design | Pending |
| INSERTERR-03 | P1: Insert errors are classified | Design | Pending |
| INSERTERR-04 | P1: Insert errors are classified | Design | Pending |
| INSERTERR-05 | P1: Insert errors are classified | Design | Pending |
| INSERTERR-06 | P2: Empty string normalizes to NULL | Design | Pending |
| INSERTERR-07 | P2: Empty string normalizes to NULL | Design | Pending |
| INSERTERR-08 | P2: Empty string normalizes to NULL | Design | Pending |
| INSERTERR-09 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-10 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-11 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-12 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-13 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-14 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-15 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-16 | P3: Native upsert via on_conflict | Design | Pending |
| INSERTERR-17 | P3: Native upsert via on_conflict | Design | Pending |

**Coverage:** 17 total, 0 mapped to tasks, 17 unmapped ⚠️ (tasks phase not yet run)

---

## Success Criteria

- [ ] No insert failure that matches a classified Postgres error code (`22007`, `22008`, `22P02`, `23505`, `23514`) returns HTTP 500.
- [ ] A `""` value for a nullable `timestamptz` column succeeds; for a required one it 400s naming the column.
- [ ] A caller can send `on_conflict: "ignore"` or `"update"` with `conflict_columns` and get idempotent behavior on retry, verified by integration test sending the same insert twice.
- [ ] No response body for any insert error path contains `pgErr.Message`, `pgErr.Detail`, or the raw attempted value.
