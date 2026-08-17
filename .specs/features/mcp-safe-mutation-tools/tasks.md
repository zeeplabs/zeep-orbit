# MCP Safe Mutation Tools Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/mcp-safe-mutation-tools/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate commands: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`), §4 (error-string rule — no raw internal errors in HTTP responses). Testing convention sampled from `internal/dashboard/table_policies_foruser_test.go`, `internal/dashboard/webhooks_store_test.go`, `internal/mcpserver/tools_test.go`, and this feature's sibling `mcp-read-only-tools/tasks.md`'s own matrix: integration tests against a real ephemeral Postgres (no mocking layer exists for `db.Pool` anywhere in this codebase), `-race` enabled, real MCP-client round-trip for tool-registry tests.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| New `*ForUser` functions (`AddTableColumnForUser`, `AddTableIndexForUser`, `CreateWebhookForUser`, `SaveEventMappingForUser`) | integration (real Postgres) | Happy path; every distinct error branch from the Error Handling Strategy table (forbidden, not-found, duplicate-name sentinels, validation failures, webhook-specific sentinels, cross-app scoping); confirm untouched columns/indexes/webhooks survive a mutation unchanged (the concrete regression test for the orphaning risk that motivated this spec) | `internal/dashboard/*_test.go` (co-located with the function, same file as its existing store/handler tests) | `go test ./internal/dashboard/...` |
| Refactored REST handlers (`CreateWebhook`, `SaveEventMapping`) | integration | Existing tests for these handlers MUST keep passing unmodified (behavior-preserving-extraction bar, same standard `mcp-read-only-tools` set for its own handler extractions) | Existing `*_test.go` files, unchanged expectations | `go test ./internal/dashboard/...` |
| MCP tool registry additions (`internal/mcpserver/tools.go`) | integration, real MCP client roundtrip | Each of the 4 new tools callable via the SDK's own test client against a running server backed by real Postgres; authorization-failure path tested at the specific tier that tool actually enforces (`CanWrite()` for the two table tools, `CanManage()` for the two webhook tools — confirmed non-uniform in design.md's Tech Decisions, not a shared "same tier" assertion); duplicate-name/validation/conflict error paths surfaced as distinct tool errors, not a generic internal error | `internal/mcpserver/tools_test.go` | `go test ./internal/mcpserver/...` |
| New sentinels (`ErrColumnAlreadyExists`, `ErrIndexAlreadyExists`) | covered by the function's own integration test above | No standalone test — asserted as part of `AddTableColumnForUser`/`AddTableIndexForUser`'s error-branch coverage | n/a | n/a |
| `mapWriteError` new cases | covered by the MCP tool's own integration test above | No standalone test — asserted as part of each tool's error-path test | n/a | n/a |

**Coverage Expectation rationale**: this repo has zero mocking infrastructure for `db.Pool` — every existing backend test already runs against a real ephemeral Postgres. New tests follow the same convention.

## Gate Check Commands

> Generated from codebase — confirm before Execute.

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick | After a task that only touches `internal/dashboard` | `go test ./internal/dashboard/... -race` |
| Full | After a task that also touches `internal/mcpserver` | `go test ./internal/dashboard/... ./internal/mcpserver/... -race` |
| Build | After the final task, and before its last commit | `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && go test ./... -race` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Add-column path

```
T1 → T2
```

### Phase 2: Add-index path

```
T3 → T4
```

### Phase 3: Webhook creation path

```
T5 → T6
```

### Phase 4: Webhook event-mapping path

```
T7 → T8
```

---

## Task Breakdown

### T1: `AddTableColumnForUser` dashboard function

