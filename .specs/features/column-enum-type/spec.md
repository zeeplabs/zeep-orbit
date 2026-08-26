# Column Enum Type Specification

## Problem Statement

Orbit's app-table column types (`internal/config/validate.go`) cover `integer, bigint, numeric, boolean, uuid, timestamptz, jsonb, text` but have no fixed-value-set type. Any status-like field (asset status, employee status, assignment status, term status) is stored as free `text` with zero database-level enforcement — a client with write access can persist a value that doesn't belong to the intended set (e.g. `status: "qualquer coisa"`). Surfaced by a client running SDD on a new app (assets/employees/assignments/terms). Enforcement today is 100% client-side; the database accepts anything.

## Goals

- [ ] A client can declare a column as `type: enum` with a fixed set of allowed values, enforced by a Postgres `CHECK` constraint that rejects any other value at write time — through Dashboard UI, MCP tools, and AI build/edit chat.
- [ ] An existing `enum` column's allowed-value set can be widened (add values) or narrowed (remove values) safely — narrowing fails closed with a precise error when existing rows would violate the new set.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Converting an existing non-enum column to `enum` (or vice versa) via the table-edit/ALTER path | Requires validating every existing row against an arbitrary new set and extending `migration.go`'s `safeTypeConversions` whitelist — separate feature if a real need surfaces (see context.md Deferred Ideas). |
| Native Postgres `ENUM` type | `ALTER TYPE ... ADD VALUE` has migration/locking downsides vs. a recreatable `CHECK` constraint; rejected per user instruction. |
| Ordering / workflow (state-transition) validation between enum values | No evidence this is needed; `AllowedValues` is a flat membership set only, not an FSM. |
| Cross-column / cross-table enum reuse (shared enum definitions) | Each `enum` column declares its own `AllowedValues` independently; no shared "enum type" registry. |
| Owner/reference-column (`*_owner_id`) denormalization behavior | Already solved by the existing column-foreign-key feature (`ColumnConfig.References`), confirmed correct by the client — unrelated to this spec. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Enum settable only at column creation | No support for converting an existing column to/from `enum` | User-confirmed (see context.md) — smaller, safer scope | y |
| Removing an in-use allowed value | Precise `COUNT(*)` pre-check, reject with row count + offending value(s) named | User-confirmed (see context.md) — clearer UX than a raw Postgres constraint error | y |
| `AllowedValues` entry character set | Arbitrary free text (not identifier-restricted), safely single-quote-escaped in generated SQL | Brazilian HR/asset-status values are natural language ("Em andamento") — restricting to identifiers would break the exact use case that motivated this feature | y |
| Value matching | Exact, case-sensitive, no trimming/case-folding | Least surprising default; avoids silently merging visually distinct values | y |
| `NULL` handling | `NULL` always allowed on a nullable `enum` column regardless of `AllowedValues` | Standard Postgres `CHECK` semantics — the check does not fire on `NULL`; nullability stays controlled by existing `Nullable` config | y |
| `Default` value constraint | If set, `Default` must be a member of `AllowedValues` | Otherwise the column would provision with an unwritable default | y |
| Value count / length caps | Max 50 values per column, each 1-100 chars, no duplicates (exact match) | Prevents a pathological `CHECK (col IN (...))` clause; generous enough for any realistic status set | y |
| Order-of-appearance for `AllowedValues` | Preserved for UI display (e.g. dropdown order) but does not imply workflow order | The client asked for value validation, not transition rules | y |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Define an enum column with enforced allowed values ⭐ MVP

**User Story**: As a client building an app on Orbit, I want to declare a column as a fixed set of allowed values so that the database itself rejects any write outside that set, instead of relying on client-side validation alone.

**Why P1**: This is the entire problem statement — without it, the feature delivers nothing.

**Acceptance Criteria**:

1. WHEN a client creates a table (`POST /dashboard/api/apps/{id}/tables`) with a column of `type: "enum"` and a non-empty `AllowedValues` list THEN the system SHALL create the physical column as `text` and attach a `CHECK (col IN (...))` constraint restricting it to exactly those values.
2. WHEN a client adds a new column of `type: "enum"` to an existing table (`orbit_add_table_column` or the equivalent Dashboard/REST path) with a non-empty `AllowedValues` list THEN the system SHALL add the column with the same `CHECK` constraint.
3. WHEN an `INSERT` or `UPDATE` on an app table attempts to write a value to an `enum` column that is not in that column's `AllowedValues` THEN the database SHALL reject the write with a constraint-violation error, and the system SHALL map that error to a typed, non-leaking error message (per AGENTS.md §4 — no raw Postgres internals in the HTTP/MCP response).
4. IF `type: "enum"` is declared with an empty, missing, or duplicate-containing `AllowedValues` list THEN the system SHALL reject the column definition at request-validation time (before touching the database) with a specific error identifying the problem (empty list / duplicate value / value too long / too many values).
5. IF a `Default` is declared on an `enum` column and that default value is not present in `AllowedValues` THEN the system SHALL reject the column definition at request-validation time.
6. The system SHALL allow `AllowedValues` entries to contain spaces, accented characters, and other free-text characters, safely escaped when embedded in the generated `CHECK` clause (no SQL injection via a crafted allowed value).

**Independent Test**: Create a table with column `status enum` and `AllowedValues: ["pending", "active", "closed"]`. Confirm the physical column exists as `text` with a `CHECK` constraint. Insert a row with `status: "pending"` (succeeds). Attempt `status: "qualquer coisa"` (rejected with a typed error, not a raw Postgres message).

---

### P1: Widen an enum column's allowed values

**User Story**: As a client, I want to add a new allowed value to an existing enum column so that I can support a new status without recreating the table.

**Why P1**: Status sets evolve; without this the feature would force a destructive table recreation for the most common maintenance operation.

**Acceptance Criteria**:

1. WHEN a client updates an existing `enum` column's `AllowedValues` to a superset of the current list (only additions, no removals) THEN the system SHALL replace the `CHECK` constraint with one reflecting the new, larger set, without requiring any data migration.
2. WHEN the widening update completes THEN existing rows SHALL remain unaffected and immediately valid under the new constraint (all their current values are still members of the superset).

**Independent Test**: Column `status` has `AllowedValues: ["pending", "active"]` with existing rows. Update to `["pending", "active", "closed"]`. Confirm existing rows are untouched and a new row with `status: "closed"` now succeeds.

---

### P1: Narrow an enum column's allowed values safely

**User Story**: As a client, I want to remove an allowed value that's no longer valid, but be stopped clearly if doing so would orphan existing data, so that I don't corrupt or silently break my own records.

