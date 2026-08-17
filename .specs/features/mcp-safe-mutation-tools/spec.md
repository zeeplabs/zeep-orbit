# MCP Safe Mutation Tools Specification

## Problem Statement

Beyond the read-only gap covered by `.specs/features/mcp-read-only-tools/`, an MCP-connected agent today can create a table (`orbit_create_table`) but has no safe way to evolve one afterwards. Adding a column, adding an index, or adding a foreign-key relationship to an existing table currently has **no dedicated REST endpoint at all** — the only path is `PUT /apps/{id}/tables/{tableId}` (`UpdateAppTable`, `internal/dashboard/handler.go:1281`), a full-replace of the table's entire `columns` and `indexes` arrays. A 2026-08-17 code survey confirmed this is unsafe to wrap directly in an MCP tool: `applyColumnChanges` (`internal/provisioner/table.go:343`) re-diffs every column in the submitted array against what Postgres already has, so an agent resending the existing columns to add one new one can accidentally trigger the column-type-change pipeline (`column-type-change-feedback` spec) on untouched columns, or — if it omits an existing column by mistake — silently orphan that column (it survives physically in Postgres but disappears from the metadata the API reports, per `internal/dashboard/apps_store.go:642`'s `UPDATE ... SET columns = $4` overwrite).

The fix is not "wrap `UpdateAppTable` carefully" — it's to give the table-evolution operations their own genuinely additive, server-side-merge backend endpoints, the same way `UpdateTableRLSModeForUser` (`internal/dashboard/handler.go:1377`) already did for RLS mode: fetch the table's current definition server-side, merge in exactly the one new thing the caller asked for, validate and apply the merged result, persist the merged result — never trust the caller to resend an unrelated array intact. Once those endpoints exist, they're safe to expose as MCP tools under the same authorization model this codebase already uses everywhere (`GetApp(ctx, pool, appID, user)` + role check).

This spec also covers a second, independent bucket: additive webhook operations (`CreateWebhook`, `SaveEventMapping`) that already have safe, non-destructive endpoints today and need no new backend work — only an MCP tool wrapper.

## Goals

