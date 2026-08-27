# column-enum-type Validation

**Date**: 2026-08-26
**Spec**: `.specs/features/column-enum-type/spec.md`
**Diff range**: `93ddd1e^..d4e3321` (16 implementation commits, 3 batch workers)
**Verifier**: independent sub-agent (author ≠ verifier) — no shared context with any implementer

**Verdict**: **PASS ✅**

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 `AllowedValues` on `ColumnConfig` | ✅ Done | `internal/config/types.go:63-66` |
| T2 `ValidateEnumValues` + wiring | ✅ Done | `internal/config/validate.go:154-176`, wired at `validate.go:56-60` + `validate.go:132-138` |
| T3 `pgType` enum + `columnDDL` CHECK | ✅ Done | `internal/provisioner/table.go:44-48`, `table.go:55-63`, `table.go:108-110` |
| T4 `EnumValueInUseError` | ✅ Done | `internal/provisioner/errors.go:64-91` |
| T5 catalog lookup + `ReplaceColumnEnumValues` | ✅ Done | `internal/provisioner/table.go:451-587`. Carries a documented `SPEC_DEVIATION` (`table.go:459-468`) — re-verified below, deviation is correct. |
| T6 `allowedTypes["enum"]` + e2e creation | ✅ Done | `internal/dashboard/handler.go:100` |
| T7 `UpdateAppTable` PUT guard | ✅ Done | `internal/dashboard/handler.go:1381-1389`; correctly scoped to pre-existing columns only (sits after the `if !existed { continue }` at `handler.go:1374`) |
| T8 `UpdateColumnEnumValuesForUser` + PATCH route | ✅ Done | `internal/dashboard/handler.go:1861-1930` (op) / `1932-2007` (route); route registered `internal/server/server.go:237` |
| T9 `23514` → safe 400 | ✅ Done | `internal/server/handler.go:73-84`, `handler.go:204-208` (create), `handler.go:319-323` (update) |
| T10 MCP enum on create-table/add-column | ✅ Done | covered by `tools_write_test.go` / `tools_add_table_column_test.go` |
| T11 `orbit_update_column_enum_values` | ✅ Done | `internal/mcpserver/tools.go:441-455`; error mapping `tools.go:191-192`, `tools.go:225-226` |
| T12 `TableCard.tsx` enum type + values input | ✅ Done | `TableCard.tsx:30` (`COLUMN_TYPES`), `TableCard.tsx:990-1088` (`EnumAllowedValuesEditor`) |
| T13 dedicated edit-allowed-values action | ✅ Done | `TableCard.tsx:1120-1212` (`EditEnumValuesAction`); PATCH helper `TableCard.tsx:1089-1118` |
| T14 build chat may propose `enum` | ✅ Done | `ai_build_chat_handlers.go:48` (prompt), `ai/client.go:55-66` + `client.go:548-552` (schema). Documented `SPEC_DEVIATION` at `ai/client.go:55`. |
| T15 edit chat may propose `enum` | ✅ Done | `ai_edit_chat_handlers.go:592` (prompt), `ai_edit_chat_handlers.go:349-354` (passthrough), `ai/client.go:653-657` (schema) |
| T16 CHANGELOG entry | ✅ Done | `CHANGELOG.md:14` under `## [Unreleased] → ### Added` |

All 16 tasks' `✅ Complete` claim independently confirmed against real code and tests. No partials, no blocked tasks.

---

## Spec-Anchored Acceptance Criteria

