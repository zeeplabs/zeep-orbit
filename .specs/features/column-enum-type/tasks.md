# column-enum-type Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/column-enum-type/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 ("Before considering any change done" — `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`; frontend `npx tsc -b`, `npm run build`; i18n JSON validation).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `config` (ColumnConfig, validation) | unit | All branches; 1:1 to CENUM-04/05/06 + every edge case (empty/dup/too-long/too-many, Default-membership) | `internal/config/validate_test.go` (existing file, add cases) | `go test ./internal/config/...` |
| `provisioner` (DDL, catalog lookup, errors) | unit + integration (real Postgres, existing pattern in `table_test.go`/`provisioner_test.go`) | All branches; 1:1 to CENUM-01/02/07/08/09/10/11/12 | `internal/provisioner/column_ddl_test.go` (existing, add cases), `internal/provisioner/table_test.go`, `internal/provisioner/errors_test.go` | `go test ./internal/provisioner/...` |
| `dashboard` handlers (`CreateAppTable`, `UpdateAppTable`, new `UpdateColumnEnumValuesForUser`) | integration (existing pattern: real handler + test DB, see `apps_column_foreign_key_foruser_test.go`) | All routes in scope: happy + every listed edge case + error paths | `internal/dashboard/apps_column_enum_values_foruser_test.go` (new, mirrors the FK test file), `internal/dashboard/apps_handler_test.go` (add enum-creation cases) | `go test ./internal/dashboard/...` |
| `server` (`HandleCreate`/`HandleUpdate` 23514 mapping) | integration (existing pattern in `handler_test.go`) | Happy path (valid enum write) + error path (out-of-set write, both insert and update) | `internal/server/handler_test.go` (add cases) | `go test ./internal/server/...` |
| `mcpserver` tools | integration (existing pattern: `tools_add_column_foreign_key_test.go`) | Every new/touched tool: happy + validation-error path | `internal/mcpserver/tools_test.go` (add enum cases to create_table/add_table_column), `internal/mcpserver/tools_update_column_enum_values_test.go` (new) | `go test ./internal/mcpserver/...` |
| Dashboard UI (`TableCard.tsx`) | none — no frontend test runner configured in this repo (`package.json` has only `build`, no `test` script) | build gate only | `internal/dashboard/ui/src/components/TableCard.tsx` | `cd internal/dashboard/ui && npx tsc -b && npm run build` |
| i18n JSON (`en.json`, `pt-BR.json`) | none | valid JSON, both locales updated together | `internal/dashboard/ui/src/locales/*.json` | `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json'))"` (+ `pt-BR.json`) |
| AI chat prompts (`ai_build_chat_handlers.go`, `ai_edit_chat_handlers.go`) | integration (existing pattern in the two `*_test.go` files) | Happy path: model proposes `enum` with `allowed_values` when asked for a status-like field | `internal/dashboard/ai_build_chat_handlers_test.go`, `internal/dashboard/ai_edit_chat_handlers_test.go` (add cases) | `go test ./internal/dashboard/...` |
| `CHANGELOG.md` | none | entry present under `[Unreleased]` | `CHANGELOG.md` | build gate only (manual read) |

## Gate Check Commands

> Generated from `Makefile` + `AGENTS.md` §3.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a Go-only task with unit/integration tests in one package | `go test ./internal/<package>/...` |
| Full | After a phase touching multiple Go packages | `go build ./... && go test ./... && go vet ./... && gofmt -l <changed .go files>` |
| Build | After a phase touching the Dashboard UI or i18n | Full gate, plus `cd internal/dashboard/ui && npx tsc -b && npm run build`, plus `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json'))"` and the same for `pt-BR.json` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Config layer

```
T1 → T2
```

### Phase 2: Provisioner layer

```
T3 → T4 → T5
```

### Phase 3: Dashboard handler layer (creation + guard + widen/narrow endpoint)

```
T6 → T7 → T8
```

### Phase 4: Write-path error mapping

```
T9
```

### Phase 5: MCP

```
T10 → T11
```

### Phase 6: Dashboard UI

```
T12 → T13
```

### Phase 7: AI chat prompts

```
T14 → T15
```

