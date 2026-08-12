# Modo RLS "policy" (rls-policy-mode) Validation

**Date**: 2026-08-12
**Spec**: `.specs/features/rls-policy-mode/spec.md`
**Diff range**: `a999e9b..HEAD` (16 commits: `7bd9571`..`11605cb`)
**Verifier**: independent sub-agent (author ≠ verifier)
**Round**: 2 of max 3 (re-verification after the round-1 FAIL)

**Verdict: ✅ PASS.** The round-1 blocker is dead: reverting `filterOwner(ownerID, table)` to plain `ownerID` at the `query.BuildUpdate` and `query.BuildDelete` call sites (`internal/server/handler.go:278`, `:332`) — the mutation that survived the whole suite in round 1 — now fails `TestPolicyMode_ListAndGetSeeOtherUsersRow/Update` and `/Delete`. All 5 mutations injected this round were killed, the full suite is green under `-p 1`, and the frontend gate is clean. What remains are 3 spec-precision gaps and a set of non-blocking observations, all flagged below rather than silently passed.

## What changed since round 1

| Round-1 gap | Severity | Fix commit | Status now |
| ----------- | -------- | ---------- | ---------- |
| Surviving mutant: UPDATE/DELETE call sites of `filterOwner` undiscriminated | Blocker | `6f96d2a` | ✅ Closed — re-injected the exact mutation, it dies (Sensor M1) |
| Fail-closed untested for get-by-id, UPDATE, DELETE | Major | `69e2a6e` | ✅ Closed — new REST get-by-id subtest + `UPDATE`/`DELETE` 0-rows-affected assertions under `SET LOCAL ROLE zeep_app_enduser` |
| CHANGELOG + 4 READMEs not updated; `design.md` untracked | Major | `11605cb` | ✅ Closed — `[Unreleased] → Added` entry, Row-Level Security row extended in `README.md` + all 3 translations, `design.md` and `validation.md` now tracked |
| 4 rejection tests asserted only `err != nil` | Minor | `293edcc` | ✅ Closed — exact/contains message assertions (Sensor M3, M4 confirm they discriminate) |
| `resolveTableRLS("policy")` pass-through untested | Minor | `b4421d5` | ✅ Closed — new table case; Sensor M2 confirms it discriminates |

No production code changed between rounds. All 5 fix commits are test-and-docs only, plus `spec.md` status flips. Verified: `git diff 6d4d481..HEAD -- '*.go'` touches only `_test.go` files.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 Predicados centrais (`internal/config/rls.go`) | ✅ Done | 3 funcs, 18 asserts across 3 table tests |
| T2 `resolveOwner`/`filterOwner` decoupling | ✅ Done | Round-1 partial resolved: all four operations (list/get/update/delete) now asserted through the HTTP entry point, not only through the helper's unit table |
| T3 `EnsureRowLevelSecurity` extracted | ✅ Done | Real-DB test + idempotency test; `table_policies_store.go` switched to the helper |
| T4 `createTable`/`addMissingColumns` recognize `"policy"` | ✅ Done | Fail-closed now proven for SELECT **and** UPDATE/DELETE against real Postgres |
| T5 `owner_id` in policy clauses | ✅ Done | Exact SQL string asserted |
| T6 enum validation + auth-email gate | ✅ Done | Both rejection messages now asserted verbatim |
| T7 `UpdateAppTable` enables RLS on switch | ✅ Done | 3 tests (enable, data preserved, one-way ratchet) |
| T8 `docs/generator.go` | ✅ Done | `generator_test.go`, 4 cases incl. negative |
| T9 Data Browser | ✅ Done | Exercises the real `ListDataBrowserApps` handler, 3 cases incl. negative |
| T10 Frontend option + warning | ⚠️ Accepted without tests | Matrix says `Required Test Type: none` for this layer; build gate green |
| T11 End-to-end integration | ✅ Done | 6 subtests green (was 5); fixture still reproduces `createTable`'s DDL by hand instead of calling the provisioner — see Observations |

`design.md` is now tracked (`11605cb`).

---

## Spec-Anchored Acceptance Criteria

