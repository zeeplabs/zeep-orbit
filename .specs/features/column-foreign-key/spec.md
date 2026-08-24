# Add/Remove Foreign Key on an Existing Column Specification

## Problem Statement

An app's table column can only get a foreign key today at the moment it's created (`AddTableColumnForUser`/`CreateAppTableForUser`) — there is no safe path to add or remove a foreign key on a column that already exists, in the REST API, the MCP tools, or the "Edit with AI" chat. This forces users into "drop and recreate the column" workarounds that lose data, and it means the "Edit with AI" chat has to explicitly decline a request it should be able to fulfill (confirmed live: a user asked to link `comments.task_id` to `tasks.id` after the column already existed, and the assistant could only refuse). Separately, the existing full-replace endpoint (`PUT /tables/{id}`) silently accepts a `References` change on an already-existing column, passes validation, and persists it to the stored schema — without ever running the DDL that would actually create the constraint in Postgres. That's a live schema-drift bug independent of this feature, closed here because both fixes touch the same code path (column mutation authorization + persistence).

## Goals

- [ ] A user (or the AI chat, or an MCP-calling agent) can add a foreign key to an existing column, or remove one, without recreating the column or losing data.
- [ ] `PUT /tables/{id}` never again claims to apply a `References` change on an existing column without actually running the DDL.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Changing a column's foreign key target/on_delete in one step (retarget) | Not requested; achievable today as two explicit operations (remove, then add) once this feature ships. |
| Multiple foreign keys per column | The data model (`config.ColumnConfig.References`) stores a single pointer, not a list; changing that is a much larger migration unrelated to this feature's goal. |
| A dedicated "list foreign keys for a table" endpoint | Not needed — `GetAppSchemaForUser`/the existing schema response already includes each column's `References` field, which is sufficient to know whether/where a column has a FK. |
| Full DDL support for `References` changes inside `PUT /tables/{id}` (full-replace) | Rejected by user decision (see Assumptions) in favor of blocking with a clear error and pointing at the dedicated endpoint — avoids duplicating the same validation/DDL/error-handling pipeline in two places. |
| Foreign keys spanning multiple columns (composite FK) | `ColumnConfig.References` is single-column by design; out of scope for this feature. |
| Automatic retry/idempotent recovery if the process crashes between a successful `ALTER TABLE` and persisting the stored schema | Rare crash-window edge case; on retry the request is treated as a fresh add attempt (see Edge Cases) and surfaces a normal (if slightly confusing) provisioning error rather than a special-cased recovery flow. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| `PUT /tables/{id}` behavior when `References` changes on an existing column | Reject the whole request with 400, explaining the dedicated endpoint must be used instead | User-confirmed. Avoids duplicating DDL/validation/error-handling logic in two pipelines; a hard, explicit error is safer than a silent no-op or a second half-tested code path. | y |
| Adding a FK to a column that already has one | Reject with 400 ("column already has a foreign key — remove it first") | User-confirmed. The data model only supports one `References` per column; an explicit two-step (remove, then add) is less surprising than an implicit drop+add bundled into one call. | y |
| Detail level in the FK-violation (orphaned row) error | Include Postgres's own `Detail` text (e.g. the specific offending key/value) | User-confirmed. The caller already has read access to this exact table's data via the same `CanWrite()` check — this isn't cross-tenant leakage, and the specific value is what makes the error actionable. | y |
| Whether the "Edit with AI" chat gets add/remove FK in this same feature | Yes — REST handler, MCP tool, and chat tool all ship together | User-confirmed. This was the actual motivating use case (a live user session hit the "can't do that" refusal). | y |
| How an existing FK constraint is located for removal | Look it up via Postgres's catalog (`information_schema`/`pg_constraint`) rather than assuming the `<table>_<column>_fkey` naming convention | The convention holds today (no code ever names a constraint explicitly) but is not guaranteed — looking it up is one query and removes a fragile assumption. | y (default — no gray area raised to user; catalog lookup is strictly safer than convention-guessing at equal cost) |
| Column-type compatibility check between the existing column and the referenced column | Compare the *actual* physical Postgres type of both columns (via `information_schema.columns`), not the type declared in the stored `config.ColumnConfig`, before attempting the DDL | The column already exists physically — checking its real type (not the possibly-stale stored config) catches a mismatch with a clear 400 instead of a raw Postgres DDL error. Mirrors the "stored config can drift from the real column" caution already flagged in this codebase's provisioner comments. | y (default — no gray area raised; strictly an implementation-quality improvement with no user-visible trade-off) |
| Concurrent/crash-window retry (DDL succeeds, persist to `app_tables` fails before commit) | Not specially handled — a retry is treated as a fresh add attempt | Documented explicitly in Out of Scope / Edge Cases rather than silently ignored. This exact crash window already exists, unhandled, for `AddTableColumnForUser`/`AddTableIndexForUser` today — not a new risk introduced by this feature. | y (default — consistent with existing sibling handlers' behavior, not a new gap) |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Add a foreign key to an existing column ⭐ MVP

**User Story**: As a developer editing my app's schema, I want to add a foreign key from an existing column to another table's column, so that I get referential integrity without dropping and recreating the column (and losing its data).

**Why P1**: This is the concrete, already-hit use case — a user's "Edit with AI" session couldn't complete a request that would otherwise require manual SQL outside the platform.

**Acceptance Criteria**:

1. WHEN a caller with `CanWrite()` on the app requests adding a foreign key to an existing column (target table, target column, optional `on_delete`) THEN the system SHALL run `ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY` on that column and persist the column's `References` in the stored schema (`app_tables`) only after the DDL succeeds.
2. IF the column already has a `References` value set THEN the system SHALL reject the request with HTTP 400 and a message stating the column already has a foreign key and must be removed first.
3. IF the target table doesn't exist, the target column is neither `id` nor `Unique: true`, or the `on_delete` value is invalid THEN the system SHALL reject the request with HTTP 400 using the same validation `validateReference` already applies when a column is created with a reference.
4. IF the target table is `_auth_users` and the existing column's type is not `uuid` THEN the system SHALL reject the request with HTTP 400 (mirrors the existing `_auth_users` rule for new columns).
5. IF the physical Postgres type of the existing column does not match the physical Postgres type of the target column THEN the system SHALL reject the request with HTTP 400, naming both real types, before attempting the DDL.
6. IF the `ALTER TABLE ADD CONSTRAINT` fails because existing rows violate the new constraint (Postgres error `23503`) THEN the system SHALL return HTTP 400 with a message that includes Postgres's own constraint-violation detail (the offending key/value).
7. IF the caller lacks `CanWrite()` on the app THEN the system SHALL return HTTP 403 and make no schema change.
8. WHEN the foreign key is added successfully THEN the system SHALL record an audit log entry with the correct origin (`dashboard`, `mcp`, or `ai_chat`), mirroring `AddTableColumnForUser`'s existing audit behavior.

**Independent Test**: Create a table with an existing `uuid` column and no FK; call the new "add foreign key" action (REST) targeting another table's `id` column; confirm the column now enforces the constraint in Postgres and the app's schema response reflects `References`.

---

### P1: Remove a foreign key from an existing column ⭐ MVP

**User Story**: As a developer editing my app's schema, I want to remove a foreign key from a column without dropping the column itself, so I can relax a constraint that's no longer correct without losing the column's data.

**Why P1**: Symmetric to add — a chat/REST/MCP flow that can add a FK but never remove one is an obvious, immediately-felt gap.

**Acceptance Criteria**:

1. WHEN a caller with `CanWrite()` on the app requests removing the foreign key from a column that currently has one THEN the system SHALL locate the real constraint via the Postgres catalog (not by assuming a naming convention), run `ALTER TABLE ... DROP CONSTRAINT`, and clear the column's `References` in the stored schema only after the DDL succeeds.
2. IF the column has no `References` set THEN the system SHALL reject the request with HTTP 400 stating the column has no foreign key to remove.
3. IF the caller lacks `CanWrite()` on the app THEN the system SHALL return HTTP 403 and make no schema change.
4. WHEN the foreign key is removed successfully THEN the system SHALL record an audit log entry with the correct origin (`dashboard`, `mcp`, or `ai_chat`).

**Independent Test**: Take a column with an existing FK (from the story above); call "remove foreign key"; confirm the constraint no longer exists in Postgres and the schema response no longer shows `References` for that column.

---

### P1: Expose both operations via MCP tools

**User Story**: As an AI agent operating zeep-orbit through MCP, I want `orbit_add_column_foreign_key` and `orbit_remove_column_foreign_key` tools, so I can perform the same safe mutation any dashboard user can.

**Why P1**: MCP tools in this codebase are consistently added in lockstep with their REST equivalent (see `orbit_add_table_column`/`orbit_add_table_index`); shipping the REST handler without the MCP tool would be an inconsistent, half-finished surface.

**Acceptance Criteria**:

1. The system SHALL expose `orbit_add_column_foreign_key` and `orbit_remove_column_foreign_key` MCP tools that call the same `*ForUser` functions as their REST equivalents, enforcing the same `CanWrite()` authorization.
2. WHEN either tool is called by a caller without `CanWrite()` on the target app THEN the system SHALL return the same class of error the REST endpoint would (mapped through the MCP server's existing `mapWriteError` convention).

**Independent Test**: Call `orbit_add_column_foreign_key` directly against a test app via the MCP server; confirm identical behavior (success and rejection paths) to the REST endpoint.

---

### P1: Expose both operations in the "Edit with AI" chat

**User Story**: As a user editing my app's schema conversationally, I want to ask the assistant to add or remove a foreign key on an existing column and have it actually do it, instead of being told it's unsupported.

**Why P1**: This is the original motivating scenario for the whole feature.

**Acceptance Criteria**:

1. The "Edit with AI" chat SHALL support `propose_add_foreign_key` and `propose_remove_foreign_key` operations, following the exact same one-operation-at-a-time propose → confirm → apply flow as the existing `propose_*` tools (no batching).
2. `editChatSystemPromptFor`'s current instruction to decline a foreign-key request on an existing column SHALL be replaced with instructions telling the model when to use `propose_add_reference` (brand-new column) versus `propose_add_foreign_key` (existing column already holding the right data type) versus `propose_remove_foreign_key`.
3. WHEN the model proposes `propose_add_foreign_key` or `propose_remove_foreign_key` THEN `EditChatConfirm` SHALL apply it through the same target `*ForUser` handler the REST endpoint uses, with audit origin `ai_chat`.
4. IF the handler rejects the operation (any P1 error above: already-has-FK, no-FK-to-remove, type mismatch, orphaned rows, invalid target) THEN the chat SHALL surface that specific message to the user (mirroring `respondEditChatConfirmError`'s existing AIEC-04 behavior), not a generic failure.

**Independent Test**: In a live "Edit with AI" session, ask to add a foreign key from an existing column to another table's `id` column, confirm the proposed operation, and see the constraint actually created — the exact scenario that previously failed.

---

### P1: Close the `PUT /tables/{id}` silent-no-op gap

**User Story**: As a developer using the manual full-replace table editor, I want a change I can't actually make through that form to fail loudly, so I never believe a foreign key exists when it doesn't.

**Why P1**: This is a live data-integrity bug (stored schema can already silently diverge from the real Postgres schema) — shipping the new dedicated endpoint without closing this gap leaves a strictly worse, still-silent alternate path standing.

**Acceptance Criteria**:

1. IF a `PUT /tables/{id}` request changes the `References` field of a column that already exists in the table (add, remove, or change target) THEN the system SHALL reject the entire request with HTTP 400, stating that foreign-key changes on an existing column must go through the dedicated add/remove-foreign-key endpoint, and SHALL persist nothing.
2. The system SHALL NOT, under any input, accept and persist a `References` change on an existing column without having actually run the corresponding DDL (closes the current gap where validation passes, the DDL step is skipped because the column already exists, and the stored schema still gets overwritten).
3. WHILE a `PUT /tables/{id}` request only changes `References` on a *brand-new* column being added in the same request THEN the system SHALL continue to accept and apply it exactly as it does today (this restriction only applies to columns that already exist before the request).

**Independent Test**: Submit a `PUT /tables/{id}` body that changes `References` on a pre-existing column (with everything else unchanged); confirm the request is rejected with 400 and the stored schema and real Postgres schema are both unchanged.

---

## Edge Cases

- IF the requested `on_delete` value is invalid THEN the system SHALL reject with HTTP 400 (same validation as new-column FK creation).
- IF the target table/column referenced for removal no longer exists in Postgres (manually dropped outside the platform) but the stored schema still lists a `References` THEN the system SHALL still attempt the catalog lookup for a constraint to drop; if none is found, treat it as "no foreign key to remove" (P1 Remove AC2) rather than erroring, and clear the stale `References` from the stored schema so it converges with reality.
- IF two confirm/apply requests for the same column's FK race (e.g. a double-click) THEN the second request SHALL fail with the normal "already has a foreign key" (add) or "no foreign key to remove" (remove) error once the first has completed — no special locking beyond the existing fetch-fresh-then-merge pattern already used by `AddTableColumnForUser`/`AddTableIndexForUser`.
- IF the process crashes between a successful `ALTER TABLE` and persisting the stored schema THEN a retry is treated as a fresh attempt and may return a raw "constraint already exists" provisioning error (HTTP 500, generic message, real error logged) — this narrow crash window is accepted as-is (see Out of Scope), matching the same unhandled window in sibling handlers today.
- IF the `_auth_users` special-case column-type rule (must be `uuid`) conflicts with the general physical-type check THEN the physical-type check subsumes it — comparing real types already produces the same rejection, so no separate code path is needed.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| CFK-01 | P1: Add FK | Design | Pending |
| CFK-02 | P1: Add FK | Design | Pending |
| CFK-03 | P1: Add FK | Design | Pending |
| CFK-04 | P1: Add FK | Design | Pending |
| CFK-05 | P1: Add FK | Design | Pending |
| CFK-06 | P1: Add FK | Design | Pending |
| CFK-07 | P1: Add FK | Design | Pending |
| CFK-08 | P1: Add FK | Design | Pending |
| CFK-09 | P1: Remove FK | Design | Pending |
| CFK-10 | P1: Remove FK | Design | Pending |
| CFK-11 | P1: Remove FK | Design | Pending |
| CFK-12 | P1: Remove FK | Design | Pending |
| CFK-13 | P1: MCP tools | Design | Pending |
| CFK-14 | P1: MCP tools | Design | Pending |
| CFK-15 | P1: AI chat | Design | Pending |
| CFK-16 | P1: AI chat | Design | Pending |
| CFK-17 | P1: AI chat | Design | Pending |
| CFK-18 | P1: AI chat | Design | Pending |
| CFK-19 | P1: Full-replace fix | Design | Pending |
| CFK-20 | P1: Full-replace fix | Design | Pending |
| CFK-21 | P1: Full-replace fix | Design | Pending |

**ID format:** `CFK-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 21 total, 0 mapped to tasks, 21 unmapped ⚠️ (expected at Specify — Design/Tasks phases will map these)

---

## Success Criteria

- [ ] A column that already exists and already holds correct data can get a foreign key added without recreating the column, via REST, MCP, and the "Edit with AI" chat.
- [ ] The same foreign key can be removed via all three surfaces without dropping the column.
- [ ] Every rejection path (already-has-FK, no-FK-to-remove, type mismatch, invalid target, orphaned rows, insufficient permission) returns a specific, actionable message — never a raw internal error string.
- [ ] `PUT /tables/{id}` can no longer silently "apply" a `References` change on an existing column without running DDL — it either runs it correctly or rejects loudly; the stored schema and real Postgres schema cannot drift apart through this path anymore.