### Phase 8: Docs

```
T16
```

---

## Task Breakdown

### T1: Add `AllowedValues` to `ColumnConfig`

**Status**: ✅ Complete

**What**: Add `AllowedValues []string` field to `config.ColumnConfig`, JSON/YAML tag `allowed_values,omitempty`.
**Where**: `internal/config/types.go`
**Depends on**: None
**Reuses**: `ReferenceConfig *ReferenceConfig` field pattern already on the same struct.
**Requirement**: CENUM-01, CENUM-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `ColumnConfig.AllowedValues []string` field added with correct tags
- [ ] `go build ./...` passes

**Tests**: none (struct field addition, no branching logic — covered by config's Coverage Expectation "none for entity/config shape changes with no logic")
**Gate**: quick (`go build ./internal/config/...`)

---

### T2: `config.ValidateEnumValues` + wiring into `ValidateTables`/`validateDefault`

**Status**: ✅ Complete

**What**: New exported `ValidateEnumValues(values []string) error` (caps: 1-50 values, each 1-100 chars, no exact-match duplicates); call it from `ValidateTables`'s per-column loop when `col.Type == "enum"`; add `case "enum":` to `validateDefault` checking `col.Default` is a member of `col.AllowedValues`.
**Where**: `internal/config/validate.go`
**Depends on**: T1
**Reuses**: `validateDefault`'s existing switch-per-type structure and error-message shape.
**Requirement**: CENUM-04, CENUM-05, CENUM-06 (edge cases: >50 values, empty/too-long/duplicate entries)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `ValidateEnumValues` rejects: empty list, >50 values, any entry empty, any entry >100 chars, any exact-match duplicate — each with a distinct, specific error message
- [ ] `ValidateTables` calls it for every `type: "enum"` column
- [ ] `validateDefault`'s new `case "enum":` rejects a `Default` not present in `AllowedValues`
- [ ] Free-text values (spaces, accents, a literal single quote) pass validation unchanged (escaping happens at DDL time, not here)
- [ ] Gate check passes: `go test ./internal/config/...`
- [ ] Test count: at least 8 new test cases (5 rejection cases for `ValidateEnumValues` + 1 acceptance case + 1 `Default`-not-member case + 1 free-text-acceptance case), no silent deletions

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add enum column type validation`

---

### T3: `pgType` enum mapping + `columnDDL` CHECK clause

**Status**: ✅ Complete

**What**: Add `case "enum": return "TEXT"` to `pgType`; add a branch in `columnDDL` that appends `CHECK ("col" IN ('v1', 'v2', ...))` for `enum` columns, escaping each value the same way `Default` is escaped (single quotes doubled).
**Where**: `internal/provisioner/table.go`
**Depends on**: T1
**Reuses**: `Default`'s escaping (`strings.ReplaceAll(col.Default, "'", "''")`, `table.go:83`).
**Requirement**: CENUM-01, CENUM-02, edge case (SQL-injection-safe escaping of a quote-containing value)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `pgType("enum")` returns `"TEXT"` explicitly (not relying on the `default:` fallthrough)
- [ ] `columnDDL` emits a `CHECK (...)` clause for an `enum` column, correctly ordered relative to `NOT NULL`/`DEFAULT`/`UNIQUE`/`REFERENCES`
- [ ] A value containing a single quote (`O'Brien`-style) is safely escaped in the generated clause — no broken SQL, no injection
- [ ] `createTable` (uses `columnDDL`) and `addMissingColumns` (also uses `columnDDL`) both pick up the new clause with zero additional changes
- [ ] Gate check passes: `go test ./internal/provisioner/...`
- [ ] Test count: at least 4 new tests in `column_ddl_test.go` (basic CHECK clause shape, quote-escaping, multi-value list, ordering relative to other clauses), no silent deletions

**Tests**: unit
**Gate**: quick

**Commit**: `feat(provisioner): emit CHECK constraint for enum columns`

---

### T4: `EnumValueInUseError`

**Status**: ✅ Complete

**What**: New typed error `EnumValueInUseError{Column string, Counts map[string]int, Cause error}` with a safe `Error()` (names every offending value + its row count) and `Unwrap()`.
**Where**: `internal/provisioner/errors.go`
**Depends on**: T3 (sequenced after it within Phase 2; no actual code dependency, but kept in-order per the phase's task sequence)
**Reuses**: `ForeignKeyViolationError`'s shape (`errors.go:44-58`).
**Requirement**: CENUM-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `EnumValueInUseError.Error()` names each offending value and its exact count (not just the first one found)
- [ ] `Unwrap()` returns `Cause` for server-side-only logging
- [ ] Gate check passes: `go test ./internal/provisioner/...`
- [ ] Test count: at least 2 new tests in `errors_test.go` (single offending value, multiple offending values), no silent deletions

**Tests**: unit
**Gate**: quick

**Commit**: `feat(provisioner): add EnumValueInUseError type`

---

### T5: Catalog lookup + `ReplaceColumnEnumValues`

**Status**: ✅ Complete (with one SPEC_DEVIATION, marked in `internal/provisioner/table.go`: the catalog lookup uses `pg_constraint` filtered to `contype='c'` + `conkey`, not `information_schema.table_constraints`/`key_column_usage` as design.md specified — `key_column_usage` holds only key columns, so that join returns zero rows for a CHECK constraint, verified against Postgres 16. Same intent as the design, correct catalog.)

**What**: New unexported helper to locate the current CHECK constraint on a column via `information_schema.table_constraints`/`key_column_usage` (mirrors `DropColumnForeignKey`'s query shape, filtered to `constraint_type = 'CHECK'`); new exported `ReplaceColumnEnumValues(ctx, schemaName, tableName, columnName string, oldValues, newValues []string) error` that: computes `removed := oldValues - newValues`; if non-empty, runs the scoped `COUNT(*) ... WHERE col = ANY($1) GROUP BY col` pre-check and returns `*EnumValueInUseError` on any non-zero count; otherwise looks up the current constraint name and runs a single atomic `ALTER TABLE ... DROP CONSTRAINT ..., ADD CONSTRAINT CHECK (...)` statement.
**Where**: `internal/provisioner/table.go`
**Depends on**: T3, T4
**Reuses**: `DropColumnForeignKey`'s catalog-lookup query shape (`table.go:402-428`); the escaping helper from T3.
**Requirement**: CENUM-07, CENUM-08, CENUM-09, CENUM-10, CENUM-11, CENUM-12, edge case (pre-check scoped with `WHERE col = ANY(removed_values)`, not a full comparison)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Pure widen (only additions, `removed` empty) skips the pre-check entirely and replaces the constraint
- [ ] Existing rows remain valid and unaffected after a widen (no data touched)
- [ ] Narrow with zero rows using any removed value replaces the constraint successfully
- [ ] Narrow with ≥1 row using a removed value is rejected before any `ALTER TABLE` runs, returning `*EnumValueInUseError` naming the value(s) and count(s)
- [ ] A rejected narrow leaves the table's existing CHECK constraint completely untouched (verified by re-querying the constraint definition after the rejected call)
- [ ] The pre-check query is scoped to `removed` values only (`WHERE col = ANY($1)`), not a full-table scan comparing old vs. new sets
- [ ] Gate check passes: `go test ./internal/provisioner/...`
- [ ] Test count: at least 6 new tests in `table_test.go` (widen success, narrow-zero-rows success, narrow-in-use rejection single value, narrow-in-use rejection multiple values, rejected-narrow leaves constraint intact, constraint correctly re-locatable via catalog lookup after a prior replace), no silent deletions

**Tests**: integration (real Postgres, existing pattern in this file)
**Gate**: quick

**Commit**: `feat(provisioner): add widen/narrow support for enum columns`

---

### T6: `allowedTypes["enum"]` + end-to-end enum table/column creation

**Status**: ✅ Complete

**What**: Add `"enum": true` to the Dashboard's `allowedTypes` map; verify (with new handler-level tests) that `CreateAppTable`/`CreateAppTableForUser` and the add-column path accept and provision an `enum` column end-to-end (request validation → `Apply` → real Postgres CHECK constraint).
**Where**: `internal/dashboard/handler.go` (the `allowedTypes` map, `~line 96-98`); tests in `internal/dashboard/apps_handler_test.go`
**Depends on**: T2, T3
**Reuses**: existing `CreateAppTable`/`CreateAppTableForUser` handler code — no logic change beyond the map entry, since `TableRequestBody` already carries `ColumnConfig` (including the new `AllowedValues` field from T1) straight through.
**Requirement**: CENUM-01, CENUM-02, CENUM-04, CENUM-05, CENUM-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `allowedTypes["enum"]` is `true`
- [ ] Creating a table with an `enum` column via `CreateAppTable` succeeds and the physical column has the expected `CHECK` constraint
- [ ] Creating a table with an invalid enum column (empty `AllowedValues`, `Default` not a member) is rejected with `400` before touching Postgres
- [ ] Adding a new `enum` column to an existing table succeeds identically
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: at least 4 new tests, no silent deletions

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): allow enum column type on table/column creation`

---

### T7: `UpdateAppTable` guard against `AllowedValues` change via `PUT`

**What**: Extend the existing `existingRefs`-style rejection block in `UpdateAppTable` with the same shape for `AllowedValues`: if an existing enum column's `AllowedValues` in the request body differs (set comparison) from what's currently stored, reject with `400` pointing at the dedicated endpoint (added in T8) — mirrors the `References`-change rejection immediately above it.
**Where**: `internal/dashboard/handler.go:1343-1356` (extend, same function)
**Depends on**: T6
**Reuses**: the exact `existingRefs` map/loop pattern in the same function.
**Requirement**: Design's "Error Handling Strategy" row for `PUT`-path `AllowedValues` changes (closes the same silent-no-op risk class AD-007 fixed for FKs)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] A `PUT /tables/{id}` request that changes an existing enum column's `AllowedValues` is rejected with `400` and an error naming the dedicated endpoint
- [ ] A `PUT /tables/{id}` request that leaves an existing enum column's `AllowedValues` unchanged succeeds normally (no false-positive rejection)
- [ ] A brand-new enum column in the same `PUT` request (no "before" state) is unaffected by this guard
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: at least 3 new tests (rejected change, unchanged-passes, new-column-in-same-request-passes), no silent deletions

**Tests**: integration
**Gate**: quick

**Commit**: `fix(dashboard): reject enum AllowedValues change via full-table PUT`

---

### T8: `UpdateColumnEnumValuesForUser` + `PATCH .../enum-values` route

**What**: New `Handler.UpdateColumnEnumValuesForUser(ctx, user, appID, tableName, columnName string, newValues []string, ip string) (*AppTableRow, error)`: auth/role check → find table/column, `404`/`400` on missing/non-enum → `config.ValidateEnumValues(newValues)` → if the column has a non-empty stored `Default`, re-check it's still in `newValues` → `p.prov.ReplaceColumnEnumValues(...)`, mapping `*provisioner.EnumValueInUseError` to `400` → persist `newValues` to the stored column config → audit `app.table.column.enum_values.update` → return refreshed `*AppTableRow`. Wire a new `PATCH /dashboard/api/apps/{id}/tables/{tableId}/columns/{columnName}/enum-values` route to it.
**Where**: `internal/dashboard/handler.go` (new method + route registration, same file/pattern as `AddColumnForeignKeyForUser`/`DropColumnForeignKeyForUser`)
**Depends on**: T5, T7
**Reuses**: `AddColumnForeignKeyForUser`/`DropColumnForeignKeyForUser` skeleton (`handler.go:1682`, `:1760`); `UpdateAppTable`'s typed-error-to-400 mapping idiom (`handler.go:1370-1375`).
**Requirement**: CENUM-07, CENUM-08, CENUM-09, CENUM-10, CENUM-11, CENUM-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Widening via the new route succeeds, existing rows unaffected
- [ ] Narrowing with no in-use values succeeds
- [ ] Narrowing with in-use values returns `400` naming the value(s)/count(s), not a raw Postgres error
- [ ] Calling the route on a non-enum column returns `400`
- [ ] Calling the route on a non-existent table/column returns `404`
- [ ] A non-writer role gets `403`
- [ ] Successful call persists the new `AllowedValues` to the stored config (verified by re-fetching the table) and writes an audit log entry
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: at least 7 new tests in `internal/dashboard/apps_column_enum_values_foruser_test.go` (mirrors `apps_column_foreign_key_foruser_test.go`'s structure), no silent deletions

**Tests**: integration
**Gate**: full (`go build ./... && go test ./... && go vet ./... && gofmt -l <changed files>` — closes Phase 1-3, first natural full-gate checkpoint)

**Commit**: `feat(dashboard): add dedicated endpoint to widen/narrow enum column values`

---

### T9: `HandleCreate`/`HandleUpdate` — map `23514` to a safe `400`

**What**: Add `errors.As(err, &pgErr) && pgErr.Code == "23514"` branching to both `HandleCreate` and `HandleUpdate`, returning `400` with a message built from `pgErr.ColumnName`/constraint name (never raw `pgErr.Message`/`pgErr.Detail`), instead of falling through to the current generic `500`.
**Where**: `internal/server/handler.go:145-198` (`HandleCreate`), `:250-303` (`HandleUpdate`)
**Depends on**: T6 (needs an enum column to exist to test against)
**Reuses**: `pgconn.PgError` type already imported/used elsewhere in the provisioner package (`table.go`) for the same `errors.As` pattern.
**Requirement**: CENUM-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `POST` (create row) with an out-of-set enum value returns `400` with a safe message (not `500`, not raw Postgres text)
- [ ] `PATCH`/`PUT` (update row) with an out-of-set enum value returns `400` with a safe message
- [ ] `POST`/`PATCH` with a valid enum value still succeeds unchanged (no regression on the happy path)
- [ ] Any other Postgres error on the same write path still falls through to the existing generic `500` (no over-broad catch)
- [ ] Gate check passes: `go test ./internal/server/...`
- [ ] Test count: at least 4 new tests in `handler_test.go` (create-reject, update-reject, create-happy-path-unaffected, other-error-still-500), no silent deletions

**Tests**: integration
**Gate**: quick

**Commit**: `fix(server): map enum CHECK violations to a safe 400 instead of a raw 500`

---

### T10: MCP enum coverage for `orbit_create_table`/`orbit_add_table_column`

**What**: No production code change (both tools already pass `config.ColumnConfig` through unchanged, per design — `AllowedValues` arrives "for free"). Add test coverage confirming an `enum` column with `AllowedValues` provisions correctly through both tools, and that an invalid enum definition is rejected the same way the Dashboard path rejects it.
**Where**: `internal/mcpserver/tools_test.go` (add cases; create if no single shared file exists — otherwise the file already covering `orbit_create_table`/`orbit_add_table_column`)
**Depends on**: T6
**Reuses**: existing `orbit_create_table`/`orbit_add_table_column` test setup.
**Requirement**: CENUM-14 (create-table/add-column portion)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `orbit_create_table` with an `enum` column + `allowed_values` provisions successfully
- [ ] `orbit_add_table_column` with an `enum` column + `allowed_values` provisions successfully
- [ ] An invalid enum definition (empty `allowed_values`) is rejected via both tools
- [ ] Gate check passes: `go test ./internal/mcpserver/...`
- [ ] Test count: at least 4 new tests, no silent deletions

**Tests**: integration
**Gate**: quick

**Commit**: `test(mcpserver): cover enum column type on create-table/add-column tools`

---

### T11: New MCP tool `orbit_update_column_enum_values`

**What**: New tool registration `{app_id, table_name, column_name, allowed_values []string}` calling `deps.DashH.UpdateColumnEnumValuesForUser`.
**Where**: `internal/mcpserver/tools.go` (new registration, near the FK tools)
**Depends on**: T8, T10
**Reuses**: `orbit_add_column_foreign_key`/`orbit_remove_column_foreign_key` registration shape and call-site pattern.
**Requirement**: CENUM-14 (widen/narrow portion)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Tool widens successfully
- [ ] Tool narrow-rejects with the safe error message when values are in use
- [ ] Tool call on a non-enum column is rejected
- [ ] Gate check passes: `go test ./internal/mcpserver/...`
- [ ] Test count: at least 3 new tests in `internal/mcpserver/tools_update_column_enum_values_test.go` (mirrors `tools_add_column_foreign_key_test.go`), no silent deletions

**Tests**: integration
**Gate**: full (closes Phase 4-5)

**Commit**: `feat(mcpserver): add orbit_update_column_enum_values tool`

---

### T12: `TableCard.tsx` — enum type option + new-column allowed-values input

**What**: Add `"enum"` to `COLUMN_TYPES`; when `type === "enum"` in the new-column form, render a values-list input (add/remove entries) bound to the column draft's `allowed_values`, with client-side caps mirroring T2 (1-50 values, 1-100 chars, no dup) for immediate feedback. Add the new i18n keys to both `en.json` and `pt-BR.json` in the same change.
**Where**: `internal/dashboard/ui/src/components/TableCard.tsx`, `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T6
**Reuses**: existing `Input`/form patterns already in this file for other column-type-specific inputs (e.g. the `DEFAULT_EXPRESSIONS` conditional rendering).
**Requirement**: CENUM-13, CENUM-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] "enum" appears in the column-type selector
- [ ] Selecting "enum" on a new column shows the allowed-values input
- [ ] New i18n keys present and identical in shape in both `en.json` and `pt-BR.json`
- [ ] `npx tsc -b` passes, `npm run build` passes
- [ ] Both locale JSON files parse as valid JSON

**Tests**: none (no frontend test runner configured in this repo)
**Gate**: build (`cd internal/dashboard/ui && npx tsc -b && npm run build`, plus the JSON-validity check)

**Commit**: `feat(dashboard-ui): add enum column type to table editor`

---

### T13: `TableCard.tsx` — dedicated "edit allowed values" action for an existing enum column

**What**: A small dedicated action (button/menu item on an existing enum column, not part of the generic save-all-columns form) that opens a focused editor and calls the new `PATCH .../enum-values` endpoint (T8) directly — mirrors how FK add/remove is already its own action in this file. New i18n keys added to both locales in the same change, including a string explaining why an existing column's values aren't inline-editable in the generic form.
**Where**: `internal/dashboard/ui/src/components/TableCard.tsx`, `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T12
**Reuses**: existing FK edit-action UI pattern (`FormDrawer`/`ConfirmDialog`) in this same file.
**Requirement**: CENUM-13, CENUM-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] An existing enum column shows an "edit allowed values" action, separate from the generic column-edit form
- [ ] The action calls the `PATCH .../enum-values` endpoint and surfaces a narrowing-rejection error (from T8) to the user
- [ ] The generic column-edit form no longer lets `AllowedValues` be edited inline for an existing enum column (consistent with T7's backend guard)
- [ ] New i18n keys present and identical in shape in both `en.json` and `pt-BR.json`
- [ ] `npx tsc -b` passes, `npm run build` passes
- [ ] Both locale JSON files parse as valid JSON

**Tests**: none (no frontend test runner configured in this repo)
**Gate**: build (closes Phase 6)

**Commit**: `feat(dashboard-ui): add dedicated action to widen/narrow enum column values`

---

### T14: `ai_build_chat_handlers.go` — allow proposing `enum`

**What**: Update the system prompt's "Column types — use ONLY these..." sentence to include `enum` (removing it from the "never propose" parenthetical) plus one sentence of guidance on when to use it; wire `allowed_values` through `propose_app_plan`'s column schema the same way other column fields already pass through.
**Where**: `internal/dashboard/ai_build_chat_handlers.go:48` (prompt text) + `propose_app_plan` tool schema in the same file
**Depends on**: T6
**Reuses**: existing `propose_app_plan` schema wiring for other `ColumnConfig` fields (e.g. how `References` already passes through for FK proposals).
**Requirement**: CENUM-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Prompt text lists `enum` as an allowed type with guidance on when to use it
- [ ] `propose_app_plan`'s column schema accepts `allowed_values`
- [ ] A confirmed plan proposing an `enum` column actually creates it (goes through the same `CreateAppTable`/validation path as T6, no separate code path)
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: at least 2 new tests in `ai_build_chat_handlers_test.go` (model proposes enum for a status-like ask, confirming the plan creates the table with the CHECK constraint), no silent deletions

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): allow AI build chat to propose enum columns`

---

### T15: `ai_edit_chat_handlers.go` — allow proposing `enum`

**What**: Same prompt change as T14, applied to the edit-chat system prompt (`ai_edit_chat_handlers.go:587`); wire `allowed_values` through `propose_add_column`'s schema.
**Where**: `internal/dashboard/ai_edit_chat_handlers.go:587` (prompt text) + `propose_add_column` tool schema in the same file
**Depends on**: T14
**Reuses**: same schema-wiring pattern as T14.
**Requirement**: CENUM-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Prompt text lists `enum` as an allowed type with the same guidance sentence
- [ ] `propose_add_column`'s schema accepts `allowed_values`
- [ ] A confirmed edit-chat proposal adding an `enum` column actually adds it (goes through T6's add-column path)
- [ ] Gate check passes: `go test ./internal/dashboard/...`
- [ ] Test count: at least 2 new tests in `ai_edit_chat_handlers_test.go`, no silent deletions

**Tests**: integration
**Gate**: full (closes Phase 7)

**Commit**: `feat(dashboard): allow AI edit chat to propose enum columns`

---

### T16: `CHANGELOG.md` entry

**What**: Add an entry under `## [Unreleased]` describing the new `enum` column type (CHECK-constraint-backed, creation-only, with widen/narrow via a dedicated endpoint).
**Where**: `CHANGELOG.md`
**Depends on**: T15
**Reuses**: existing `[Unreleased]` section format.
**Requirement**: AGENTS.md §6 (CHANGELOG must land in the same change as the feature)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Entry present under `[Unreleased]`, correctly categorized (Added)
- [ ] Full gate re-run clean as a final closing check: `go build ./... && go test ./... && go vet ./... && gofmt -l <all changed files>`, plus `npx tsc -b && npm run build`, plus both locale JSON files valid

**Tests**: none
**Gate**: build (final full-repo closing gate)

**Commit**: `docs: add column-enum-type CHANGELOG entry`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6 → Phase 7 → Phase 8

Phase 1:  T1 ------→ T2
Phase 2:  T3 ------→ T4 ------→ T5
Phase 3:  T6 ------→ T7 ------→ T8
Phase 4:  T9
Phase 5:  T10 -----→ T11
Phase 6:  T12 -----→ T13
Phase 7:  T14 -----→ T15
Phase 8:  T16
```

**Cross-phase dependencies** (backward arrows omitted from the per-phase blocks above for readability, listed here so every `Depends on` has a matching arrow):

```
T1 → T3
T3 → T5
T2 → T6
T3 → T6
T5 → T8
T6 → T9
T6 → T10
T6 → T12
T6 → T14
T8 → T11
T15 → T16
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

**Proposed batching** (~7-task budget, whole phases only, cut only at phase boundaries):

- **Batch 1** — Phases 1-3 (T1-T8, 8 tasks): config → provisioner → dashboard creation/guard/widen-narrow endpoint. This is the core, highest-ambiguity chain — everything downstream depends on it.
- **Batch 2** — Phases 4-6 (T9-T13, 5 tasks): write-path error mapping, MCP, Dashboard UI.
- **Batch 3** — Phases 7-8 (T14-T16, 3 tasks): AI prompts, CHANGELOG.

16 tasks total, 3 batches — within the session's medium workflow-size guideline (this is Execute via the skill's own sub-agent delegation, not the `Workflow` tool, so that guideline doesn't apply here; noted only for scale context).

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add `AllowedValues` field | 1 struct field | ✅ Granular |
| T2: `ValidateEnumValues` + wiring | 1 function + 2 call sites, 1 file | ✅ Granular |
| T3: `pgType` + `columnDDL` CHECK | 2 functions, 1 file | ✅ Granular |
| T4: `EnumValueInUseError` | 1 type, 1 file | ✅ Granular |
| T5: catalog lookup + `ReplaceColumnEnumValues` | 2 functions (one helper, one orchestrator), 1 file, tight dependency (orchestrator needs the helper) | ✅ Granular (cohesive pair) |
| T6: `allowedTypes` + creation E2E | 1 map entry + tests proving an existing path already works | ✅ Granular |
| T7: `UpdateAppTable` guard | 1 function extension, 1 file | ✅ Granular |
| T8: `UpdateColumnEnumValuesForUser` + route | 1 method + 1 route registration, cohesive (route has no meaning without the method) | ✅ Granular |
| T9: `HandleCreate`/`HandleUpdate` mapping | 2 functions, same file, same one-line fix pattern applied twice | ✅ Granular (cohesive pair) |
| T10: MCP enum coverage (no prod code) | tests only, 1 file | ✅ Granular |
| T11: new MCP tool | 1 tool registration, 1 file | ✅ Granular |
| T12: `TableCard.tsx` enum type + new-column input | 1 component, 1 concern (new-column path) | ✅ Granular |
| T13: `TableCard.tsx` edit-allowed-values action | 1 component, 1 concern (existing-column path) | ✅ Granular |
| T14: `ai_build_chat_handlers.go` prompt + schema | 1 file, 1 concern | ✅ Granular |
| T15: `ai_edit_chat_handlers.go` prompt + schema | 1 file, 1 concern | ✅ Granular |
| T16: CHANGELOG entry | 1 file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T1 | (start of Phase 2; cross-phase dep on T1 not drawn in intra-phase diagram, backward dep, allowed) | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T3, T4 | T3 → T4 → T5 | ✅ Match |
| T6 | T2, T3 | (start of Phase 3; backward deps on Phase 1/2 tasks) | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T5, T7 | T7 → T8 (T5 is a backward cross-phase dep) | ✅ Match |
| T9 | T6 | (start of Phase 4; backward dep) | ✅ Match |
| T10 | T6 | (start of Phase 5; backward dep) | ✅ Match |
| T11 | T8, T10 | T10 → T11 (T8 is a backward cross-phase dep) | ✅ Match |
| T12 | T6 | (start of Phase 6; backward dep) | ✅ Match |
| T13 | T12 | T12 → T13 | ✅ Match |
| T14 | T6 | (start of Phase 7; backward dep) | ✅ Match |
| T15 | T14 | T14 → T15 | ✅ Match |
| T16 | T15 | (start of Phase 8; backward dep) | ✅ Match |

No task depends on a task in a later phase. All arrows point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Add field | Entity/config (no logic) | none | none | ✅ OK |
| T2: `ValidateEnumValues` | config (domain logic) | unit | unit | ✅ OK |
| T3: `pgType`/`columnDDL` | provisioner (domain logic) | unit | unit | ✅ OK |
| T4: `EnumValueInUseError` | provisioner (domain logic) | unit | unit | ✅ OK |
| T5: `ReplaceColumnEnumValues` | provisioner (domain logic, DB-touching) | unit + integration | integration | ✅ OK |
| T6: `allowedTypes` + E2E | dashboard handler | integration | integration | ✅ OK |
| T7: `UpdateAppTable` guard | dashboard handler | integration | integration | ✅ OK |
| T8: new endpoint | dashboard handler | integration | integration | ✅ OK |
| T9: `HandleCreate`/`HandleUpdate` | server handler | integration | integration | ✅ OK |
| T10: MCP coverage | mcpserver | integration | integration | ✅ OK |
| T11: new MCP tool | mcpserver | integration | integration | ✅ OK |
| T12: `TableCard.tsx` (new-column) | Dashboard UI | none (no test runner) | none | ✅ OK |
| T13: `TableCard.tsx` (edit action) | Dashboard UI | none (no test runner) | none | ✅ OK |
| T14: AI build chat | dashboard handler (AI prompt/tool) | integration | integration | ✅ OK |
| T15: AI edit chat | dashboard handler (AI prompt/tool) | integration | integration | ✅ OK |
| T16: CHANGELOG | docs | none | none | ✅ OK |

No violations. Every task with a matrix-required test type includes those tests in the same task, not deferred.

---

## Tools for Execution

Before dispatching Batch 1, confirm with the user which MCPs/skills to use per task (no project MCP servers are obviously applicable here beyond the standard file/Bash tools already in use — flagging for explicit confirmation rather than assuming NONE).
