# Add/Remove Foreign Key on an Existing Column Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/column-foreign-key/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 ("Before considering any change done: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l <changed files>`"). No frontend files are touched by this feature (per design: no new REST HTTP route, no new UI form — conforms to AD-002 precedent), so the frontend gates (`npx tsc -b`, `npm run build`) do not apply here.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Config value method (`ReferenceConfig.Equal`) | unit | All branches: both nil, one nil, equal, differing on each field | `internal/config/types_test.go` (new file — sampled `internal/config/validate_test.go` for style) | `go test ./internal/config/...` |
| Provisioner error type (`ForeignKeyViolationError`) | unit | `Error()` message shape, `Unwrap()`/`Cause` never leaks into `Error()` — mirrors `TestTypeChangeError_DoesNotLeakCause` | `internal/provisioner/errors_test.go` (append) | `go test ./internal/provisioner/...` |
| Provisioner DDL functions (`CheckForeignKeyColumnTypesMatch`, `AddColumnForeignKey`, `DropColumnForeignKey`) | integration (live Postgres, `TEST_DATABASE_URL`-gated — sampled `internal/provisioner/table_test.go`) | 1:1 to spec ACs CFK-05/06/09/10 + edge case (stale-schema convergence on remove) | `internal/provisioner/table_test.go` (append) | `go test ./internal/provisioner/...` |
| Dashboard handler layer (`AddColumnForeignKeyForUser`, `RemoveColumnForeignKeyForUser`, `UpdateAppTable` full-replace guard) | integration (`TEST_DATABASE_URL`-gated — sampled `internal/dashboard/apps_table_column_foruser_test.go`, `internal/dashboard/apps_handler_test.go`) | All routes in scope: happy path + every P1 error path (already-has-FK, no-FK-to-remove, forbidden, not-found, type-mismatch, full-replace rejection, full-replace new-column-allowed) | `internal/dashboard/apps_column_foreign_key_foruser_test.go` (new), `internal/dashboard/apps_handler_test.go` (append) | `go test ./internal/dashboard/...` |
| MCP tools (`orbit_add_column_foreign_key`, `orbit_remove_column_foreign_key`) | integration (sampled `internal/mcpserver/tools_add_table_column_test.go`) | Happy path + forbidden + the two new sentinel error mappings each | `internal/mcpserver/tools_add_column_foreign_key_test.go`, `internal/mcpserver/tools_remove_column_foreign_key_test.go` (new) | `go test ./internal/mcpserver/...` |
| AI client wiring (`PlanForeignKeyOp`, `PlanRemoveForeignKeyOp`, `parseEditOperation`, `editToolDefs`) | unit (sampled `internal/dashboard/ai/client_edit_test.go`) | One round-trip test per new tool call (mirrors `TestCallEditModel_AddReference`) + malformed-arguments case + updated tool-count assertion | `internal/dashboard/ai/client_edit_test.go` (append) | `go test ./internal/dashboard/ai/...` |
| Edit-chat confirm wiring (`applyEditOperation`, `respondEditChatConfirmError`, `editChatSystemPromptFor` content) | integration (sampled `internal/dashboard/ai_edit_chat_handlers_test.go`) | One confirm-and-apply test per new operation kind (mirrors `TestEditChatConfirm_AddReference`) + one test per new error path + one prompt-content assertion | `internal/dashboard/ai_edit_chat_handlers_test.go` (append) | `go test ./internal/dashboard/...` |
| Documentation (`CHANGELOG.md`) | none | - (build gate only) | `CHANGELOG.md` | build gate only |

**Coverage Expectation values** used above follow the strong defaults (domain/business logic = all branches, 1:1 to ACs; route/integration layers = happy + every listed edge/error path) since `AGENTS.md` states the gate commands but not a per-layer depth target.

## Gate Check Commands

