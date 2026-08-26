# column-enum-type Context

**Gathered:** 2026-08-26
**Spec:** `.specs/features/column-enum-type/spec.md`
**Status:** Ready for design

---

## Feature Boundary

Add a native `enum` column type to Orbit's app-table schema system, enforced at the database layer with a Postgres `CHECK` constraint (not a native Postgres `ENUM` type). Closes a gap surfaced by a client doing SDD on a new app (assets/employees/assignments/terms/assignment_requests): today any writer with write permission can persist an invalid status string, because status-like fields have no DB-level enforcement — validation is 100% client-side.

---

## Implementation Decisions

### Enum scope: creation-only

- `type: enum` is only accepted when a column is first created — either on `POST /dashboard/api/apps/{id}/tables` (new table) or `orbit_add_table_column` (new column on an existing table).
- Converting an existing non-enum column into `enum`, or an existing `enum` column into another type, is explicitly out of scope. Rationale: doing this safely on a populated column requires validating every existing row against an arbitrary new allowed-set and extending `migration.go`'s `safeTypeConversions` whitelist — real scope creep for a feature whose driving need is "new status fields on a not-yet-created app."

### Removing an allowed value: precise pre-check

- When editing an existing `enum` column's `AllowedValues` to remove a value, the system runs a `COUNT(*)` query against rows currently holding the value(s) being removed **before** attempting to change the constraint.
- If any row uses a value being removed, the change is rejected with a typed error naming the offending value(s) and the row count — not a generic Postgres constraint-violation message bubbled up.
- Adding new allowed values (widening) has no such check — always safe, just recreate the `CHECK` constraint with the expanded list.

### Agent's Discretion

Resolved technically, without a user decision needed (documented as assumptions in spec.md, not open questions):

- `AllowedValues` entries are arbitrary free text (not restricted to identifier characters) — this is a Brazilian HR/asset-status use case (e.g. "Em andamento", "Baixado"), so accented/spaced values must be allowed. They are safely single-quote-escaped when embedded in the generated `CHECK (col IN (...))` clause, following the existing `Default`-value quoting precedent in `provisioner/table.go`.
- Matching is exact/case-sensitive, no trimming or case-folding — least surprising default, avoids silently merging visually-distinct values.
- `NULL` is allowed on a nullable `enum` column regardless of `AllowedValues` (standard Postgres `CHECK` semantics: the check does not fire on `NULL`) — nullability is controlled by the existing `Nullable`/`NOT NULL` config, unchanged by this feature.
- If a `Default` is set on an `enum` column, it must be one of `AllowedValues` — validated the same place `validateDefault` already validates defaults per-type today.
- No ordering/workflow semantics: `AllowedValues` is a flat set for membership validation only. No state-transition guard (e.g. "pending" → "closed" without going through "in_progress" is not blocked). The client's original feedback was about invalid-value writes, not transition rules.
- Caps to prevent a pathological `CHECK (col IN (...))` clause: max 50 values per enum column, each value non-empty and ≤100 characters, no duplicate values (after exact-match comparison).

### Declined / Undiscussed Gray Areas → Assumptions

None declined — the two real product decisions (creation-only scope, precise pre-check on removal) were both resolved above with the recommended defaults. All remaining implementation-detail gray areas were resolved via Agent's Discretion above rather than left open.

---

## Specific References

None — no external product reference cited. The client's own SDD complaint ("qualquer valor pode ser gravado, ex.: status: 'qualquer coisa'") is the concrete driving scenario.

---

## Deferred Ideas

- Converting an existing column to/from `enum` after creation (needs `safeTypeConversions` extension + full-table data validation) — separate future feature if a real need surfaces.
- Native Postgres `ENUM` type — rejected for this feature (locking/migration downsides of `ALTER TYPE ... ADD VALUE` vs. a recreatable `CHECK` constraint).
- Ordering/workflow (state-transition) validation on enum values — out of scope, no evidence it's needed yet.