- [x] An MCP client can add a single new column to an existing table (with an optional foreign-key reference to another table/column) without resending or risking any other column already on that table.
- [x] An MCP client can add a single new index to an existing table without resending or risking the table's existing indexes.
- [x] An MCP client can create a new webhook and register a new event mapping on an app, using the existing endpoints as-is.
- [x] Every new backend endpoint in this spec merges server-side against the table's current stored definition — the request body never needs to (and structurally cannot) omit or corrupt an existing column/index.
- [x] Every new tool authorizes through the same `GetApp` + `role.CanWrite()`/`CanManage()` path every existing write tool already uses — no new permission model.
- [x] Every new tool's mutation is auditable through the existing `h.audit(...)` call, same as every other write operation in this codebase.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---|---|
| Adding a foreign-key reference to a column that already exists (no new column involved) | Requires `ALTER TABLE ... ADD CONSTRAINT ... REFERENCES`, which validates existing row data and can fail for reasons unrelated to schema shape (orphaned values). Genuinely useful, but a different failure mode than "add a brand-new column" — deserves its own endpoint and its own error-handling story, not bundled into this spec's simpler case. |
| Renaming or dropping a column, index, or constraint via any new tool | Destructive/identity-changing by nature — explicitly excluded from every MCP-tool survey so far (`mcp-server` spec's Out of Scope, the 2026-08-17 REST survey's "unsafe" bucket). Column *rename* already exists in `UpdateAppTable`'s full-replace path for the Dashboard UI's own use — this spec doesn't touch or re-expose it. |
| `orbit_update_webhook` (full-replace `PATCH .../webhooks/{webhookId}`) | `UpdateWebhook`'s handler comment (`internal/dashboard/webhooks_handler.go:163-165`) confirms it fully replaces `name`/`method`/`event_type_path`/`event_id_path` together — an agent that means to change one field risks blanking another it didn't intend to touch. Same category of risk as the table full-replace this spec is designed to avoid; needs its own additive/partial-update endpoint first, same reasoning as the column/index work above. |
| `orbit_create_app_token` (issuing a new app API token) | Additive in the narrow sense (doesn't invalidate anything), but the effect is minting a new credential with app-wide scope — a credential-issuance decision, not a schema decision. Confirmed with the user's own scoping in the 2026-08-17 survey follow-up: keep credential-surface tools out of the "safe schema/config mutation" bucket regardless of additivity. |
| `orbit_add_app_member` (granting a person access to an app) | Additive and idempotent (`ErrAlreadyMember` already handles the duplicate case), but granting access is an access-governance decision about a person, not a schema mutation — a different risk category than everything else in this spec, per the same 2026-08-17 survey follow-up. |
| Any tool wrapping `UpdateAppTable` itself | This spec's entire premise is that the full-replace endpoint is unsafe to expose directly; nothing here changes or wraps it. |
| Changing `CREATE INDEX`'s locking behavior for the Dashboard UI's own existing `PUT .../tables/{tableId}` full-replace path | Out of scope for this spec — if `ensureIndexes` moves to `CREATE INDEX CONCURRENTLY` (see Assumptions), that change is scoped to the new additive index endpoint this spec introduces, not a retrofit of the existing full-replace path (would need its own transaction-boundary changes, tracked separately if ever needed). |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| New endpoint shape for "add column" | **Corrected during Execute**: implemented as a new `*Handler` method (`AddTableColumnForUser`) reachable only through the `orbit_add_table_column` MCP tool — no new `POST /dashboard/api/apps/{id}/tables/{tableId}/columns` HTTP route was wired, since neither design.md nor tasks.md called for one. The spec's Goals only require MCP-client capability, which this satisfies; the REST-endpoint shape below was the original assumption, not what was built | Design/tasks scoped this as an MCP-only capability (thin tool wrapper over a new Handler method), matching the existing internal reuse pattern, rather than also exposing a new REST route the Dashboard UI doesn't yet have a use for. A REST route can be added later without changing `AddTableColumnForUser`'s signature, if a UI need for it appears | y — confirmed by reading the actual implementation; no `internal/server` route references this method |
| New endpoint shape for "add index" | Same correction as above, for `AddTableIndexForUser`/`orbit_add_table_index` — MCP-tool-only, no new `POST /dashboard/api/apps/{id}/tables/{tableId}/indexes` HTTP route | Same rationale | y — confirmed by reading the actual implementation |
| Server-side merge mechanism | The new handler/`*ForUser` function loads the table's current `Columns`/`Indexes` from the already-fetched `app.Tables` (from `GetApp`), appends the one new column/index, then calls `validateTableInput`/`config.ValidateTables` and `h.prov.Apply` with the merged full list — the caller-supplied request body never contains anything but the one new item | Exactly mirrors `UpdateTableRLSModeForUser`'s existing pattern (`handler.go:1377`): fetch current state server-side, mutate only the targeted field, persist the merged whole. This is the concrete mechanism that eliminates the "agent forgets an existing column" orphaning risk identified in the 2026-08-17 survey | y — this is the point of the spec |
| Duplicate name handling | IF the new column name already exists on the table, or the new index name already exists on the table, THEN the endpoint SHALL reject with a 400 validation error (reusing `validateTableInput`'s existing duplicate-name-shaped checks where applicable, extended to columns/indexes) — never silently overwrite | An "add" operation that can silently turn into a "replace" reintroduces exactly the ambiguity this spec exists to remove | y |
| `CREATE INDEX` locking behavior | Out of scope to change in this spec — the new `orbit_add_table_index` tool's docstring/description SHALL warn the calling agent (and, by extension, its operator) that index creation is a blocking operation on the target table, matching `ensureIndexes`'s current non-concurrent behavior. **Corrected during design (`design.md` Risks & Concerns / Tech Decisions)**: `h.prov.Apply` never opens an explicit transaction — every DDL statement runs as its own autocommit `pool.Exec` — so `CREATE INDEX CONCURRENTLY` is structurally usable today; the real blocker is that this codebase has no cleanup/retry logic for the `INVALID` index `CONCURRENTLY` can leave behind on a failed build. Design decided to keep the current blocking behavior for this spec and disclose it to the caller, not adopt `CONCURRENTLY` | y — resolved during design, see `design.md` |
| FK validation failure at apply-time (not request-validation-time) | IF the new column's `references` is well-formed (target table/column exist, target has PK/UNIQUE, valid `on_delete`) per `config.ValidateTables`, but the underlying `ALTER TABLE ... ADD COLUMN ... REFERENCES ...` still fails at the database level (e.g. a target constraint that changed between validation and apply — a narrow race, not an expected case) THEN the endpoint SHALL return the same structured 500-with-generic-message pattern (`AGENTS.md` §4) already used for provisioning failures elsewhere in `UpdateAppTable`, not a raw Postgres error string | Consistent with the existing error-handling convention for provisioner failures; this is a defensive edge case, not the expected path (new-column FK additions don't have pre-existing rows to violate the constraint, unlike the "reference an existing column" case explicitly excluded above) | y |
| Webhook tools reuse existing endpoints as-is | `orbit_create_webhook` wraps `CreateWebhook` (`internal/dashboard/webhooks_handler.go:112`) and `orbit_save_webhook_event_mapping` wraps `SaveEventMapping` (`internal/dashboard/webhooks_handler.go:353`) directly — no new backend endpoint needed for either | Both are already genuinely additive: `CreateWebhook` has no overwrite path, `SaveEventMapping` rejects conflicting inserts with a 409 (`ErrMappingConflict`) instead of silently overwriting | y |
| Audit action names | New audit actions `app.table_column.create` and `app.table_index.create`, following the existing `app.table_policy.create`/`app.table.update` naming convention (`internal/dashboard/handler.go:1569`) | Consistency with existing `h.audit(...)` call-site naming across the codebase | y |
| Tool authorization level | `role.CanWrite()` for add-column and add-index (same tier `CreateAppTableForUser`/`UpdateTableRLSModeForUser` already require) — not the stricter `role.CanManage()` `CreateTablePolicyForUser` requires, since these are structural/schema operations, not access-control operations | Matches the existing tier split in the codebase: schema mutations use `CanWrite()`, access/policy-shaping mutations use the stricter `CanManage()` | y |

**Open questions:** none — the `CREATE INDEX CONCURRENTLY` question is resolved in `design.md` (kept as blocking `CREATE INDEX IF NOT EXISTS`; `CONCURRENTLY` is structurally feasible but deferred for lack of failure-cleanup logic, not a transactional constraint as first assumed above).

---

## User Stories

### P1: Agent adds a column to an existing table, including an optional relationship ⭐ MVP

**User Story**: As an operator using an MCP-connected agent, I want the agent to add a single new column (optionally referencing another table as a foreign key) to a table I already created, so that I can evolve a schema conversationally without hand-writing a full-table `PUT` request or risking the rest of the table's columns.

**Why P1**: This is the most-requested gap (schema evolution after initial table creation) and the one with the clearest, already-proven-safe implementation pattern (`UpdateTableRLSModeForUser`'s server-side-merge approach) to follow.

**Acceptance Criteria**:

1. WHEN an MCP client calls a new `orbit_add_table_column` tool with a valid `app_id`, `table_name`, and a single new column definition (name, type, required, optional default, optional `references`) THEN the system SHALL add that column to the table while leaving every other column, index, and RLS setting on that table unchanged.
2. WHEN the new column's `references` field is present and points at a valid existing table/column (per the same rules `config.ValidateTables` already enforces for table creation) THEN the system SHALL create the column with that foreign-key constraint.
3. IF the new column's name already exists on the table THEN the system SHALL reject the request with a 400 validation error and SHALL NOT modify the table.
4. IF the new column's `references` field points at a nonexistent table/column, a target lacking a PK/UNIQUE constraint, or specifies an invalid `on_delete` value THEN the system SHALL reject the request with the same validation error `config.ValidateTables` already produces for that case at table-creation time, and SHALL NOT modify the table.
5. IF the caller's role fails `role.CanWrite()` on the given app, or the app/table doesn't exist/isn't visible to the caller THEN the system SHALL return the same forbidden/not-found error `UpdateTableRLSModeForUser` already returns for the equivalent case.
6. The system SHALL record the successful column addition in the audit log (`app.table_column.create`), following the existing `h.audit(...)` pattern.

**Independent Test**: Create a table with two columns via `orbit_create_table`, then call `orbit_add_table_column` to add a third column with a foreign-key reference to another existing table; confirm via `orbit_get_app_schema` (or `orbit_list_table_policies`'s sibling read tool) that all three original-plus-new columns are present and the new one carries the reference; attempt to add a column with a duplicate name and confirm it's rejected with the table left untouched.

---

### P2: Agent adds an index to an existing table

**User Story**: As an operator using an MCP-connected agent, I want the agent to add a single new index (optionally unique) to a table, so that query-performance or uniqueness needs discovered mid-conversation can be applied immediately without a full-table update.

**Why P2**: Independently useful from column addition, but less frequently needed in the middle of an app-building conversation than adding a column — most schemas are usable without a hand-picked index from day one.

**Acceptance Criteria**:

1. WHEN an MCP client calls a new `orbit_add_table_index` tool with a valid `app_id`, `table_name`, and a single new index definition (name, target columns, unique flag) THEN the system SHALL add that index to the table while leaving every other index and all columns unchanged.
2. IF the new index's name already exists on the table THEN the system SHALL reject the request with a 400 validation error and SHALL NOT modify the table.
3. IF any of the index's target columns don't exist on the table THEN the system SHALL reject the request with a validation error and SHALL NOT modify the table.
4. IF the caller's role fails `role.CanWrite()` on the given app, or the app/table doesn't exist/isn't visible to the caller THEN the system SHALL return the same forbidden/not-found error the column-addition tool returns for the equivalent case.
5. The system SHALL record the successful index addition in the audit log (`app.table_index.create`).
6. The tool's description SHALL disclose to the calling agent that index creation briefly blocks writes to the target table, so the operator can be warned before running it against a table already receiving production traffic.

**Independent Test**: Add an index on an existing column via `orbit_add_table_index`; confirm the index is queryable/reflected in the schema; attempt to add an index with a duplicate name and confirm rejection; attempt to add an index referencing a nonexistent column and confirm rejection.

---

### P3: Agent creates a webhook and registers an event mapping

**User Story**: As an operator using an MCP-connected agent, I want the agent to create a new webhook and map an event type to a target table/column, so that integration setup discovered mid-conversation doesn't require switching to the Dashboard UI.

**Why P3**: Useful and genuinely safe today with zero new backend work, but a narrower use case than schema evolution — lowest priority of the three tiers in this spec.

**Acceptance Criteria**:

1. WHEN an MCP client calls a new `orbit_create_webhook` tool with a valid `app_id` and webhook config THEN the system SHALL create the webhook using the same validation `CreateWebhook`'s REST handler already applies, without affecting any other webhook on the app.
2. WHEN an MCP client calls a new `orbit_save_webhook_event_mapping` tool with a valid `app_id`, `webhook_id`, and mapping definition THEN the system SHALL create the mapping using the same validation `SaveEventMapping` already applies.
3. IF the event mapping's target table or column is unknown THEN the system SHALL reject with the same `ErrUnknownTargetTable`/`ErrUnknownTargetColumn` errors the REST endpoint already returns.
4. IF the event mapping conflicts with an existing mapping for the same `event_type_value` THEN the system SHALL reject with the same 409 `ErrMappingConflict` the REST endpoint already returns, and SHALL NOT overwrite the existing mapping.
5. IF the caller's role fails the same authorization check `CreateWebhook`/`SaveEventMapping`'s REST handlers already apply, or the app/webhook doesn't exist/isn't visible to the caller THEN the system SHALL return the same forbidden/not-found error.

**Independent Test**: Create a webhook via `orbit_create_webhook`; register an event mapping to a known table/column via `orbit_save_webhook_event_mapping`; confirm both appear via the Dashboard's Webhooks page; attempt a conflicting second mapping for the same `event_type_value` and confirm the 409 rejection leaves the first mapping intact.

---

## Edge Cases

- IF `table_name` doesn't exist on the given `app_id` THEN `orbit_add_table_column` and `orbit_add_table_index` SHALL both return a not-found error, mirroring `UpdateTableRLSModeForUser`'s existing `findAppTableByName` check.
- IF the merged column/index list fails `h.prov.Apply` at the database level for a reason validation didn't catch (see Assumptions row on FK apply-time failure) THEN the system SHALL leave the table's stored metadata unchanged — the store write only happens after `Apply` succeeds, matching `UpdateAppTable`'s existing "provision before persist" ordering (`handler.go:1331-1346`).
- IF a `references.on_delete` value is omitted on a new column's reference THEN the system SHALL apply whatever default `config.ValidateTables`/`ColumnConfig` already defines for an absent `on_delete` today — no new default introduced by this spec.
- WHEN the caller is a superadmin or holds `CanReadAnyApp`/write-equivalent THEN every tool in this spec SHALL behave exactly as the equivalent identity's REST call would — no additional restriction introduced by the MCP layer.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ------ | ------ |
| MSMT-01 | P1: Agent adds a column | T1, T2 | Verified |
| MSMT-02 | P1: Agent adds a column | T1, T2 | Verified |
| MSMT-03 | P1: Agent adds a column | T1, T2 | Verified |
| MSMT-04 | P1: Agent adds a column | T1, T2 | Verified |
| MSMT-05 | P1: Agent adds a column | T1, T2 | Verified |
| MSMT-06 | P1: Agent adds a column | T1, T2 | Verified |
| MSMT-07 | P2: Agent adds an index | T3, T4 | Verified |
| MSMT-08 | P2: Agent adds an index | T3, T4 | Verified |
| MSMT-09 | P2: Agent adds an index | T3, T4 | Verified |
| MSMT-10 | P3: Agent creates webhook + mapping | T5, T6 | Verified |
| MSMT-11 | P3: Agent creates webhook + mapping | T7, T8 | Verified |

**ID format:** `MSMT-[NUMBER]` (MCP Safe Mutation Tools)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 11 total, 11 mapped to tasks, 0 unmapped — verified PASS, see `validation.md` (diff range `4b54100..8e52c1c`)

---

## Success Criteria

- [x] `orbit_add_table_column`, `orbit_add_table_index`, `orbit_create_webhook`, `orbit_save_webhook_event_mapping` are all callable via a real MCP client against a running server.
- [x] A test proves that calling `orbit_add_table_column` twice in a row (adding two different columns) results in a table with all original columns plus both new ones — the concrete regression test for the orphaned-column risk that motivated this spec.
- [x] Zero existing column, index, or webhook is ever modified or removed as a side effect of any tool in this spec.
- [x] Every new tool's authorization is proven to reject a caller without `CanWrite()` (or the webhook tools' equivalent) on the given `app_id`.