**What**: Add `ErrColumnAlreadyExists` sentinel and `AddTableColumnForUser(ctx, user, appID, tableName string, col config.ColumnConfig, ip string) (*AppTableRow, error)` to `internal/dashboard/handler.go`, mirroring `UpdateTableRLSModeForUser`'s exact composition: `GetApp` + `role.CanWrite()` → `findAppTableByName` (nil → `ErrNotFound`) → reject duplicate column name (→ `ErrColumnAlreadyExists`, no mutation) → force-clear `col.RenameFrom` → merge into a copy of `existingTable.Columns` → `validateTableInput`/`config.ValidateTables` against the merged table + `otherTables` → single-table `h.prov.Apply` → `apps_store.UpdateAppTable` (merged columns, existing indexes/RLS unchanged) → `h.reg.Register` refresh → `h.audit(..., "app.table_column.create", "app_table", ...)`.
**Where**: `internal/dashboard/handler.go` (new function + sentinel, co-located with `CreateAppTableForUser`/`UpdateTableRLSModeForUser`)
**Depends on**: None
**Reuses**: `GetApp`, `role.CanWrite()`, `findAppTableByName`, `validateTableInput`, `buildAppConfig`, `h.prov.Apply`, `apps_store.UpdateAppTable`, `h.reg.Register`, `h.audit`
**Requirement**: MSMT-01, MSMT-02, MSMT-03, MSMT-04, MSMT-05, MSMT-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `AddTableColumnForUser` adds the new column while leaving every other column, index, and RLS setting on the table unchanged (test: seed a 2-column table, add a 3rd, assert all 3 present with original 2 byte-for-byte unchanged)
- [x] A column with a valid `references` (existing table/column, PK/UNIQUE target) is created with that FK constraint applied in Postgres
- [x] Returns `ErrColumnAlreadyExists` (not a generic validation error) for a duplicate column name, and the table is left untouched (re-fetch and compare)
- [x] Returns the same `*ValidationError` `config.ValidateTables` already produces for a bad `references` target (nonexistent table/column, missing PK/UNIQUE, invalid `on_delete`), table left untouched
- [x] Returns `ErrForbidden` for a caller whose role fails `CanWrite()` (tested with a viewer role, not just "no membership")
- [x] Returns `ErrNotFound` for an invisible/nonexistent app, and a not-found result for a nonexistent `table_name`
- [x] Records `app.table_column.create` in the audit log on success
- [x] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `feat(dashboard): add AddTableColumnForUser additive column endpoint`

---

### T2: `orbit_add_table_column` MCP tool