> Generated from `AGENTS.md` §3 and `Makefile`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a task touching only `internal/config` or `internal/provisioner/errors.go` (pure unit, no DB) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./internal/config/... ./internal/provisioner/...` |
| Full | After a task touching `internal/provisioner` DDL, `internal/dashboard`, or `internal/mcpserver` (needs live Postgres) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./internal/provisioner/... ./internal/dashboard/... ./internal/mcpserver/...` |
| Build | End of every phase, and before the feature is declared done | `go build ./... && go vet ./... && gofmt -l $(git diff --name-only develop -- '*.go') && go test ./...` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Provisioner & Config Primitives

```
T1 (isolated)
T2 → T4
T3 (isolated)
T5 (isolated)
```

### Phase 2: Dashboard Handler Layer

```
T3 → T6
T4 → T6
T5 → T7
T1 → T8
```

### Phase 3: MCP Tools

```
T6 → T9
T7 → T10
```

### Phase 4: AI Chat Integration

```
T11 → T12
T6 → T13
T7 → T13
T9 → T13
T10 → T13
T12 → T13
```

### Phase 5: Documentation

```
T13 → T14
```

---

## Task Breakdown

### T1: Add `ReferenceConfig.Equal` method

**What**: Add a nil-safe `Equal(other *ReferenceConfig) bool` method to `config.ReferenceConfig`, comparing `Table`/`Column`/`OnDelete`.
**Where**: `internal/config/types.go`
**Depends on**: None
**Reuses**: N/A (new, self-contained value method)
**Requirement**: CFK-19, CFK-20, CFK-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Equal` returns true for both-nil, true for identical values, false for any single differing field, false for exactly-one-nil
- [x] Unit tests added in `internal/config/types_test.go` (new file) covering all 4 branch classes above
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/config/types.go internal/config/types_test.go && go test ./internal/config/...`
- [x] Test count: 4+ new test cases pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(column-foreign-key): add ReferenceConfig.Equal for change detection`

---

### T2: Add `ForeignKeyViolationError`

**What**: Add `ForeignKeyViolationError` struct (`Column`, `Detail`, `Cause`) with `Error()`/`Unwrap()`, mirroring `TypeChangeError`.
**Where**: `internal/provisioner/errors.go`
**Depends on**: None
**Reuses**: `TypeChangeError`'s exact shape (`internal/provisioner/errors.go`)
**Requirement**: CFK-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Error()` includes `Column` and `Detail` in its message, never leaks internal Go error formatting
- [x] `Unwrap()` returns `Cause` without exposing it through `Error()` — mirrors `TestTypeChangeError_DoesNotLeakCause`
- [x] Unit tests appended to `internal/provisioner/errors_test.go`
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/provisioner/errors.go internal/provisioner/errors_test.go && go test ./internal/provisioner/...`
- [x] Test count: 2+ new test cases pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(column-foreign-key): add ForeignKeyViolationError type`

---

### T3: Add `Provisioner.CheckForeignKeyColumnTypesMatch`

**What**: New method comparing the real physical Postgres type (`udt_name`) of an existing column against the real physical type of a target table/column (including the `_auth_users` special case), returning a descriptive error naming both real types on mismatch, `nil` on match.
**Where**: `internal/provisioner/table.go`
**Depends on**: None
**Reuses**: `fetchExistingColumns` (`internal/provisioner/table.go:320-340`), called twice
**Requirement**: CFK-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Returns `nil` when source and target column real types match (both regular-table and `_auth_users` targets)
- [x] Returns a non-nil error naming both real types when they differ
- [x] Integration tests appended to `internal/provisioner/table_test.go` (`TEST_DATABASE_URL`-gated): match case, mismatch case, `_auth_users` target case
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/provisioner/table.go internal/provisioner/table_test.go && go test ./internal/provisioner/...`
- [x] Test count: 3+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add physical column-type compatibility check`

---

### T4: Add `Provisioner.AddColumnForeignKey`