### P1: Define an enum column with enforced allowed values

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| **CENUM-01** POST tables with `type:"enum"` + non-empty `AllowedValues` → physical column `text` + `CHECK (col IN (...))` | column type exactly `text`; CHECK restricting to exactly those values; DB rejects out-of-set | `internal/provisioner/column_ddl_test.go:41-48` — `strings.HasPrefix(ddl, "\"status\" TEXT")` and `strings.Contains(ddl, "CHECK (\"status\" IN ('pending'))")`; `internal/dashboard/apps_handler_test.go:637` — `physicalColumnType(...) != "text"`; `apps_handler_test.go:640-644` — CHECK def contains `pending`/`Em andamento`; `apps_handler_test.go:647-651` — in-set INSERT succeeds, `'qualquer coisa'` INSERT returns non-nil error | ✅ PASS |
| **CENUM-02** add new `enum` column to existing table (MCP / Dashboard REST) → same CHECK | same constraint as creation path | `internal/dashboard/apps_handler_test.go:749-751` — `checkConstraintDefForColumn(...) == ""` → fatal; `apps_handler_test.go:752-754` — in-set INSERT ok; `apps_handler_test.go:755-757` — `'qualquer coisa'` INSERT must error; `internal/mcpserver/tools_add_table_column_test.go:255-258` — `reflect.DeepEqual(*got, []string{"pending","active","closed"})`; `internal/dashboard/apps_handler_test.go` `TestUpdateAppTable_AllowsAllowedValuesOnBrandNewColumn` (PUT path, new column) | ✅ PASS |
| **CENUM-03** INSERT/UPDATE with out-of-set value → DB rejects; system maps to typed, non-leaking error (AGENTS.md §4) | HTTP 400, non-empty message, no raw Postgres text / attempted value echoed | **POST:** `internal/server/handler_test.go:746` — `rec.Code != http.StatusBadRequest` → fatal; `handler_test.go:757-759` — `strings.Contains(msg, "qualquer coisa")` → fatal (no value leak). **PATCH:** `internal/server/handler_test.go:818` — `updateRec.Code != http.StatusBadRequest` → fatal; `handler_test.go:829-831` — no value leak. **Happy path:** `handler_test.go:777` — `201`, `handler_test.go:784` — `row["status"] == "pending"`. **Scope guard:** `handler_test.go:834+` `TestHandlerCreateOtherErrorStillGeneric500` proves a non-23514 pg error (22021) still falls through to 500. | ✅ PASS |
| **CENUM-04** empty / missing / duplicate `AllowedValues` → rejected at request-validation time with a specific error | rejected before DB, error identifies empty list / duplicate / too long / too many | `internal/config/validate_test.go:201-209` empty list → error contains `"at least one value"`; `validate_test.go:213-227` 51 values → error contains `"51"` and `"50"`, and exactly 50 is accepted; `validate_test.go:231-238` empty entry → error contains `"allowed_values[1]"` + `"empty"`; `validate_test.go:242-255` 101-char entry → error contains `"allowed_values[1]"` + `"101"`, 100 accented runes accepted; `validate_test.go:259-267` duplicate → error contains `"pending"` + `"duplicate"`; wiring: `validate_test.go:281-289` (`ValidateTables` names `column[0] (col)`); handler path: `internal/dashboard/apps_handler_test.go:657+`, `:758+`; widen/narrow path: `apps_column_enum_values_foruser_test.go:216-237` (`*ValidationError` for empty/dup/empty-entry); MCP: `tools_write_test.go:284+`, `tools_add_table_column_test.go:267+` (`res.IsError` true, no column added) | ✅ PASS |
| **CENUM-05** `Default` not in `AllowedValues` → rejected at request-validation time | rejected before DB, error names the offending default | `internal/config/validate_test.go:299-318` — member default accepted; non-member `"closed"` → error contains `"closed"`; `internal/dashboard/apps_handler_test.go:691+` `TestCreateAppTableForUser_EnumDefaultOutsideAllowedValuesRejected`; narrowing that drops the default: `apps_column_enum_values_foruser_test.go:264-270` — `errors.As(err, &valErr)` and message contains `"pending"` | ✅ PASS |
| **CENUM-06** free-text values (spaces, accents, quotes) accepted and safely escaped — no SQL injection | value accepted at validation; single quote doubled in the CHECK clause | `internal/config/validate_test.go:274-279` — `["Em andamento","Não iniciado","O'Brien","active","Active"]` accepted (also proves case-sensitive exact matching); `internal/provisioner/column_ddl_test.go:72-76` — exact string equality on `CHECK ("status" IN ('O''Brien', 'Em andamento', '''); DROP TABLE t; --'))`; live DB round-trip: `internal/provisioner/table_test.go:964-971` — `O'Brien`/`Em andamento` accepted after two constraint replaces, `qualquer coisa` still rejected | ✅ PASS |