### P1: Modo `rls: "policy"` sem auto-scope de dono

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| P1-1 / RLSP-02: WHEN table created with `rls:"policy"` THEN provisioner enables RLS at creation, before any policy | `relrowsecurity = true` right after `createTable`, zero policies | `internal/provisioner/table_test.go:152` — `t.Fatal("expected RLS enabled on a rls:policy table right after createTable, before any policy exists")` guarded by `!rowSecurityEnabled(t, pool, schema, "posts")` | ✅ PASS |
| P1-2 / RLSP-02: WHILE no `select` policy for the user's role, `GET /{app}/{table}` and `GET /{app}/{table}/{id}` return zero rows via native enforcement | list: HTTP 200 + `data` length 0; get-by-id: no row | list `internal/server/rls_policy_mode_test.go:185` — `if len(data) != 0`; **get-by-id `internal/server/rls_policy_mode_test.go:200`** — `t.Fatalf("esperado 404 (nenhuma select policy para no_policy_role), obtido %d…")`; DB-level proof `:228` — `count != 0` → `"RLS deve negar tudo sem policy"`; provisioner-level `internal/provisioner/table_test.go:185` — `count != 0` | ✅ PASS (was ⚠️ Partial in round 1) |
| P1-3 / RLSP-01: WHEN a `select` policy without a row-restricting clause exists for a role THEN that user sees all rows, including other users' | 2 rows returned, both seeded IDs present | `internal/server/rls_policy_mode_test.go:270` — `if len(data) != 2`; `:279` — `if !seen[postAID] \|\| !seen[postBID]` | ✅ PASS |
| P1-4 / RLSP-01: The system SHALL NOT apply `WHERE owner_id = $sub` (or the UPDATE/DELETE equivalent) to any operation on a `rls:"policy"` table | No `owner_id` predicate on **list, get, update, delete** | list `internal/server/handler_test.go:616` — `len(data) != 1` → `"nenhum filtro owner_id em rls:policy"`; get `:627` — `rec.Code != http.StatusOK`; **update `:645`** — `rec.Code != http.StatusOK` on `PATCH` of another user's row, plus `:651` asserting `row["title"]` actually changed; **delete `:663`** — `rec.Code != http.StatusNoContent` on `DELETE` of another user's row; predicate table `:472`/`:500-502` — `filterOwner("user-123", rls:"policy") == ""` | ✅ PASS (was ❌ GAP in round 1; Sensor M1 confirms it now discriminates) |
| P1-5 / RLSP-03: IF a `rls:"policy"` table receives a valid INSERT THEN `owner_id` is filled with the authenticated `sub` | Response `owner_id` == the JWT `sub` | `internal/server/handler_test.go:706` — `if ownerID != creatingUserID`; `internal/server/rls_policy_mode_test.go:341` — `if ownerID != userAID`; `internal/server/handler_test.go:472` — `{"policy + user → real owner_id (still populated for INSERT)", "policy", userCtx, "user-123", true}` | ✅ PASS |
| P1-6 / RLSP-04: The system SHALL keep `""`/`"owner"`/`"enabled"` behavior byte-identical | No generated-SQL change for those modes | `internal/provisioner/table_test.go:305` — `t.Fatalf("rls=%q: expected RLS NOT enabled at creation (still lazy, on first policy)", rls)`; `internal/config/rls_test.go:39`/`:61` — `HasOwnerColumn("policy")=true` vs `AutoScopesByOwner("policy")=false`, with `owner`/`enabled` unchanged; `internal/server/handler_test.go:500` — `filterOwner` returns `"user-123"` for owner/enabled; full pre-existing suite green with zero assertion edits | ⚠️ Spec-precision gap (unchanged from round 1) — spec says "byte a byte" and T2's Done-when says "teste de regressão comparando query antes/depois"; no test compares the generated SQL string before/after. Covered behaviorally, not literally |
| P1-7 / RLSP-09: IF `rls` is not one of `""`/`"owner"`/`"enabled"`/`"policy"` THEN validation rejects with a clear error | Rejection with an error naming the accepted values | `internal/dashboard/handler_test.go:116-118` — `wantMsg := \`table clientes has an invalid rls value: disabled (must be one of "", "owner", "enabled", "policy")\`; if err.Error() != wantMsg { … }`; enum table `internal/config/rls_test.go:15` and negative cases (`"disabled"`, `"polcy"`, `"enable"`, `"OWNER"`) | ✅ PASS (was ⚠️ in round 1; Sensor M3 confirms) |