**What**: New method running `ALTER TABLE ... ADD FOREIGN KEY (col) REFERENCES target(col) ON DELETE ...` on an existing column (constraint left unnamed, matching Postgres's own auto-naming convention), catching Postgres error `23503` into `*ForeignKeyViolationError`.
**Where**: `internal/provisioner/table.go`
**Depends on**: T2 (`ForeignKeyViolationError`)
**Reuses**: `onDeleteSQL` (`internal/provisioner/table.go:50-61`)
**Requirement**: CFK-01, CFK-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Successfully adds a working FK constraint on an existing column with valid, non-orphaned data
- [ ] Orphaned-row insert followed by an add attempt returns `*ForeignKeyViolationError` with the Postgres `Detail` text preserved
- [ ] Integration tests appended to `internal/provisioner/table_test.go`: success case, orphaned-row rejection case, `on_delete` clause applied correctly (verified via a cascading delete)
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/provisioner/table.go internal/provisioner/table_test.go && go test ./internal/provisioner/...`
- [ ] Test count: 3+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add Provisioner.AddColumnForeignKey`

---

### T5: Add `Provisioner.DropColumnForeignKey`

**What**: New method that looks up the real FK constraint on a column via `information_schema` (never by naming convention) and drops it; returns `found=false, err=nil` when no constraint exists.
**Where**: `internal/provisioner/table.go`
**Depends on**: None
**Reuses**: The three-table `information_schema` join style from `checkDependents` (`internal/provisioner/table.go:213-236`), filter direction flipped plus a `column_name` filter
**Requirement**: CFK-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Drops an existing FK constraint on a column and confirms it no longer appears in `information_schema`
- [ ] Returns `found=false, err=nil` (no error) when the column has no FK constraint
- [ ] Correctly finds and drops a constraint even when it was **not** named via the `<table>_<column>_fkey` convention (test creates one with an explicit custom name via raw SQL, confirms catalog lookup still finds it)
- [ ] Integration tests appended to `internal/provisioner/table_test.go`
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/provisioner/table.go internal/provisioner/table_test.go && go test ./internal/provisioner/...`
- [ ] Test count: 3+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add Provisioner.DropColumnForeignKey`

---

### T6: Add `Handler.AddColumnForeignKeyForUser`

**What**: New `*ForUser` handler (sentinel `ErrColumnAlreadyHasReference` + fetch → mutate-one-field → `validateTableInput` → `CheckForeignKeyColumnTypesMatch` → `AddColumnForeignKey` → persist → refresh registry → audit), mirroring `AddTableColumnForUser`'s shape.
**Where**: `internal/dashboard/handler.go`
**Depends on**: T3, T4
**Reuses**: `AddTableColumnForUser`'s control flow (`internal/dashboard/handler.go:1492-1555`)
**Requirement**: CFK-01, CFK-02, CFK-03, CFK-04, CFK-05, CFK-06, CFK-07, CFK-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Successfully adds a FK to an existing column and persists `References` in the stored schema only after the DDL succeeds
- [ ] Rejects with `ErrColumnAlreadyHasReference` when the column already has one
- [ ] Rejects with a `*ValidationError` (invalid target table/column, bad `on_delete`, `_auth_users` non-uuid rule, reference cycle) — reusing `config.ValidateTables`'s existing behavior
- [ ] Rejects with a `*ValidationError` on physical type mismatch (from T3)
- [ ] Rejects with `*provisioner.ForeignKeyViolationError` propagated on orphaned rows (from T4)
- [ ] Rejects with `ErrForbidden` for a caller without `CanWrite()`, making no schema change
- [ ] Records an audit log entry (`app.table_column.add_foreign_key`) on success
- [ ] Integration tests added in `internal/dashboard/apps_column_foreign_key_foruser_test.go` (new file) covering every path above
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/dashboard/handler.go internal/dashboard/apps_column_foreign_key_foruser_test.go && go test ./internal/dashboard/...`
- [ ] Test count: 6+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add AddColumnForeignKeyForUser handler`

---

### T7: Add `Handler.RemoveColumnForeignKeyForUser`

**What**: New `*ForUser` handler (sentinel `ErrColumnHasNoReference` + fetch → `DropColumnForeignKey` → clear field → persist → refresh registry → audit), including the stale-schema self-healing edge case (drop reports `found=false` but stored `References` is still cleared).
**Where**: `internal/dashboard/handler.go`
**Depends on**: T5
**Reuses**: Same skeleton as T6, minus the pre-DDL validation step
**Requirement**: CFK-09, CFK-10, CFK-11, CFK-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Successfully removes a FK from an existing column and clears `References` in the stored schema only after the DDL succeeds (or after confirming no constraint exists — self-healing case)
- [ ] Rejects with `ErrColumnHasNoReference` when the column's stored schema shows no `References`
- [ ] Rejects with `ErrForbidden` for a caller without `CanWrite()`, making no schema change
- [ ] Records an audit log entry (`app.table_column.remove_foreign_key`) on success
- [ ] Integration tests appended to `internal/dashboard/apps_column_foreign_key_foruser_test.go` covering every path above, including the stale-schema convergence case
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/dashboard/handler.go internal/dashboard/apps_column_foreign_key_foruser_test.go && go test ./internal/dashboard/...`
- [ ] Test count: 4+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add RemoveColumnForeignKeyForUser handler`

---

### T8: Close the `PUT /tables/{id}` silent-no-op gap

**What**: Insert a guard in `UpdateAppTable` (full-replace) that rejects the whole request with HTTP 400 if any column present both before and after the request has a changed `References` value (via `ReferenceConfig.Equal`), while continuing to allow `References` on a brand-new column in the same request.
**Where**: `internal/dashboard/handler.go` (modify `UpdateAppTable`, ~line 1332, right after `decodeJSONBody`)
**Depends on**: T1
**Reuses**: `existingTable.Columns` (already fetched by this handler), `ReferenceConfig.Equal` (T1)
**Requirement**: CFK-19, CFK-20, CFK-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] A request changing `References` (add, remove, or retarget) on a column that existed before the request is rejected with HTTP 400, and nothing is persisted (stored schema and DDL both untouched)
- [ ] A request that only sets `References` on a column that is brand-new in this same request still succeeds exactly as before
- [ ] A request that changes non-`References` fields on an existing column, with `References` on shared columns left byte-identical, still succeeds exactly as before
- [ ] Integration tests appended to `internal/dashboard/apps_handler_test.go` covering all 3 cases above
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/dashboard/handler.go internal/dashboard/apps_handler_test.go && go test ./internal/dashboard/...`
- [ ] Test count: 3+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `fix(column-foreign-key): reject silent no-op References change in full-replace`

---

### T9: Add `orbit_add_column_foreign_key` MCP tool

**What**: Register the MCP tool wrapping `AddColumnForeignKeyForUser`, and extend `mapWriteError` with `ErrColumnAlreadyHasReference` and `*provisioner.ForeignKeyViolationError` cases.
**Where**: `internal/mcpserver/tools.go` (modify `registerAppConfigWriteTools` + `mapWriteError`)
**Depends on**: T6
**Reuses**: `orbit_add_table_column`'s registration shape (`internal/mcpserver/tools.go:315-326`)
**Requirement**: CFK-13, CFK-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Tool call succeeds end-to-end against a test app (matches T6's happy path through the MCP layer)
- [ ] A caller without `CanWrite()` gets the same class of rejection the REST-equivalent handler returns
- [ ] `ErrColumnAlreadyHasReference` and `*provisioner.ForeignKeyViolationError` map to their specific, safe-to-expose messages (not a generic internal error)
- [ ] Integration tests added in `internal/mcpserver/tools_add_column_foreign_key_test.go` (new file, mirrors `tools_add_table_column_test.go`)
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/mcpserver/tools.go internal/mcpserver/tools_add_column_foreign_key_test.go && go test ./internal/mcpserver/...`
- [ ] Test count: 3+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add orbit_add_column_foreign_key MCP tool`

---

### T10: Add `orbit_remove_column_foreign_key` MCP tool

**What**: Register the MCP tool wrapping `RemoveColumnForeignKeyForUser`, and extend `mapWriteError` with `ErrColumnHasNoReference`.
**Where**: `internal/mcpserver/tools.go` (modify `registerAppConfigWriteTools` + `mapWriteError`)
**Depends on**: T7
**Reuses**: Same registration shape as T9
**Requirement**: CFK-13, CFK-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Tool call succeeds end-to-end against a test app (matches T7's happy path through the MCP layer)
- [ ] A caller without `CanWrite()` gets the same class of rejection the REST-equivalent handler returns
- [ ] `ErrColumnHasNoReference` maps to its specific message
- [ ] Integration tests added in `internal/mcpserver/tools_remove_column_foreign_key_test.go` (new file)
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/mcpserver/tools.go internal/mcpserver/tools_remove_column_foreign_key_test.go && go test ./internal/mcpserver/...`
- [ ] Test count: 3+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): add orbit_remove_column_foreign_key MCP tool`

---

### T11: Wire `propose_add_foreign_key`/`propose_remove_foreign_key` into the AI client

**What**: Add `ai.PlanForeignKeyOp`/`PlanRemoveForeignKeyOp` types, extend `EditOperation` with two new `Kind` values and fields, extend `parseEditOperation`, `editProposalToolNames`, and `editToolDefs` with the two new tool schemas (and tighten `propose_add_reference`'s description to say "new column only").
**Where**: `internal/dashboard/ai/client.go`
**Depends on**: None
**Reuses**: The existing 6-operation pattern in the same file (`PlanReferenceOp`, `parseEditOperation`'s `propose_add_reference` case, `editToolDefs`'s `propose_add_reference` entry)
**Requirement**: CFK-15, CFK-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `parseEditOperation` correctly parses both new tool calls into `EditOperation{Kind: "add_foreign_key", ...}` / `{Kind: "remove_foreign_key", ...}`
- [ ] Malformed/incomplete arguments for either new tool return `ErrMalformedEditOp`
- [ ] `editToolDefs()` now advertises 10 tools total (8 propose_* + 2 read-only); `editProposalToolNames` recognizes both new names
- [ ] `propose_add_reference`'s tool description no longer implies it should ever be used for an existing column's FK (still says "never use this for a column that already exists" — but now that the model has a real alternative, mention `propose_add_foreign_key` by name)
- [ ] Unit tests appended to `internal/dashboard/ai/client_edit_test.go`: one round-trip test per new tool call (mirrors `TestCallEditModel_AddReference`), one malformed-arguments test, `TestEditToolDefs_IncludesAllSixProposalsPlusReadTools` renamed/updated to assert all 8 propose_* tools
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/dashboard/ai/client.go internal/dashboard/ai/client_edit_test.go && go test ./internal/dashboard/ai/...`
- [ ] Test count: 3+ new/updated test cases pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(column-foreign-key): add propose_add/remove_foreign_key to the edit-chat client`

---

### T12: Update `editChatSystemPromptFor` routing guidance

**What**: Replace the prompt's current instruction to decline a FK request on an existing column with routing guidance: new column → `propose_add_reference`; existing column, adding a FK → `propose_add_foreign_key`; existing column, removing a FK → `propose_remove_foreign_key`.
**Where**: `internal/dashboard/ai_edit_chat_handlers.go` (`editChatSystemPromptTemplate` constant)
**Depends on**: T11
**Reuses**: The existing prompt structure/tone (unchanged everywhere else)
**Requirement**: CFK-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] The prompt text no longer instructs the model to decline a FK request on an existing column
- [ ] The prompt text names all three relevant tools and when to use each (new column / add-to-existing / remove-from-existing)
- [ ] A test asserts the rendered prompt (`editChatSystemPromptFor(appName)`) contains `propose_add_foreign_key` and `propose_remove_foreign_key` and does not contain the old "decline" sentence — appended to `internal/dashboard/ai_edit_chat_handlers_test.go`
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/dashboard/ai_edit_chat_handlers.go internal/dashboard/ai_edit_chat_handlers_test.go && go test ./internal/dashboard/...`
- [ ] Test count: 1+ new test case passes (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): update edit-chat prompt to route existing-column FK requests`

---

### T13: Wire `add_foreign_key`/`remove_foreign_key` into `EditChatConfirm`

**What**: Add two `applyEditOperation` cases calling `AddColumnForeignKeyForUser`/`RemoveColumnForeignKeyForUser` with `ip="ai_chat"`, and extend `respondEditChatConfirmError` with `ErrColumnAlreadyHasReference`, `ErrColumnHasNoReference`, and an `errors.As` case for `*provisioner.ForeignKeyViolationError`.
**Where**: `internal/dashboard/ai_edit_chat_handlers.go`
**Depends on**: T6, T7, T9, T10, T12
**Reuses**: The existing `case "add_reference":` block's shape (`internal/dashboard/ai_edit_chat_handlers.go:367-384`) and `respondEditChatConfirmError`'s existing `errors.Is`/`errors.As` dispatch
**Requirement**: CFK-17, CFK-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Confirming a proposed `add_foreign_key` operation applies it through `AddColumnForeignKeyForUser` and returns the updated table
- [ ] Confirming a proposed `remove_foreign_key` operation applies it through `RemoveColumnForeignKeyForUser` and returns the updated table
- [ ] Each of the handler-layer error paths (already-has-FK, no-FK-to-remove, type mismatch, FK violation, invalid target) surfaces its own specific message through the chat, not a generic failure — mirrors AIEC-04's existing behavior
- [ ] Integration tests appended to `internal/dashboard/ai_edit_chat_handlers_test.go`, mirroring `TestEditChatConfirm_AddReference`: one happy-path test per new kind, one test per new error path
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/dashboard/ai_edit_chat_handlers.go internal/dashboard/ai_edit_chat_handlers_test.go && go test ./internal/dashboard/...`
- [ ] Test count: 6+ new test cases pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(column-foreign-key): wire add/remove foreign key into EditChatConfirm`

---

### T14: Update CHANGELOG.md

**What**: Add an `## [Unreleased]` entry documenting the new add/remove-FK capability (REST handler layer + MCP tools + Edit-with-AI chat) and the full-replace silent-no-op fix, per `AGENTS.md` §6.
**Where**: `CHANGELOG.md`
**Depends on**: T13 (execution is strictly sequential — its completion implies every earlier task is done too)
**Reuses**: The existing `[Unreleased]` section's `### Added`/`### Fixed` structure

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] An `### Added` entry describes the new add/remove-FK capability across all three surfaces, following the existing entries' voice/detail level
- [ ] A `### Fixed` entry describes the closed `PUT /tables/{id}` silent-no-op gap
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./...` (full repo, confirming nothing broke across the whole feature)

**Tests**: none
**Gate**: build

**Commit**: `docs(column-foreign-key): update CHANGELOG for add/remove FK feature`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5

Phase 1: T1 (isolated)
Phase 1: T2 → T4
Phase 1: T3 (isolated)
Phase 1: T5 (isolated)
Phase 2: T3 → T6
Phase 2: T4 → T6
Phase 2: T5 → T7
Phase 2: T1 → T8
Phase 3: T6 → T9
Phase 3: T7 → T10
Phase 4: T11 → T12
Phase 4: T6 → T13
Phase 4: T7 → T13
Phase 4: T9 → T13
Phase 4: T10 → T13
Phase 4: T12 → T13
Phase 5: T13 → T14
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add `ReferenceConfig.Equal` | 1 method, 1 file | ✅ Granular |
| T2: Add `ForeignKeyViolationError` | 1 type, 1 file | ✅ Granular |
| T3: Add `CheckForeignKeyColumnTypesMatch` | 1 function, 1 file | ✅ Granular |
| T4: Add `AddColumnForeignKey` | 1 function, 1 file | ✅ Granular |
| T5: Add `DropColumnForeignKey` | 1 function, 1 file | ✅ Granular |
| T6: Add `AddColumnForeignKeyForUser` | 1 function (+ 1 sentinel error), 1 file | ✅ Granular |
| T7: Add `RemoveColumnForeignKeyForUser` | 1 function (+ 1 sentinel error), 1 file | ✅ Granular |
| T8: Close full-replace silent-no-op gap | 1 guard clause, 1 file | ✅ Granular |
| T9: Add `orbit_add_column_foreign_key` | 1 tool registration + `mapWriteError` cases, 1 file | ✅ Granular |
| T10: Add `orbit_remove_column_foreign_key` | 1 tool registration + `mapWriteError` case, 1 file | ✅ Granular |
| T11: AI client wiring | 2 types + 4 tightly-coupled function extensions, 1 file | ⚠️ OK if cohesive — see note |
| T12: Prompt routing guidance | 1 constant's text, 1 file | ✅ Granular |
| T13: `EditChatConfirm` wiring | 2 switch cases + error-mapping cases, 1 file | ✅ Granular |
| T14: CHANGELOG update | 1 file | ✅ Granular |

**Note on T11**: `PlanForeignKeyOp`/`PlanRemoveForeignKeyOp` plus the four call sites that parse/advertise them (`EditOperation`, `parseEditOperation`, `editProposalToolNames`, `editToolDefs`) cannot be split without leaving the package in a non-compiling intermediate state — they are one cohesive round-trip (a tool call must be advertised, parseable, and typed together, or the round-trip test for either new operation cannot run at all). This mirrors how the original 6 operations were added together in `ai-edit-chat`'s own T5, per that feature's `design.md`.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (isolated node, Phase 1) | ✅ Match |
| T2 | None | T2 → T4 (source, no incoming) | ✅ Match |
| T3 | None | (isolated node, Phase 1; also T3 → T6 as a source) | ✅ Match |
| T4 | T2 | T2 → T4 | ✅ Match |
| T5 | None | (isolated node, Phase 1; also T5 → T7 as a source) | ✅ Match |
| T6 | T3, T4 | T3 → T6, T4 → T6 | ✅ Match |
| T7 | T5 | T5 → T7 | ✅ Match |
| T8 | T1 | T1 → T8 | ✅ Match |
| T9 | T6 | T6 → T9 | ✅ Match |
| T10 | T7 | T7 → T10 | ✅ Match |
| T11 | None | T11 → T12 (source, no incoming) | ✅ Match |
| T12 | T11 | T11 → T12 | ✅ Match |
| T13 | T6, T7, T9, T10, T12 | T6 → T13, T7 → T13, T9 → T13, T10 → T13, T12 → T13 | ✅ Match |
| T14 | T13 | T13 → T14 | ✅ Match |

**Rules check**: every `Depends on` points backward (earlier phase) or to an earlier task in the same phase — no forward-phase dependency exists. Every diagram arrow has a matching `Depends on` entry and vice versa. ✅

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Config value method | unit | unit | ✅ OK |
| T2 | Provisioner error type | unit | unit | ✅ OK |
| T3 | Provisioner DDL function | integration | integration | ✅ OK |
| T4 | Provisioner DDL function | integration | integration | ✅ OK |
| T5 | Provisioner DDL function | integration | integration | ✅ OK |
| T6 | Dashboard handler layer | integration | integration | ✅ OK |
| T7 | Dashboard handler layer | integration | integration | ✅ OK |
| T8 | Dashboard handler layer | integration | integration | ✅ OK |
| T9 | MCP tools | integration | integration | ✅ OK |
| T10 | MCP tools | integration | integration | ✅ OK |
| T11 | AI client wiring | unit | unit | ✅ OK |
| T12 | Edit-chat confirm wiring (prompt) | integration | integration | ✅ OK |
| T13 | Edit-chat confirm wiring | integration | integration | ✅ OK |
| T14 | Documentation (no code layer) | none (docs) | none | ✅ OK |

No violations - every task's `Tests` field matches the layer it creates/modifies in the Test Coverage Matrix.