**What**: Register `orbit_add_table_column` in `internal/mcpserver/tools.go` (new `registerAppConfigWriteTools` function), wrapping `AddTableColumnForUser` from T1 with `ip = "mcp"`, following the exact `orbit_create_table` registration pattern. Add a `case errors.Is(err, dashboard.ErrColumnAlreadyExists):` branch to `mapWriteError` returning a passthrough `"column already exists"` message.
**Where**: `internal/mcpserver/tools.go` (new `registerAppConfigWriteTools` function, called from `RegisterTools`; `mapWriteError` extended)
**Depends on**: T1
**Reuses**: `AddTableColumnForUser`, `dashboard.UserFromContext`, `mapWriteError`, `orbit_create_table`'s registration shape (`tools.go:200-218`)
**Requirement**: MSMT-01, MSMT-02, MSMT-03, MSMT-04, MSMT-05, MSMT-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Calling `orbit_add_table_column` twice in a row (two different columns) results in a table with all original columns plus both new ones (the concrete regression test for the orphaned-column risk, per spec.md's Success Criteria)
- [x] Returns a distinct `"column already exists"` tool error (not a generic internal error) for a duplicate name
- [x] Returns forbidden for a caller whose role fails `CanWrite()` on that app (explicit tier test)
- [x] Returns not-found for a nonexistent/invisible app or table
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_add_table_column safe mutation tool`

---

### T3: `AddTableIndexForUser` dashboard function

**What**: Add `ErrIndexAlreadyExists` sentinel and `AddTableIndexForUser(ctx, user, appID, tableName string, idx config.IndexConfig, ip string) (*AppTableRow, error)` to `internal/dashboard/handler.go`, mirroring T1's shape for indexes: `GetApp` + `role.CanWrite()` → `findAppTableByName` → reject duplicate index name (→ `ErrIndexAlreadyExists`) → merge into a copy of `existingTable.Indexes` (columns unchanged) → `validateTableInput`/`config.ValidateTables` (rejects unknown index-target columns via the existing `validateIndexes` check, no new logic needed) → single-table `h.prov.Apply` (uses current blocking `CREATE INDEX IF NOT EXISTS`, per design.md's Tech Decisions — `CONCURRENTLY` explicitly deferred) → `apps_store.UpdateAppTable` (existing columns, merged indexes) → `h.reg.Register` → `h.audit(..., "app.table_index.create", ...)`.
**Where**: `internal/dashboard/handler.go` (new function + sentinel)
**Depends on**: None
**Reuses**: Same primitives as T1, applied to `Indexes` instead of `Columns`; `validateIndexes` (`internal/config/validate.go:191-214`) for the unknown-column check
**Requirement**: MSMT-07, MSMT-08, MSMT-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `AddTableIndexForUser` adds the new index while leaving every other index and all columns unchanged
- [x] Returns `ErrIndexAlreadyExists` for a duplicate index name, table left untouched
- [x] Returns a validation error for an index referencing a column that doesn't exist on the table, table left untouched
- [x] Returns `ErrForbidden` for a caller whose role fails `CanWrite()`
- [x] Returns not-found for an invisible/nonexistent app or table
- [x] Records `app.table_index.create` in the audit log on success
- [x] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `feat(dashboard): add AddTableIndexForUser additive index endpoint`

---

### T4: `orbit_add_table_index` MCP tool

**What**: Register `orbit_add_table_index` in `registerAppConfigWriteTools` (from T2), wrapping `AddTableIndexForUser` from T3. Tool description explicitly discloses that index creation briefly blocks writes to the target table (per spec.md P2 AC6 / design.md's Tech Decisions on `CREATE INDEX CONCURRENTLY`). Add a `case errors.Is(err, dashboard.ErrIndexAlreadyExists):` branch to `mapWriteError`.
**Where**: `internal/mcpserver/tools.go` (`registerAppConfigWriteTools`, `mapWriteError` extended)
**Depends on**: T3
**Reuses**: `AddTableIndexForUser`, `mapWriteError`
**Requirement**: MSMT-07, MSMT-08, MSMT-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Adds an index on an existing column; index is reflected in the schema afterward
- [x] Returns `"index already exists"` distinct tool error for a duplicate name
- [x] Returns a validation tool error for an index referencing a nonexistent column
- [x] Returns forbidden for a caller whose role fails `CanWrite()`
- [x] Tool description text includes the blocking-write disclosure
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_add_table_index safe mutation tool`

---

### T5: `CreateWebhookForUser` dashboard function + REST handler refactor

**What**: Add `CreateWebhookForUser(ctx, user, appID string, input webhooks.CreateWebhookInput, ip string) (WebhookRow, error)` to `internal/dashboard/webhooks_store.go`, replicating `webhookRBACGate`'s auth (`GetApp` + `role.CanManage()` — **not** `CanWrite()`, confirmed non-uniform tier per design.md) context-based instead of HTTP-based, then the same `Method`/`Name`/`EventTypePath` validation the REST handler does (`webhooks_handler.go:127-136`), then calling the existing `webhooks_store.CreateWebhook` store function as-is, then `h.audit(ctx, user.ID, user.Email, "webhook.create", "webhook", row.ID, app.Name+"/"+row.Name, nil, ip)` — reusing the REST handler's existing action string, not a new one. Refactor the `CreateWebhook` REST handler (`webhooks_handler.go:112`) to call this new function instead of inlining the same steps.
**Where**: `internal/dashboard/webhooks_store.go` (new function), `internal/dashboard/webhooks_handler.go:112` (refactor)
**Depends on**: None
**Reuses**: `GetApp`, `role.CanManage()`, `webhooks_store.CreateWebhook`, `h.audit`
**Requirement**: MSMT-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `CreateWebhookForUser` creates a webhook using the same validation `CreateWebhook`'s REST handler already applies, without affecting any other webhook on the app
- [x] Returns forbidden for a caller whose role fails `CanManage()` — dedicated test with an editor-role member (not admin), confirming the stricter tier this endpoint actually enforces vs. the table endpoints' `CanWrite()`
- [x] Returns not-found for an invisible/nonexistent app
- [x] Existing `CreateWebhook` REST handler test(s) still pass unmodified
- [x] Records `webhook.create` in the audit log on success (same action string the REST handler already uses)
- [x] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract CreateWebhookForUser shared operation function`

---

### T6: `orbit_create_webhook` MCP tool

**What**: Register `orbit_create_webhook` in `internal/mcpserver/tools.go` (new `registerOperationalWriteTools` function), wrapping `CreateWebhookForUser` from T5 with `ip = "mcp"`.
**Where**: `internal/mcpserver/tools.go` (new `registerOperationalWriteTools` function, called from `RegisterTools`)
**Depends on**: T5
**Reuses**: `CreateWebhookForUser`, `mapWriteError` (existing cases: `ErrForbidden`/`ErrNotFound`/`*ValidationError`)
**Requirement**: MSMT-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Creates a webhook for a caller who can manage the app; returned config matches what the Dashboard's Webhooks page would show
- [x] Returns forbidden for a caller whose role fails `CanManage()` on that app (explicit tier test, distinct from the table tools' `CanWrite()` tier test)
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_create_webhook safe mutation tool`

---

### T7: `SaveEventMappingForUser` dashboard function + REST handler refactor

**What**: Add `SaveEventMappingForUser(ctx, user, appID, webhookID string, def webhooks.EventMappingDef, ip string) (EventMappingRow, error)` to `internal/dashboard/webhooks_store.go`, composing `GetApp` + `role.CanManage()` + the same webhook-belongs-to-app scoping `getScopedWebhook`/`GetWebhookForUser` already enforce (cross-app-scoped `webhook_id` → not-found) + the existing `webhooks_store.SaveEventMapping` store function (propagating `ErrUnknownTargetTable`/`ErrUnknownTargetColumn`/`ErrMappingConflict` unchanged) + `h.audit(..., "webhook.mapping.save", "webhook_event_mapping", ...)` (existing action string). Refactor the `SaveEventMapping` REST handler (`webhooks_handler.go:376`) to call it. Add `mapWriteError` cases for `ErrUnknownTargetTable`, `ErrUnknownTargetColumn`, and `ErrMappingConflict` (passthrough messages — these are already safe, existing sentinels never wired into `mapWriteError` before this feature).
**Where**: `internal/dashboard/webhooks_store.go` (new function), `internal/dashboard/webhooks_handler.go:376` (refactor), `internal/mcpserver/tools.go` (`mapWriteError` extended)
**Depends on**: None
**Reuses**: `GetApp`, `role.CanManage()`, webhook cross-app scoping pattern, `webhooks_store.SaveEventMapping`, `h.audit`
**Requirement**: MSMT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `SaveEventMappingForUser` creates the mapping using the same validation `SaveEventMapping` already applies
- [ ] Returns `ErrUnknownTargetTable`/`ErrUnknownTargetColumn` for an unknown mapping target, matching the REST endpoint exactly
- [ ] Returns `ErrMappingConflict` for a conflicting `event_type_value`, and the existing mapping is left intact (re-fetch and compare)
- [ ] Returns not-found for a `webhook_id` belonging to a **different** app than the given `app_id` (explicit cross-app scoping test)
- [ ] Returns forbidden for a caller whose role fails `CanManage()`
- [ ] Existing `SaveEventMapping` REST handler test(s) still pass unmodified
- [ ] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract SaveEventMappingForUser shared operation function`

---

### T8: `orbit_save_webhook_event_mapping` MCP tool

**What**: Register `orbit_save_webhook_event_mapping` in `registerOperationalWriteTools` (from T6), wrapping `SaveEventMappingForUser` from T7.
**Where**: `internal/mcpserver/tools.go` (`registerOperationalWriteTools`)
**Depends on**: T6, T7
**Reuses**: `SaveEventMappingForUser`, `mapWriteError`
**Requirement**: MSMT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Registers an event mapping to a known table/column for a caller who can manage the app
- [ ] Returns distinct tool errors for unknown target table/column, and for a conflicting mapping (409-equivalent), leaving the first mapping intact
- [ ] Returns not-found for a `webhook_id` belonging to a different app
- [ ] Returns forbidden for a caller whose role fails `CanManage()`
- [ ] Gate passes: `go test ./internal/mcpserver/... -race`
- [ ] Full feature build gate passes: `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && go test ./... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: build

**Commit**: `feat(mcp): add orbit_save_webhook_event_mapping safe mutation tool`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

Phase 1:  T1 ------→ T2
Phase 2:  T3 ------→ T4
Phase 3:  T5 ------→ T6
Phase 4:  T7 ------→ T8
Phase 4:  T6 ------→ T8
```

Execution is strictly sequential within a phase — no intra-phase parallelism. All 8 tasks pack into a single ~8-task batch (no sub-agent offer needed — fits inline per the skill's ≤~8 threshold).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: `AddTableColumnForUser` + sentinel | 1 function + 1 sentinel | ✅ Granular |
| T2: `orbit_add_table_column` tool | 1 tool registration + 1 `mapWriteError` case | ✅ Granular |
| T3: `AddTableIndexForUser` + sentinel | 1 function + 1 sentinel | ✅ Granular |
| T4: `orbit_add_table_index` tool | 1 tool registration + 1 `mapWriteError` case | ✅ Granular |
| T5: `CreateWebhookForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T6: `orbit_create_webhook` tool | 1 tool registration | ✅ Granular |
| T7: `SaveEventMappingForUser` + REST refactor | 1 function + its 1 call site + 3 `mapWriteError` cases (same error family) | ⚠️ OK — cohesive (3 sentinels from the same store function, same task per "2-3 related things = OK if cohesive") |
| T8: `orbit_save_webhook_event_mapping` tool | 1 tool registration | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ----------------------- | -------------- | ------ |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | None | (start of Phase 2) | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | None | (start of Phase 3) | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | None | (start of Phase 4) | ✅ Match |
| T8 | T6, T7 | T7 → T8, T6 → T8 | ✅ Match |

No task depends on a task in a later phase. All dependencies point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ---------------------------- | ---------------- | ---------- | ------ |
| T1 | `*ForUser` function | integration (Postgres) | integration | ✅ OK |
| T2 | MCP tool registry | integration (MCP roundtrip) | integration | ✅ OK |
| T3 | `*ForUser` function | integration | integration | ✅ OK |
| T4 | MCP tool registry | integration | integration | ✅ OK |
| T5 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T6 | MCP tool registry | integration | integration | ✅ OK |
| T7 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T8 | MCP tool registry | integration | integration | ✅ OK |

No `Tests: none` used anywhere — every task modifies a code layer the matrix requires integration coverage for.

---

## Sub-Agent Offer

8 tasks fits a single task-budgeted batch (≤ ~8 tasks) — no sub-agent dispatch needed, execute inline.