### P2: `owner_id` referenciável em cláusula de policy

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| P2-1 / RLSP-05: WHEN a policy references `owner_id` THEN `translateClause` accepts it as a valid `uuid` column | Clause translates to `"owner_id" = current_setting('app.jwt_sub', true)::UUID` | `internal/provisioner/policy_test.go:630` — `want := "\"owner_id\" = current_setting('app.jwt_sub', true)::UUID"` asserted with `strings.Contains(sql, want)` | ✅ PASS |
| P2-2 / RLSP-06: IF a clause uses an operator incompatible with `uuid` (e.g. `LIKE`) THEN reject with a clear error, "mesma validação de tipo já aplicada às demais colunas `uuid`" | Rejection with a clear error | `internal/provisioner/policy_test.go:652-653` — `if !strings.Contains(err.Error(), \`invalid operator "LIKE"\`)` | ⚠️ Spec-precision gap (**new finding, round 2**) — the message assertion is now correct and discriminating (Sensor M4), but the rejection comes from `policyOperators`, a **global** operator allowlist (`internal/provisioner/policy.go:21-32`, `:231-233`) that has never contained `LIKE` for any column type. There is no per-type operator compatibility check anywhere in `translateClause`, so the AC's premise ("same type validation already applied to other uuid columns") describes a mechanism that does not exist. Behavior is right; the AC's wording is not |
| P2-3 / RLSP-06: The system SHALL keep rejecting any column not in `table.Columns` and not `owner_id` | `id`/`updated_at`/`deleted_at` still rejected as unknown column | `internal/provisioner/policy_test.go:678-681` — loop over `{"id","updated_at","deleted_at"}` with `wantMsg := fmt.Sprintf("unknown column %q", col)` and `strings.Contains(err.Error(), wantMsg)`; pre-existing `:169` unchanged | ✅ PASS (was ⚠️ in round 1) |

**Scope note (not an AC violation, unchanged):** `internal/provisioner/policy.go:162` injects `owner_id` into `colByName` **unconditionally**, so a clause on an `rls: ""` table (which has no `owner_id` column) passes the builder's validation and would only fail later in Postgres. P2-1 scopes the requirement to `"policy"`/`"owner"`/`"enabled"`. Low practical impact; no test covers the `""` case either way.

### P3: Troca de modo em tabela existente via Dashboard

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| P3-1 / RLSP-07: WHEN the admin switches RLS between `"enabled"` and `"policy"` THEN an explicit warning appears before confirming | Warning shown before the change is applied | `internal/dashboard/ui/src/components/TableCard.tsx:182-184` — `if (!isDraft && isPolicyRLS(val) !== isPolicyRLS(rls)) { if (!confirm(t("tableCard.rlsModeSwitchConfirm"))) return; }`; strings at `src/locales/en.json:737` and `pt-BR.json:737` | ⚠️ No test evidence — accepted per tasks.md Test Coverage Matrix (`Frontend … Required Test Type: none`). `confirm()` is the established dashboard pattern (`Webhooks.tsx:130,140`, `TablePolicies.tsx:86`, `TableCard.tsx:248`), so T10's "reuse the existing confirmation pattern" is satisfied |
| P3-2 / RLSP-07: WHEN confirmed THEN the change applies without recreating the table or losing data | Pre-existing row survives the switch | `internal/dashboard/apps_store_test.go:442` — `t.Fatalf("expected the pre-existing row to survive the mode switch, got count=%d", count)` guarded by `count != 1` | ✅ PASS |
| P3-3 / RLSP-08: IF the target table has no RLS enabled THEN switching to `"policy"` enables RLS then, preserving P1-2's fail-closed | `relrowsecurity` false before, true after | `internal/dashboard/apps_store_test.go:394` — `t.Fatal("expected RLS disabled before switching to policy mode")`; `:405` — `t.Fatal("expected RLS enabled after switching to policy mode")`; one-way ratchet at `:477-479` | ✅ PASS |