### P1: Widen an enum column's allowed values

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| **CENUM-07** update to a superset → CHECK replaced with the larger set, no data migration | new value writable, out-of-set still rejected, constraint replaced not stacked | `internal/provisioner/table_test.go:781-783` — `'closed'` accepted after widening; `table_test.go:785-787` — `'qualquer coisa'` still rejected (replaced, not dropped); `table_test.go:949-962` — exactly **1** CHECK constraint on the column after two replaces (`n != 1` → error), proving the catalog lookup re-locates the constraint it created; handler path: `apps_column_enum_values_foruser_test.go:44+`, route: `apps_column_enum_values_foruser_test.go:336-340` (`200`); MCP: `tools_update_column_enum_values_test.go:48+` | ✅ PASS |
| **CENUM-08** existing rows unaffected and immediately valid after widening | pre-existing row count unchanged | `internal/provisioner/table_test.go:789-795` — `SELECT COUNT(*) ... WHERE status='active'` must equal `1`; `apps_column_enum_values_foruser_test.go:44+` (handler-level widen leaves rows intact) | ✅ PASS |

**SPEC_DEVIATION re-verified (CENUM-07/08):** `internal/provisioner/table.go:459-468` documents that design.md's `information_schema.table_constraints` ⋈ `key_column_usage` join cannot work (`key_column_usage` holds only key columns; a CHECK constraint yields zero rows). I independently confirmed the implementation at `table.go:469-489` uses `pg_constraint` filtered on `contype = 'c'` **and** `conkey = ARRAY[a.attnum]` joined through `pg_class`/`pg_namespace`/`pg_attribute` — exactly what the comment claims, and the same catalog shape the test's own oracle uses (`table_test.go:734-740`). Deviation is correct and better-grounded than the design. Not a gap.

### P1: Narrow an enum column's allowed values safely

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| **CENUM-09** narrowing runs a `COUNT(*)` pre-check for rows holding removed values | pre-check happens before DDL, scoped to removed values | Implementation: `internal/provisioner/table.go:510-516` — `SELECT %q, COUNT(*) FROM %q.%q WHERE %q = ANY($1) GROUP BY %q` (genuinely scoped, `$1` = `removedValues(old,new)` from `table.go:566-572`; **not** a full-set comparison in Go). Assertions: `table_test.go:851-856` — `Counts["closed"] == 1` **and** `Counts["active"]` must be absent even though 2 rows hold `active` (proves the `= ANY(removed)` scoping); `table_test.go:809-811` — clean narrow succeeds | ✅ PASS |
| **CENUM-10** count > 0 → reject whole update before altering the constraint, naming each offending value + exact row count | typed error naming every value and its count | `internal/provisioner/table_test.go:844-847` — `errors.As(err, &inUse)` on `*EnumValueInUseError`; `table_test.go:851-853` — `Counts["closed"] == 1`; `table_test.go:857-859` — message contains `` `"closed" is used by 1 row(s)` ``; multi-value: `table_test.go:884-892` — `Counts["closed"]==1`, `Counts["archived"]==3`, `len(Counts)==2`; message shape: `internal/provisioner/errors_test.go:64+`, `:75+`; no-leak: `errors_test.go:89+` (`DoesNotLeakCause`) and `apps_column_enum_values_foruser_test.go:146-148` — message must not contain `"SQLSTATE"` or `"check constraint"`; HTTP: `apps_column_enum_values_foruser_test.go:348-356` — `400` + `used by 1 row(s)` + no `SQLSTATE`; MCP: `tools_update_column_enum_values_test.go:148-153` | ✅ PASS |
| **CENUM-11** count zero for every removed value → CHECK replaced with the narrowed set | removed value now rejected, remaining value still accepted | `internal/provisioner/table_test.go:813-815` — `'closed'` must now be rejected; `table_test.go:816-818` — `'pending'` still accepted; handler: `apps_column_enum_values_foruser_test.go:106-115` — persisted `AllowedValues` is the 2-value set **and** INSERT of the removed value errors | ✅ PASS |
| **CENUM-12** a rejected narrow never leaves a partially-migrated state (no constraint dropped without a valid replacement) | existing CHECK definition byte-for-byte unchanged after rejection | `internal/provisioner/table_test.go:906-919` — `checkConstraintDef` captured **before**, re-queried **after** via `pg_get_constraintdef`, asserted `after != before` → error (an actual constraint re-query, not merely "no error returned"); `table_test.go:922-927` — the value we tried to remove is still writable and an out-of-set value still rejected; handler level: `apps_column_enum_values_foruser_test.go:131` + `:162-164` — same before/after constraint-def comparison, plus `:151-160` stored config still has 3 values. Implementation backing: the swap is a single `ALTER TABLE ... DROP CONSTRAINT x, ADD CHECK (...)` statement (`table.go:581`) so Postgres applies both or neither. | ✅ PASS |

