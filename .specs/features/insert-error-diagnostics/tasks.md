# Insert Error Diagnostics & Native Upsert Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Spec**: `.specs/features/insert-error-diagnostics/spec.md`
**Design**: none (Medium scope - no architectural decisions beyond extending the existing `checkViolationMessage`/pgErr-branch pattern already in `internal/server/handler.go`)
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/server/handler_test.go`, `internal/query/builder_test.go`) and `AGENTS.md` §3 - confirm before Execute. Guidelines found: `AGENTS.md` (build/test/vet/gofmt gate), existing test suites in both packages.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| HTTP handler (`internal/server`, `HandleCreate` and helpers) | integration (real Postgres via `TEST_DATABASE_URL`, existing `TestMain` pattern) | Every classified error code, every `on_conflict` value, every response-status branch (201/200/204/400/409/500) gets its own test - 1:1 to spec ACs | `internal/server/handler_test.go` (`TestHandlerCreate*` naming) | `TEST_DATABASE_URL=... go test -p 1 -parallel 4 ./internal/server/...` |
| Query builder (`internal/query`, `BuildInsert`) | unit (no DB) | All branches: normalization applied/not-applied, `ON CONFLICT` SQL shape per `on_conflict` value - 1:1 to spec ACs | `internal/query/builder_test.go` (`TestBuildInsert_*` naming) | `go test ./internal/query/...` |
| Test fixture (new `insert_diag` table + registry entry in `handler_test.go`) | none | Compiles and the schema/registry match; exercised indirectly by every handler test above | `internal/server/handler_test.go` (`TestMain`) | build gate only |

## Gate Check Commands

> Generated from `AGENTS.md` §3 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a query-builder-only task (no handler/DB change) | `go build ./... && go vet ./... && gofmt -l <changed files>` |
| Full | After any task touching `internal/server` or requiring DB-backed tests | `go build ./... && TEST_DATABASE_URL=<local test db> go test -p 1 -parallel 4 ./... && go vet ./... && gofmt -l <changed files>` |
| Build | Fixture-only task | `go build ./...` |

---

## Execution Plan

### Phase 1: Test fixture

```
T1
```

### Phase 2: Error classification (P1)

```
T1 → T2
```

### Phase 3: Empty-string normalization (P2)

```
T2 → T3
```

### Phase 4: Native upsert (P3)

```
T3 → T4 → T5 → T6
```

---

## Task Breakdown

### T1: Add dedicated `insert_diag` test fixture ✅ Complete

**What**: In `internal/server/handler_test.go`'s `TestMain`, add a new schema-local table `insert_diag` (own `CREATE TABLE`, not reusing `items`) with: `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`, `label TEXT NOT NULL`, `opened_at TIMESTAMPTZ NOT NULL` (no default - required, exercises the "required timestamptz still 400s" case), `closed_at TIMESTAMPTZ` (nullable - exercises normalization), `external_id TEXT UNIQUE` (exercises unique-violation/on_conflict). Register a matching `registry.Table` (`Columns`: `label` required text, `opened_at` required timestamptz, `closed_at` optional timestamptz, `external_id` optional text with `Unique: true`) under the existing `testhandler` app in `testReg`. Grant the same `zeep_app_enduser` privileges as the other fixture tables in the same setup block.
**Where**: `internal/server/handler_test.go` (modify `TestMain` only)
**Depends on**: None
**Reuses**: The existing `testSchema`/`testTable` fixture block immediately above as the pattern (same GRANT statements, same registration style)
**Requirement**: none (infrastructure for INSERTERR-01..17)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `insert_diag` table and registry entry exist with exactly the columns above
- [ ] Existing fixture (`items`, `notes`) and their tests are untouched
- [ ] `go build ./...` succeeds; `go vet ./...` clean

**Tests**: none
**Gate**: build

**Commit**: `test(server): add insert_diag fixture for insert-error-diagnostics tests`

---

### T2: Classify insert errors - 400 for invalid input, 409 for unique violation

**What**: In `HandleCreate`'s error branch (`internal/server/handler.go`, currently around the `pgErr.Code == "23514"` check), add two more classified branches before the generic 500 fallback: (a) `pgErr.Code` in `{"22007","22008","22P02"}` → `http.StatusBadRequest` with a message built by a new `invalidInputMessage(pgErr *pgconn.PgError) string` helper (mirrors `checkViolationMessage`: names `pgErr.ColumnName` when present, else falls back to a generic "invalid value" message; never includes `pgErr.Message`/`pgErr.Detail`); (b) `pgErr.Code == "23505"` (only when the request's `on_conflict` is absent or `"error"` - `on_conflict` parsing itself is added in T4, so for this task treat every `23505` as the `"error"` path) → `http.StatusConflict` with a message from a new `uniqueViolationMessage(pgErr *pgconn.PgError) string` helper (names `pgErr.ConstraintName` when present, else a generic "row already exists"). Leave the existing `23514` and statement-timeout branches untouched.
**Where**: `internal/server/handler.go` (modify `HandleCreate` and add the two helper functions near `checkViolationMessage`)
**Depends on**: T1
**Reuses**: `checkViolationMessage` as the structural pattern for both new helpers
**Requirement**: INSERTERR-01, INSERTERR-02, INSERTERR-03, INSERTERR-04, INSERTERR-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `""` posted for `insert_diag.opened_at` (required timestamptz) → 400, message names `opened_at`, does not contain raw driver text
- [ ] A duplicate `external_id` posted twice → second insert 409, message names the unique constraint
- [ ] `TestHandlerCreateOtherErrorStillGeneric500` (existing, unrelated SQLSTATE 22021) still passes unmodified - proves the new branches are narrowly scoped
- [ ] No response body for these two new paths contains `pgErr.Message` or `pgErr.Detail`
- [ ] Full gate passes

**Tests**: integration
**Gate**: full

**Commit**: `fix(server): classify insert errors as 400/409 instead of generic 500`

---

### T3: Normalize empty string to NULL for nullable timestamptz columns

**What**: In `query.BuildInsert` (`internal/query/builder.go`), before appending a column's value to `args`, check: if `types[col.Name] == "timestamptz"` and the incoming value is the empty string `""` and the column is not in `table`'s required set (`!col.Required`), substitute `nil` instead of `""`. Leave every other type and every required timestamptz column untouched - a `""` sent for `opened_at` (required) still reaches Postgres as-is and is now classified 400 by T2.
**Where**: `internal/query/builder.go` (modify `BuildInsert`)
**Depends on**: T2
**Reuses**: The existing `types := columnTypes(table)` lookup already present in `BuildInsert`
**Requirement**: INSERTERR-06, INSERTERR-07, INSERTERR-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Unit test: `BuildInsert` with `{"closed_at": ""}` on a nullable timestamptz column produces `nil` in `Args`, not `""`
- [ ] Unit test: `BuildInsert` with `{"closed_at": ""}` on a `text` column leaves `""` unchanged (scope check - normalization never leaks to other types)
- [ ] Integration test: POST `{"label":"x","opened_at":"","closed_at":""}` to `insert_diag` → 201, response has `closed_at: null`
- [ ] Integration test: POST `{"label":"x","opened_at":""}` (required column empty) → 400 naming `opened_at` (proves T2's classification still fires - normalization must not silently "fix" a required field)
- [ ] Full gate passes

**Tests**: unit, integration
**Gate**: full

**Commit**: `fix(query): normalize empty string to NULL for nullable timestamptz columns`

---

### T4: Parse and validate `on_conflict` / `conflict_columns`

**What**: In `HandleCreate`, after decoding the request body and before calling `query.BuildInsert`, read optional `on_conflict` (string) and `conflict_columns` ([]string) fields out of `body` (then delete them from `body` so they aren't treated as table columns by `BuildInsert`'s unknown-field check). Validate: (a) `on_conflict` not in `{"", "error", "ignore", "update"}` → 400; (b) `on_conflict == "update"` and `len(conflict_columns) == 0` → 400; (c) any name in `conflict_columns` not present in `table`'s known columns (reuse the column-set lookup `BuildInsert` already has, or export it) → 400 naming the invalid column. On success, pass the validated `on_conflict`/`conflict_columns` through to `query.BuildInsert` (extend its signature or add a small options struct - implementer's choice, keep `BuildInsert`'s existing call sites working for the `"error"`/no-op default).
**Where**: `internal/server/handler.go` (modify `HandleCreate`; export a column-name-validation helper from `internal/query/builder.go` only if reuse turns out cleaner than a local copy - `handler.go` is the primary and only required file)
**Depends on**: T3
**Reuses**: `known := columnSet(table)` pattern already in `BuildInsert`
**Requirement**: INSERTERR-13, INSERTERR-15, INSERTERR-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] POST with `on_conflict: "bogus"` → 400
- [ ] POST with `on_conflict: "update"` and no `conflict_columns` → 400
- [ ] POST with `on_conflict: "update"`, `conflict_columns: ["does_not_exist"]` → 400 naming `does_not_exist`
- [ ] POST with no `on_conflict` field behaves exactly as before (existing `TestHandlerCRUD` create case still passes unmodified)
- [ ] Full gate passes

**Tests**: integration
**Gate**: full

**Commit**: `feat(server): validate on_conflict and conflict_columns request fields`

---

### T5: Build `ON CONFLICT` SQL in the insert query

**What**: Extend `query.BuildInsert` (or the options it now accepts from T4) to emit the conflict clause: `on_conflict == "ignore"` with `conflict_columns` present → `ON CONFLICT (<conflict_columns>) DO NOTHING`; `"ignore"` with `conflict_columns` absent → bare `ON CONFLICT DO NOTHING`; `"update"` (always has `conflict_columns` per T4's validation) → `ON CONFLICT (<conflict_columns>) DO UPDATE SET <every column present in the request body except conflict_columns and system fields>, updated_at = now()`; `"error"`/absent → no conflict clause (today's SQL, unchanged). Column names interpolated into the conflict target and `SET` clause come only from `table`'s known column set (already validated in T4) or from `conflict_columns` (already validated) - never raw user strings beyond that whitelist, since this is string-built SQL.
**Where**: `internal/query/builder.go` (modify `BuildInsert`)
**Depends on**: T4
**Reuses**: The existing `cols`/`placeholders`/`args` construction loop in `BuildInsert`
**Requirement**: INSERTERR-09, INSERTERR-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Unit test: `on_conflict:"ignore"`, no `conflict_columns` → SQL contains `ON CONFLICT DO NOTHING` (no target list)
- [ ] Unit test: `on_conflict:"ignore"`, `conflict_columns:["external_id"]` → SQL contains `ON CONFLICT (external_id) DO NOTHING`
- [ ] Unit test: `on_conflict:"update"`, `conflict_columns:["external_id"]`, body has `label` → SQL contains `ON CONFLICT (external_id) DO UPDATE SET label = excluded.label, updated_at = now()` (or equivalent parameterized form) and does NOT include `external_id` itself in the `SET` list
- [ ] Unit test: `on_conflict` absent → SQL identical to current `BuildInsert` output (no regression)
- [ ] Quick gate passes

**Tests**: unit
**Gate**: quick

**Commit**: `feat(query): build ON CONFLICT DO NOTHING / DO UPDATE clauses`

---

### T6: Wire response status branching for ignore / update outcomes

**What**: In `HandleCreate`, after executing the (possibly upsert) insert query: if `on_conflict != "update"` and a row came back from `RETURNING *`, respond 201 (today's behavior, unchanged - covers both plain insert and `"ignore"` when no conflict occurred). If `on_conflict == "update"`, respond 200 with the returned row (conflict path always returns a row via `DO UPDATE ... RETURNING *`). If `on_conflict == "ignore"` and the query returns zero rows (conflict occurred, `DO NOTHING` short-circuited `RETURNING`): when `conflict_columns` was supplied, run a follow-up `SELECT * FROM <schema>.<table> WHERE <conflict_columns match the submitted values>` and respond 200 with that row; when `conflict_columns` was absent, respond 204 with no body.
**Where**: `internal/server/handler.go` (modify `HandleCreate`)
**Depends on**: T5
**Reuses**: `pgx.CollectOneRow`/`pgx.CollectRows` already imported and used elsewhere in the file
**Requirement**: INSERTERR-10, INSERTERR-11, INSERTERR-12, INSERTERR-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] POST twice with `on_conflict:"ignore"`, `conflict_columns:["external_id"]`, same `external_id` → first 201, second 200 with the existing row's data
- [ ] POST twice with `on_conflict:"ignore"`, no `conflict_columns`, same `external_id` → first 201, second 204 with empty body
- [ ] POST twice with `on_conflict:"update"`, `conflict_columns:["external_id"]`, second POST changes `label` → second response 200, `label` updated, `updated_at` newer than the first response's
- [ ] POST with no `on_conflict` and a real conflict → 409 (T2's path), unchanged
- [ ] Full gate passes

**Tests**: integration
**Gate**: full

**Commit**: `feat(server): return 200/204 for on_conflict ignore/update outcomes`

---

## Phase Execution Map

```
Phase 1: T1
Phase 2: T2
Phase 3: T3
Phase 4: T4 → T5 → T6
```

Execution is strictly sequential - one task at a time, in order. 6 tasks total fits a single batch (≤ ~7) - no sub-agent dispatch needed; runs inline.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add insert_diag fixture | 1 file (test setup only) | ✅ Granular |
| T2: Classify insert errors | 1 function + 2 helpers, 1 file | ✅ Granular (cohesive - same error-handling block) |
| T3: Normalize "" to NULL | 1 function, 1 file | ✅ Granular |
| T4: Parse/validate on_conflict | 1 function, 1 file (+ maybe 1 exported helper) | ✅ Granular |
| T5: Build ON CONFLICT SQL | 1 function, 1 file | ✅ Granular |
| T6: Wire response branching | 1 function, 1 file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start of Phase 1, no incoming arrow) | ✅ Match |
| T2 | T1 | Phase 1 → Phase 2 | ✅ Match |
| T3 | T2 | Phase 2 → Phase 3 | ✅ Match |
| T4 | T3 | Phase 3 → Phase 4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: insert_diag fixture | Test fixture | none | none | ✅ OK |
| T2: Classify insert errors | HTTP handler | integration | integration | ✅ OK |
| T3: Normalize "" to NULL | Query builder | unit | unit, integration | ✅ OK (exceeds matrix minimum - cross-layer AC) |
| T4: Parse/validate on_conflict | HTTP handler | integration | integration | ✅ OK |
| T5: Build ON CONFLICT SQL | Query builder | unit | unit | ✅ OK |
| T6: Wire response branching | HTTP handler | integration | integration | ✅ OK |