### RLSP-10: superfícies que só reconheciam `"owner"`/`"enabled"`

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| Auth-email gate treats `"policy"` like `"owner"`/`"enabled"` | Rejected without email auth, accepted with | `internal/dashboard/handler_test.go:140-142` — `wantMsg := \`table posts uses restricted access (RLS), which requires 'Autenticação por e-mail' to be enabled for this app\`; if err.Error() != wantMsg`; accepted-with-auth case at `:121-131` | ✅ PASS |
| OpenAPI schema exposes `owner_id` for `"policy"` (uuid, readOnly, required) | `{type: string, format: uuid, readOnly: true}` + in `required` | `internal/docs/generator_test.go:21-24` — `prop.Type != "string" \|\| prop.Format != "uuid" \|\| !prop.ReadOnly`; `:34` — present in `schema.Required`; regressions at `:47`, `:60`, negative at `:73` | ✅ PASS |
| Data Browser lists `owner_id` for `"owner"`/`"enabled"`/`"policy"` | `owner_id` in the table's column list | `internal/dashboard/handler_test.go:239` (`"policy"`), `:230` (`"enabled"`), negative (`""`) — via the real `ListDataBrowserApps` handler | ✅ PASS |
| `resolveTableRLS` never picks `"policy"` implicitly; explicit `"policy"` passes through; empty `rls` still defaults to `"enabled"` | Default stays `"enabled"`; `"policy"` → `"policy"` | `internal/dashboard/table_rls_test.go:13` — `{"omitted + require + auth → enabled", "", true, true, "enabled"}`; `:19` — `{"explicit policy respected", "policy", true, true, "policy"}` | ✅ PASS (was ⚠️ Partial in round 1; Sensor M2 confirms) |

**Status**: ✅ All ACs covered — 0 hard gaps, 0 partials, 2 spec-precision gaps (P1-6, P2-2) plus 1 accepted no-test layer (P3-1).

---

## Discrimination Sensor

Scratch: `git worktree add --detach <scratch> HEAD`, mutated there, `git worktree remove --force` after the run. `internal/dashboard/static/` was copied in (generated, not tracked) so the `//go:embed` in `internal/dashboard/embed.go` could resolve.

| # | File:line | Description | Killed? |
| - | --------- | ----------- | ------- |
| M1 | `internal/server/handler.go:278` and `:332` | **Round-1 survivor, re-injected verbatim:** `filterOwner(ownerID, table)` → `ownerID` at the `query.BuildUpdate` and `query.BuildDelete` call sites (re-applies `WHERE owner_id = $sub` to UPDATE/DELETE on `rls:"policy"` tables) | ✅ **Killed** — `TestPolicyMode_ListAndGetSeeOtherUsersRow/Update` (`handler_test.go:645`) and `/Delete` (`:663`) both FAIL; `/List` and `/GetByID` stay green, so the new subtests are what catches it |
| M2 | `internal/dashboard/handler.go:109` | `if requested == "" && requireRLSDefault && authEmailEnabled` → dropped the `requested == ""` guard (explicit `"policy"` gets coerced to `"enabled"`) | ✅ Killed — `TestResolveTableRLS/explicit_policy_respected` (the case added in `b4421d5`) plus `/explicit_public_always_respected` |
| M3 | `internal/dashboard/handler.go:130` | Stripped the accepted-values list from the enum rejection message (`" has an invalid rls value: " + t.RLS + " (must be one of …)"` → `" has an invalid rls value"`) | ✅ Killed — `TestValidateTableInputRejectsUnknownRLS`; would have survived round 1's `err != nil` assertion |
| M4 | `internal/provisioner/policy.go:21-32` | Added `"LIKE": "LIKE"` to the `policyOperators` allowlist | ✅ Killed — `TestBuildPolicySQL_OwnerIDRejectsIncompatibleOperator` + pre-existing `TestBuildPolicySQL_RejectsOperatorOutsideAllowlist` |
| M5 | `internal/config/rls.go:19-26` | `HasOwnerColumn`: removed `"policy"` from the true-case | ✅ Killed — `TestHasOwnerColumn` (`rls_test.go:44`), `TestValidateTableInputRejectsPolicyWithoutEmailAuth`, `TestListDataBrowserApps_PolicyRLSShowsOwnerIDColumn`, `TestBuildResponseSchema_PolicyRLSExposesOwnerID`, `TestCreateTable_PolicyModeEnablesRLSAtCreation`, `TestCreateTable_PolicyModeCreatesOwnerColumn`, `TestAddMissingColumns_PolicyModeAddsOwnerColumn` |

**Sensor depth**: P0-full (data-integrity/authorization path). Cumulative across both rounds: 11 behavior-level mutations, 10 killed on first injection, 1 (M1) killed after the round-1 fix.
**Result**: 5/5 killed — ✅ PASS
**Isolation**: `git status --porcelain` before and after is identical (` M .specs/LESSONS.md`, ` M .specs/lessons.json`, `?? internal/dashboard/ui/package-lock.json`). `git worktree list` shows only the real tree. `git stash` was never used.