### P2: Enum type available in Dashboard UI, MCP, and AI chat

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| **CENUM-13** Dashboard column-type selector lists `enum`; while `enum` selected, shows an allowed-values input | `enum` present in the type list; values input rendered | Verified by direct source reading (repo has **no** frontend test runner — `package.json` has no `test` script; tasks.md Test Coverage Matrix declares "build gate only" for this layer). `internal/dashboard/ui/src/components/TableCard.tsx:30` — `"enum"` appended to `COLUMN_TYPES`; `TableCard.tsx:832-838` — `col.type === "enum" && !existingEnumColumn` renders `<EnumAllowedValuesEditor onChange={...}>`; `TableCard.tsx:820-831` — existing enum column renders a read-only chip list + `EditEnumValuesAction` (T13's dedicated action); `TableCard.tsx:1120-1212` — `EditEnumValuesAction` (FormDrawer, PATCH, error surfaced); submit-time guard `TableCard.tsx:245-248` blocks an enum column with zero values. Gate: `npx tsc -b` exit 0, `npm run build` exit 0. | ✅ PASS (verified by inspection — layer has no test runner by design) |
| **CENUM-14** `orbit_create_table` / `orbit_add_table_column` with `type:"enum"` + `AllowedValues` → provisioned identically to Dashboard (same validation, same constraint) | success + same CHECK; invalid enum → structured tool error, nothing provisioned | `internal/mcpserver/tools_write_test.go:233+` `TestOrbitCreateTable_EnumColumnProvisions`; `tools_add_table_column_test.go:245-258` — `res.IsError` false, `c.Type == "enum"`, `reflect.DeepEqual(*got, []string{"pending","active","closed"})`; invalid: `tools_add_table_column_test.go:290-296` — `res.IsError` true, non-empty message, and schema re-fetched to confirm no column added; `tools_write_test.go:284+` same for create-table. New tool: `tools_update_column_enum_values_test.go:48+` / `:106+` / `:159+` (widen 200 / in-use narrow rejected / non-enum → `dashboard.ErrColumnIsNotEnum.Error()` exact-string match at `:200`) | ✅ PASS |
| **CENUM-15** AI build/edit chat may propose `type:"enum"` with `AllowedValues` (prohibition removed from both handler prompts) | prohibition removed; `enum` + `allowed_values` survive end-to-end to a real CHECK constraint | Prompt prohibition removal verified by source reading (a system-prompt string is not behavior-testable): `internal/dashboard/ai_build_chat_handlers.go:48` and `internal/dashboard/ai_edit_chat_handlers.go:592` — both now list `jsonb, enum` and drop `"enum"` from the forbidden-type examples. Behavior: `ai_build_chat_handlers_test.go:1003-1008` — `col.Type == "enum"` and `AllowedValues == [pending shipped delivered]`; `ai_build_chat_handlers_test.go:1052-1054` + `:1058-1065` — confirm creates the real CHECK constraint mentioning every value; `ai_edit_chat_handlers_test.go:1427-1432` — edit-chat op preserves `enum` + `[open closed]`; `ai_edit_chat_handlers_test.go:1440+` — confirm attaches the CHECK constraint | ✅ PASS |
| **CENUM-16** every new user-facing string added to **both** `en.json` and `pt-BR.json` in the same change (AGENTS.md §5) | 1:1 key parity, both files valid JSON | Diff-verified: the same **12** keys (`tableCard.allowedValuesLabel` … `tableCard.editAllowedValuesExplainer`) added to `internal/dashboard/ui/src/locales/en.json` and `internal/dashboard/ui/src/locales/pt-BR.json` in commit `e525cc2`. Every `t("tableCard.…")` call introduced in `TableCard.tsx` resolves to one of those 12 keys or a pre-existing key (`tableCard.cancel`, `tableCard.saveTable`, `tableCard.savingTable`, `tableCard.saveError`). No hardcoded user-facing literal in the new components. `python3 -c "import json; json.load(...)"` → both parse (`JSON_OK`). | ✅ PASS |

**Status**: ✅ **16/16 ACs covered and matched to the spec-defined outcome. 0 gaps, 0 spec-precision gaps.**

Two criteria (CENUM-13, and CENUM-15's prompt-text clause) are verified by direct source inspection rather than an automated assertion. This is not an evidence gap: the repo has no frontend test runner (declared in tasks.md's Test Coverage Matrix), and an LLM system-prompt string has no deterministic assertion surface. Both are cited to exact `file:line`, and CENUM-15's *behavioral* half is fully assertion-backed.

---

## Discrimination Sensor

**Isolation**: temporary git worktree at `…/scratchpad/sensor` created from `HEAD` (`d4e3321`). Real-tree `git status --porcelain` baseline captured before (3 untracked `.specs/features/column-enum-type/*.md`) and re-confirmed identical after `git worktree remove --force` + `git worktree prune`. No `git stash` used. Real tree never mutated.

| # | File:line | Mutation | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/provisioner/table.go:512` | Narrowing pre-check scoping inverted: `WHERE col = ANY($1)` → `WHERE col != ALL($1)` (counts *kept* values instead of removed ones) | ✅ Killed — `table_test.go:852` `Counts["closed"] = 0, want 1`; `table_test.go:855` `Counts must only cover removed values, got map[active:2]`; 4 tests failed |
| 2 | `internal/provisioner/table.go:576` | Rejection condition flipped: `if len(counts) > 0` → `if len(counts) < 0` (never rejects a narrowing; falls through to raw Postgres 23514) | ✅ Killed — `table_test.go:846`/`:882`/`:914` all `expected *EnumValueInUseError, got *fmt.wrapError (… SQLSTATE 23514)`; 3 tests failed |
| 3 | `internal/server/handler.go:205` + `:320` | Postgres error code checked changed `23514` → `23503` in both `HandleCreate` and `HandleUpdate` | ✅ Killed — `handler_test.go:747` `esperado 400 …, obtido 500: {"error":"failed to insert row"}`; `handler_test.go:819` same for the PATCH path |
| 4 | `internal/provisioner/table.go:60` | CHECK-clause escaping removed: `strings.ReplaceAll(v, "'", "''")` → `v` (single quotes no longer doubled) | ✅ Killed — `column_ddl_test.go:74` exact-string mismatch on the escaped clause; `table_test.go:944` live DB `syntax error at or near "Brien" (SQLSTATE 42601)` |

**Sensor depth**: lightweight (4 mutations, standard-feature tier — one above the 1-3 floor because the risk surface spans two packages)
**Result**: **4/4 killed — PASS ✅**. No surviving mutants; no fix tasks generated by the sensor.

---

## Code Quality

| Principle | Status | Note |
| --- | --- | --- |
| Minimum code | ✅ | No speculative surface. `ValidateEnumValues` exported only because two real call sites (creation + widen/narrow) must not drift — justified inline at `validate.go:148-152`. |
| Surgical changes | ✅ | Every touched file maps to a task. The two out-of-listed-scope touches (`ai/client.go`, `ui/src/lib/api.ts`) are both mechanically forced (the tool schema and the TS interface literally have nowhere else to live) and both documented. |
| No scope creep | ✅ | Spec's Out of Scope table respected: no non-enum→enum conversion path, no native PG `ENUM`, no FSM/ordering, no shared enum registry. `UpdateColumnEnumValuesForUser` returns `ErrColumnIsNotEnum` rather than converting. |
| Matches patterns | ✅ | `ReplaceColumnEnumValues`/`findColumnCheckConstraint` mirror the existing `AddColumnForeignKey`/`DropColumnForeignKey` catalog-lookup shape; the PUT guard mirrors the `References` guard directly above it; MCP tool + `mapWriteError` branch follow `orbit_remove_column_foreign_key`. |
| Would a senior engineer approve? | ✅ | Yes. The single-statement `DROP CONSTRAINT …, ADD CHECK …` atomic swap and the deliberately unnamed replacement constraint (so origin is indistinguishable by name) are both explicitly reasoned at `table.go:539-552`. |
| Tests map to ACs, non-shallow (spot-check) | ✅ | Spot-checked the narrowing story: `TestReplaceColumnEnumValues_RejectedNarrowLeavesConstraintIntact` re-queries `pg_get_constraintdef` before/after and additionally re-probes write behavior — the opposite of a shallow "no error returned" assertion. |
| Spec-anchored outcome check | ✅ | All asserted values match spec-defined outcomes (see AC table; status codes 400/404/403/200, exact row counts, exact escaped DDL strings). |
| Per-layer Coverage Expectation met | ✅ | config: 1:1 to CENUM-04/05/06 + every listed edge case. provisioner: 1:1 to CENUM-01/02/07-12. dashboard routes: happy + 400 (invalid values, in-use narrow, non-enum) + 404 (unknown table, unknown column, non-member) + 403 (viewer). server: happy + both error paths + a negative-control test proving the 23514 branch is narrowly scoped. mcpserver: happy + validation-error per new/touched tool. |
| Every test maps to a spec requirement | ✅ | 55 new `Test*` functions; each carries a `CENUM-nn` or task-id comment. No unclaimed tests. |
| Documented guidelines followed | ✅ | `AGENTS.md` §3 (gates), §4 (English-only API errors; no raw `err.Error()` in 500s — `checkViolationMessage` at `handler.go:76-84` deliberately omits `pgErr.Message`/`Detail`; typed-error carve-out used for `EnumValueInUseError`), §5 (both locales in the same change), §6 (CHANGELOG in the same change). |

### Minor observations (non-blocking, no fix task warranted)

1. `tasks.md:443` (T13) states its two SPEC_DEVIATIONs are "noted inline as code comments in `TableCard.tsx`" — the explanatory comments are indeed present (`TableCard.tsx:1120-1125`, `:1089-1093`), but they are **not** tagged with the literal `SPEC_DEVIATION` marker the other three use. A future `grep -rn SPEC_DEVIATION` sweep will miss them. Traceability nit only; the reasoning itself is documented and correct.
2. `ErrColumnIsNotEnum`'s text carries the `"dashboard: "` package prefix and is surfaced verbatim to MCP clients (`tools.go:191-192`). The REST route maps it to a clean `"column is not an enum column"` (`handler.go:1985`). Cosmetic inconsistency between surfaces.
3. `TableCard.tsx:820` gates the existing-enum-column UI on `table.id` being truthy; if it were ever falsy, a saved enum column would render neither its read-only chips nor the edit action. Defensive-only — a persisted table always has an id.

---

## Edge Cases

- [x] `AllowedValues` > 50 entries → rejected, error states the limit — `validate_test.go:213-227` (asserts both `"51"` and `"50"` appear; exactly 50 accepted)
- [x] Entry empty / > 100 chars / exact-match duplicate → rejected, offending entry identified — `validate_test.go:231-238`, `:242-255`, `:259-267`
- [x] Entry containing a single quote → accepted at validation, correctly escaped in the CHECK clause, no injection — `validate_test.go:274-279` + `column_ddl_test.go:72-76` (including `'); DROP TABLE t; --`) + live round-trip `table_test.go:964-971`
- [x] `enum` without explicit `Nullable` → nullability semantics unchanged — `column_ddl_test.go:88-95` asserts full clause ordering `TEXT NOT NULL DEFAULT 'pending' UNIQUE CHECK (...) REFERENCES ...` by exact string equality; the CHECK is additive and never touches the null clause
- [x] Narrowing pre-check scoped with `WHERE col = ANY(removed)` rather than an in-application full-set comparison — `table.go:512`; empirically enforced by sensor mutation #1

---

## Gate Check

- **Gate command** (tasks.md "Build" level): `go build ./... && go test -p 1 -parallel 4 ./... && go vet ./... && gofmt -l <changed .go files>`, plus `cd internal/dashboard/ui && npx tsc -b && npm run build`, plus JSON validation of both locale files.

| Step | Result |
| --- | --- |
| `go build ./...` | ✅ exit 0 |
| `go vet ./...` | ✅ exit 0 |
| `gofmt -l` on all 20 changed `.go` files | ✅ empty output |
| `go test -p 1 -parallel 4 ./...` | ✅ all packages `ok` |
| `npx tsc -b` (`internal/dashboard/ui`) | ✅ exit 0 |
| `npm run build` | ✅ exit 0, `✓ built in 1.81s` |
| `json.load(en.json)` / `json.load(pt-BR.json)` | ✅ `JSON_OK` |

**Environmental note (not a feature defect):** the first `go test ./...` run reported 36 failures across `internal/mcpserver` and `internal/server`, **all** webhook tests, **all** with the same root cause: `crypto: neither WEBHOOK_TOKEN_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set` — a missing local env var that `.github/workflows/reusable-ci.yml:26` supplies in CI. Re-running the affected packages with `DASHBOARD_BOOTSTRAP_SECRET=ci-test-bootstrap-secret-not-for-prod` gives exit 0 across `internal/server`, `internal/mcpserver`, `internal/provisioner`, `internal/config`. Zero column-enum-type tests were among the failures, and zero column-enum-type tests skipped (`go test -v -run Enum` → 25 PASS, 0 SKIP, 0 FAIL).

- **Test count before feature**: N (baseline)
- **Test count after feature**: N + 55
- **Delta**: **+55 new `Test*` functions** — no test deleted, no assertion weakened anywhere in the diff (`git diff --stat` shows +3565/−16, the 16 deletions all being replaced comment/signature lines, not assertions).
- **Skipped tests**: none among the feature's tests. The pre-existing repo-wide `t.Skip("TEST_DATABASE_URL not set")` guards did not fire (`TEST_DATABASE_URL` is set; local Postgres reachable on :5434).
- **Failures**: none attributable to this feature.

---

## Fix Plans

None. No blocking gaps found. The three "Minor observations" above are recorded for awareness and do not warrant a fix task.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| CENUM-01 | Design / Pending | ✅ Verified |
| CENUM-02 | Design / Pending | ✅ Verified |
| CENUM-03 | Design / Pending | ✅ Verified |
| CENUM-04 | Design / Pending | ✅ Verified |
| CENUM-05 | Design / Pending | ✅ Verified |
| CENUM-06 | Design / Pending | ✅ Verified |
| CENUM-07 | Design / Pending | ✅ Verified |
| CENUM-08 | Design / Pending | ✅ Verified |
| CENUM-09 | Design / Pending | ✅ Verified |
| CENUM-10 | Design / Pending | ✅ Verified |
| CENUM-11 | Design / Pending | ✅ Verified |
| CENUM-12 | Design / Pending | ✅ Verified |
| CENUM-13 | Design / Pending | ✅ Verified |
| CENUM-14 | Design / Pending | ✅ Verified |
| CENUM-15 | Design / Pending | ✅ Verified |
| CENUM-16 | Design / Pending | ✅ Verified |

`spec.md`'s Requirement Traceability table updated accordingly (Phase → `Verify`, Status → `✅ Verified`, coverage line → 16/16).

---

## Summary

**Overall**: ✅ **Ready**

**Spec-anchored check**: 16/16 ACs matched the spec-defined outcome — 0 gaps, 0 spec-precision gaps
**Sensor**: 4/4 mutations killed (lightweight tier)
**Gate**: 7/7 steps passed, 0 feature-attributable failures

**What works**

- `type: "enum"` provisions as `TEXT` + `CHECK (col IN (...))` on both the create-table and add-column paths, across Dashboard REST, MCP, and AI chat — enforcement is genuinely at the database layer, proven by live out-of-set `INSERT` assertions rather than by inspecting generated SQL alone.
- Out-of-set writes on the app-table API return **400** with a message that never echoes the attempted value or any Postgres text; a negative-control test proves the `23514` branch does not swallow other write failures.
- Widening replaces the constraint atomically and re-locates it by catalog lookup, so a second widen/narrow operates on the constraint the first one created (asserted: exactly 1 CHECK constraint after two replaces).
- Narrowing pre-checks with a genuinely scoped `WHERE col = ANY(removed)` query, rejects with a typed `*EnumValueInUseError` naming every offending value and its exact count, and a rejected narrow leaves the physical constraint byte-for-byte unchanged (verified by re-querying `pg_get_constraintdef`, not by absence of an error).
- Free-text values (accents, spaces, single quotes) round-trip safely; `'); DROP TABLE t; --` is escaped, not executed.
- The full-table `PUT` path is guarded against the silent-no-op class where an `AllowedValues` change would be persisted without the DDL ever running — while still permitting a brand-new enum column in the same request.

**Issues found**: none blocking. Three minor observations recorded above (untagged SPEC_DEVIATION comments in `TableCard.tsx`; `"dashboard: "` prefix leaking into the MCP-surfaced `ErrColumnIsNotEnum` message; a defensive `table.id` gate in the UI).

**Next steps**: feature is done. Orchestrator may mark `column-enum-type` complete. The three SPEC_DEVIATION markers from the three batches have been distilled into lessons (see `.specs/LESSONS.md`).