**Why P1**: Narrowing is the operation most likely to corrupt data if done carelessly (the exact concern the client's original SDD feedback raised for the whole enum gap) — this story is what makes the feature safe to actually use.

**Acceptance Criteria**:

1. WHEN a client updates an existing `enum` column's `AllowedValues` to remove one or more values THEN the system SHALL first run a `COUNT(*)` query for rows currently holding any value being removed.
2. IF that count is greater than zero for any value being removed THEN the system SHALL reject the entire update before altering the constraint, and SHALL return an error naming each offending value and its exact row count.
3. IF the count is zero for every value being removed THEN the system SHALL replace the `CHECK` constraint with the narrowed set.
4. The system SHALL perform the pre-check and the constraint replacement such that a narrowing rejection never leaves the table in a partially-migrated state (no constraint dropped without a valid replacement in place).

**Independent Test**: Column `status` has `AllowedValues: ["pending", "active", "closed"]` with one row at `status: "closed"`. Attempt to remove `"closed"` — rejected, error names `"closed"` and count `1`. Delete/update that row to a remaining value, retry — succeeds.

---

### P2: Enum type available in Dashboard UI, MCP, and AI chat

**User Story**: As a client, I want to pick "enum" as a column type and enter its allowed values from the same surfaces I already use to design tables (Dashboard form, MCP tool, AI build/edit chat), so the feature isn't backend-only.

**Why P2**: P1 makes the type real and enforced at the API/DB layer; this story is what makes it actually usable end-to-end without hand-crafting raw API calls.

**Acceptance Criteria**:

1. WHEN a client opens the column-type selector in the Dashboard's table editor (`TableCard.tsx`) THEN the system SHALL list `enum` alongside the existing types, and WHILE `enum` is selected THEN the system SHALL show an input for entering the list of allowed values.
2. WHEN a client calls `orbit_create_table` or `orbit_add_table_column` with a column of `type: "enum"` and `AllowedValues` THEN the system SHALL accept and provision it identically to the Dashboard path (same validation, same constraint).
3. WHEN a client asks the AI build or edit chat to create a status-like field with a small fixed set of values THEN the AI SHALL be allowed to propose `type: "enum"` with the appropriate `AllowedValues` (removing the current explicit prohibition in `ai_build_chat_handlers.go` and `ai_edit_chat_handlers.go`).
4. Any new user-facing string introduced in the Dashboard UI for this feature SHALL be added to both `en.json` and `pt-BR.json` in the same change (AGENTS.md §5).

**Independent Test**: In the Dashboard, create a column, select "enum", enter three values, save — table is created with the `CHECK` constraint. Ask the AI edit chat "add a status field with values open/closed" — AI proposes an `enum` column instead of refusing or falling back to plain `text`.

---

## Edge Cases

- IF `AllowedValues` contains more than 50 entries THEN the system SHALL reject the column definition at validation time with an error stating the limit.
- IF any `AllowedValues` entry is empty, exceeds 100 characters, or duplicates another entry (exact match) THEN the system SHALL reject the column definition at validation time, identifying the offending entry.
- IF an `AllowedValues` entry contains a single quote or other character requiring SQL escaping THEN the system SHALL still accept it and correctly escape it in the generated `CHECK` clause (no injection, no constraint-generation failure).
- WHEN a column is declared `type: "enum"` without `Nullable` explicitly set to allow `NULL` THEN existing default nullability rules apply unchanged (this feature does not alter nullability semantics).
- IF a narrowing update removes values used by rows in a table with a very large row count THEN the pre-check `COUNT(*)` SHALL be scoped with a `WHERE col = ANY(removed_values)` filter (not a full table scan comparison in application code) to keep the check reasonably fast — implementation detail for Design, not a new product behavior.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| CENUM-01 | P1: Define an enum column | Verify | ✅ Verified |
| CENUM-02 | P1: Define an enum column | Verify | ✅ Verified |
| CENUM-03 | P1: Define an enum column | Verify | ✅ Verified |
| CENUM-04 | P1: Define an enum column | Verify | ✅ Verified |
| CENUM-05 | P1: Define an enum column | Verify | ✅ Verified |
| CENUM-06 | P1: Define an enum column | Verify | ✅ Verified |
| CENUM-07 | P1: Widen allowed values | Verify | ✅ Verified |
| CENUM-08 | P1: Widen allowed values | Verify | ✅ Verified |
| CENUM-09 | P1: Narrow allowed values safely | Verify | ✅ Verified |
| CENUM-10 | P1: Narrow allowed values safely | Verify | ✅ Verified |
| CENUM-11 | P1: Narrow allowed values safely | Verify | ✅ Verified |
| CENUM-12 | P1: Narrow allowed values safely | Verify | ✅ Verified |
| CENUM-13 | P2: Enum in Dashboard/MCP/AI | Verify | ✅ Verified |
| CENUM-14 | P2: Enum in Dashboard/MCP/AI | Verify | ✅ Verified |
| CENUM-15 | P2: Enum in Dashboard/MCP/AI | Verify | ✅ Verified |
| CENUM-16 | P2: Enum in Dashboard/MCP/AI | Verify | ✅ Verified |

**Coverage:** 16 total, 16 verified, 0 gaps ✅ (independent Verifier, 2026-08-26 — see `validation.md`)

---

## Success Criteria

- [ ] A write with an out-of-set value to an `enum` column is rejected at the database layer in all three surfaces (Dashboard, MCP, direct API), not just client-side.
- [ ] Narrowing an enum's allowed values never silently corrupts or orphans existing data — it either succeeds cleanly or is rejected with a precise, actionable error.
- [ ] `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l` clean on changed files; `npx tsc -b` and `npm run build` clean on the Dashboard UI; both `en.json`/`pt-BR.json` updated and valid JSON.