**M1 kill-mechanism note (not a gap):** the mutant dies with HTTP 500, not 404. `setupPolicyModeFixture` issues a JWT whose `sub` is the literal string `"calling-user-id"`, so the re-introduced `owner_id = $N` predicate fails as an invalid UUID cast in Postgres before it can filter anything. The assertion (`rec.Code != http.StatusOK` / `!= http.StatusNoContent`) is satisfied by any non-success, so it discriminates the mutant reliably, but via an error class the spec does not describe. A UUID `sub` in the fixture would make the kill a clean 404/0-rows and pin the behavior more precisely.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ Two named predicates replacing six duplicated conditions; no enum type ceremony (design.md option B) |
| Surgical changes | ✅ Every touched line traces to a task; the 5 fix commits touch only `_test.go`, docs, and `spec.md` statuses |
| No scope creep | ✅ The two pre-existing gaps fixed for free (`enabled` missing from the Data Browser and the OpenAPI schema) are declared in design.md and pinned by their own regression tests |
| Matches patterns | ✅ `Test<Verb><Entity>_<Scenario>` naming, `TEST_DATABASE_URL` skip guard, pool-RLS/pool-owner fixture shape follow `rls_policy_test.go`; server errors stay in English (AGENTS.md §4); frontend uses the dashboard's existing `confirm()` pattern (AGENTS.md §5 i18n satisfied in both locales) |
| Spec-anchored outcome check | ⚠️ 2 spec-precision gaps remain (P1-6 no SQL-string comparison; P2-2's AC premise). All 4 round-1 `err != nil` assertions now assert message content |
| Per-layer Coverage Expectation met | ✅ `internal/server` now covers list/get/update/delete for `rls:"policy"` (5 of 5 routes in scope, happy + fail-closed); `internal/config`, `internal/provisioner`, `internal/dashboard`, `internal/docs` all meet theirs |
| Every test maps to a spec requirement | ✅ All new test functions and subtests carry a comment naming the AC/task they cover; no unclaimed tests |
| Documented guidelines followed | ✅ AGENTS.md §6 satisfied: `CHANGELOG.md` `[Unreleased] → Added` entry present; `README.md` Row-Level Security row extended and mirrored into `i18n/README.pt-BR.md`, `.pt-PT.md`, `.es.md`; `design.md` tracked. AGENTS.md §3 gate commands run and green |

Observations (non-blocking, carried from round 1 unless noted):

- `internal/server/handler_test.go`'s `setupPolicyModeFixture` and `internal/server/rls_policy_mode_test.go`'s `setupRLSPolicyModeFixture` both use the schema/app name `rls_policy_mode_test_app` in the same package. Safe only because neither calls `t.Parallel()`.
- `TestRLSPolicyMode_EndToEnd`'s subtests are order-dependent: `DataBrowserOwnerPoolSeesEveryRowRegardlessOfPolicy` hardcodes `count != 3`, assuming `REST_InsertStillPopulatesOwnerID` ran first.
- T11's fixture builds the `posts` DDL and the `ALTER TABLE … ENABLE ROW LEVEL SECURITY` by hand rather than calling `provisioner.createTable`, so the end-to-end test does not exercise the provisioner path it documents (covered separately in `internal/provisioner/table_test.go`).
- `spec.md`'s traceability table is now internally inconsistent: the fix commits flipped RLSP-01/02/06/09/10 to `Verified` but left RLSP-03/04/05/07/08 at `Implementing`, even though those were already fully covered in round 1. See the Requirement Traceability Update below for the correct end state.
- `appForm.tablePolicy` is the string `"Policy"` in both `en.json` and `pt-BR.json`. Both files carry the key (AGENTS.md §5 satisfied); leaving the technical term untranslated is a judgment call, not a defect.
- Round 1's Fix 5 (record `DASHBOARD_BOOTSTRAP_SECRET` and `-p 1` in tasks.md's Gate Check Commands) was not in the routed gap set and is still open. It costs the next verifier a false-regression investigation.

---

## Edge Cases

- [x] **IF a `rls:"policy"` table receives DELETE or UPDATE with no matching policy THEN the operation is denied (0 rows affected)** — now covered. `internal/provisioner/table_test.go:158` grants `SELECT, UPDATE, DELETE` to `zeep_app_enduser`; `:196` asserts `updateTag.RowsAffected() != 0` fails and `:203` the same for `deleteTag`, both under `SET LOCAL ROLE zeep_app_enduser` against a zero-policy table.
- [x] IF the admin tries `rls:"policy"` on an app without email auth THEN reject — `internal/dashboard/handler_test.go:140-142` (message asserted verbatim).
- [x] WHEN the Data Browser lists a `rls:"policy"` table THEN all rows are shown, no policy required — `internal/server/rls_policy_mode_test.go:355` (`count != 3` via the owner pool) and `internal/dashboard/handler_test.go:239` (column list).
- [x] WHEN the OpenAPI generator processes a `rls:"policy"` table THEN the schema includes `owner_id` (uuid, readOnly) — `internal/docs/generator_test.go:21-24`, `:34`.
- [x] WHEN `resolveTableRLS` and the auth-email gate evaluate `rls:"policy"` THEN both treat it like `"owner"`/`"enabled"`, and the empty-`rls` default stays `"enabled"` — `internal/dashboard/table_rls_test.go:13` (default) and `:19` (explicit `"policy"` pass-through); gate at `handler_test.go:140`, `:121`.

---

## Gate Check

- **Gate command** (tasks.md, Build + Full levels):
  - `go build ./... && go test ./... && go vet ./... && gofmt -l $(git diff --name-only a999e9b..HEAD -- '*.go')`
  - `cd internal/dashboard/ui && npx tsc -b && npm run build`
- **Result**: 738 passed, 0 failed, 1 skipped (full suite exit 0; round 1 was 734 passed — +4 from the new subtests). `go build` exit 0, `go vet` exit 0, `gofmt -l` empty. `npx tsc -b` (TypeScript 5.6.3, the pinned local toolchain) exit 0, `npm run build` exit 0. Both locale JSONs parse.
- **Environment required to reach green** (still undocumented in tasks.md — see Observations):
  - `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable` (the repo's own `docker-compose.yml` `db` service, container `zeep-orbit-db-1`)
  - `DASHBOARD_BOOTSTRAP_SECRET` set — without it 42 pre-existing webhook tests fail with `crypto: neither WEBHOOK_TOKEN_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set`
  - `-p 1` — with Go's default parallel package execution, 13 pre-existing `internal/dashboard` webhook tests fail from cross-package contention on the shared database. Reproduced this round without `-p 1`, and verified pre-existing at baseline `a999e9b` in round 1
  - Toolchain note: invoking `tsc` through a globally-resolved newer TypeScript (7.x) reports `TS5102: Option 'baseUrl' has been removed` from `tsconfig.json`. Not a feature defect — the repo pins `typescript: ~5.6.2` and the local `node_modules/.bin/tsc` (5.6.3) exits 0
- **Test count before feature**: 429 top-level `func Test`
- **Test count after feature**: 459 top-level `func Test`
- **Delta**: +30 test functions, +6 subtests/cases added in round 2 (`/Update`, `/Delete`, `REST_NoSelectPolicyReturnsNotFoundForGetByID`, the UPDATE and DELETE deny assertions, the `explicit policy respected` case) and 4 assertions strengthened from `err != nil` to message-exact. No test deleted, no assertion weakened.
- **Skipped tests**: `TestInstallationAutoAccess` (pre-existing, needs GitHub App credentials). Justified.
- **Failures**: none under the documented gate conditions.

---

## Fix Plans

No blocking fixes. Remaining items, in priority order:

### Fix A: tasks.md's gate command is not runnable as written (carried over)

- **Root cause**: reaching a green `go test ./...` requires `DASHBOARD_BOOTSTRAP_SECRET` and `-p 1` on top of `TEST_DATABASE_URL`; without them 42 and 13 pre-existing tests fail, which reads as a feature regression.
- **Fix task**: record the full command in tasks.md's Gate Check Commands, and in AGENTS.md §3 if it generalizes.
- **Priority**: Minor.

### Fix B: AC P2-2 describes a type-vs-operator validation that does not exist

- **Root cause**: `translateClause` (`internal/provisioner/policy.go:231-233`) rejects operators against the global `policyOperators` allowlist, not against the column's type. `LIKE` is rejected for every column, uuid or not. The AC claims it is "mesma validação de tipo já aplicada às demais colunas `uuid`".
- **Fix task**: reword AC P2-2 to state the real mechanism (global operator allowlist), or, if per-type validation is actually wanted, spec it as new work. Do not change the test — it asserts the real behavior.
- **Priority**: Minor (spec wording, no behavior change).

### Fix C: P1-6's "byte a byte" claim has no literal test (carried over)

- **Root cause**: T2's Done-when asks for a regression test comparing the generated query before/after for `""`/`"owner"`/`"enabled"`. Coverage is behavioral (predicate tables + green pre-existing suite), never a generated-SQL string comparison.
- **Fix task**: either add a golden-SQL assertion for one `owner`/`enabled` list+update+delete triple, or soften the AC to "behaviorally unchanged".
- **Priority**: Minor.

### Fix D: harden M1's kill mechanism

- **Root cause**: `setupPolicyModeFixture` uses a non-UUID JWT `sub`, so the reverted-owner-filter mutant dies as a 500 cast error rather than a 404. The kill is reliable but the assertion does not pin the intended semantics.
- **Fix task**: issue the fixture's JWT with a real UUID `sub` (a second `_auth_users` row), so `/Update` under the mutant yields 404 and `/Delete` yields 404/0-affected.
- **Priority**: Minor.

### Fix E: spec.md traceability statuses are half-updated

- **Root cause**: the fix commits flipped only the requirements tied to the round-1 gaps.
- **Fix task**: set RLSP-03/04/05/07/08 to `Verified` per the table below.
- **Priority**: Cosmetic.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| RLSP-01 | ❌ Needs Fix (round 1) | ✅ Verified — Sensor M1 now kills the UPDATE/DELETE mutant |
| RLSP-02 | ⚠️ Verified with gap (round 1) | ✅ Verified — get-by-id and write-side fail-closed now covered |
| RLSP-03 | Implementing | ✅ Verified |
| RLSP-04 | Implementing | ⚠️ Verified with spec-precision gap (behavioral regression only, no SQL-string comparison) |
| RLSP-05 | Implementing | ✅ Verified |
| RLSP-06 | Verified | ⚠️ Verified with spec-precision gap (AC premise wrong; behavior and message assertion correct) |
| RLSP-07 | Implementing | ⚠️ Verified — backend halves (P3-2/P3-3) covered; the UI warning has no automated coverage, accepted per the Test Coverage Matrix |
| RLSP-08 | Implementing | ✅ Verified |
| RLSP-09 | Verified | ✅ Verified — message content now asserted (Sensor M3) |
| RLSP-10 | Verified | ✅ Verified — `resolveTableRLS("policy")` pass-through now covered (Sensor M2) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 13/13 ACs covered with `file:line` evidence; 2 spec-precision gaps flagged (P1-6, P2-2); 1 layer accepted without tests by the Test Coverage Matrix (P3-1, frontend)
**Sensor**: 5/5 mutations killed this round, including the round-1 survivor re-injected verbatim (P0-full depth; 11 mutations cumulative)
**Gate**: 738 passed, 0 failed, 1 justified skip under `-p 1` with `TEST_DATABASE_URL` + `DASHBOARD_BOOTSTRAP_SECRET`; build/vet/gofmt/tsc/npm all clean

**What works**: the authorization change the feature exists to make is now discriminated at every call site it touches. `AutoScopesByOwner` vs `HasOwnerColumn` cleanly separates "has the column" from "filters by it", which is what keeps INSERT working in policy mode. Fail-closed is proven against a real Postgres for SELECT, UPDATE and DELETE via `SET LOCAL ROLE zeep_app_enduser`, not mocked. The cross-user visibility case that motivated the spec is proven end to end through the HTTP layer. Mode-switch RLS enablement, data preservation, and the one-way ratchet each have their own test. The two pre-existing `enabled` gaps (Data Browser, OpenAPI) were fixed and pinned. Rejection paths now assert message content, so a wrong-but-non-nil error cannot pass. Documentation obligations under AGENTS.md §6 are met.

**Issues found**: 5 minor/cosmetic items, none blocking — Fix A (tasks.md gate command incomplete), Fix B (AC P2-2 wording), Fix C (P1-6 literal SQL comparison), Fix D (M1 kill mechanism precision), Fix E (spec.md statuses half-updated).

**Next steps**: ship. Fixes A-E are cheap cleanups that can ride the next touch of this area; none of them change behavior or coverage of a spec requirement.
