# Modo RLS "policy" (rls-policy-mode) Validation

**Date**: 2026-08-12
**Spec**: `.specs/features/rls-policy-mode/spec.md`
**Diff range**: `a999e9b..6d4d481` (11 commits, `7bd9571`..`6d4d481`)
**Verifier**: independent sub-agent (author ≠ verifier)

**Verdict: ❌ FAIL.** The build gate is green and 5 of 6 injected mutants died, but a 6th mutant survived the full suite: removing `filterOwner(...)` from the `HandleUpdate` and `HandleDelete` call sites in `internal/server/handler.go` (restoring the `owner_id = $sub` filter on `rls: "policy"` UPDATE/DELETE) leaves every test in the repository passing. Spec AC P1-4 explicitly names UPDATE/DELETE, and T2's "Done when" names all four operations, so half of that criterion is asserted only through the `filterOwner` helper's unit table, never through the wiring.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 Predicados centrais (`internal/config/rls.go`) | ✅ Done | 3 funcs, 18 asserts across 3 table tests |
| T2 `resolveOwner`/`filterOwner` decoupling | ⚠️ Partial | Code correct; UPDATE/DELETE call-site wiring not covered by any test (see Sensor mutation 6) |
| T3 `EnsureRowLevelSecurity` extracted | ✅ Done | Real-DB test + idempotency test; `table_policies_store.go` switched to helper |
| T4 `createTable`/`addMissingColumns` recognize `"policy"` | ✅ Done | Fail-closed proven against real Postgres via `SET LOCAL ROLE zeep_app_enduser` |
| T5 `owner_id` in policy clauses | ✅ Done | Exact SQL string asserted |
| T6 enum validation + auth-email gate | ✅ Done | - |
| T7 `UpdateAppTable` enables RLS on switch | ✅ Done | 3 new tests (enable, data preserved, one-way ratchet) |
| T8 `docs/generator.go` | ✅ Done | New `generator_test.go`, 4 cases incl. negative |
| T9 Data Browser | ✅ Done | Exercises the real `ListDataBrowserApps` handler, 3 cases incl. negative |
| T10 Frontend option + warning | ⚠️ Partial | Ships and builds clean; zero automated coverage (matrix says `none` for this layer, so accepted) |
| T11 End-to-end integration | ⚠️ Partial | 5 subtests green; fixture reproduces `createTable`'s DDL by hand instead of calling the provisioner, and the fail-closed case covers list only, not get-by-id/update/delete |

`design.md` is present in the tree but **untracked** — it never got committed with the feature, unlike `spec.md`/`tasks.md`.

---

## Spec-Anchored Acceptance Criteria

### P1: Modo `rls: "policy"` sem auto-scope de dono

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| P1-1 / RLSP-02: WHEN table created with `rls:"policy"` THEN provisioner enables RLS at creation, before any policy | `relrowsecurity = true` right after `createTable`, zero policies | `internal/provisioner/table_test.go:152` — `if !rowSecurityEnabled(t, pool, schema, "posts") { t.Fatal("expected RLS enabled on a rls:policy table right after createTable, before any policy exists") }` | ✅ PASS |
| P1-2 / RLSP-02: WHILE no `select` policy for the user's role, `GET /{app}/{table}` returns zero rows via native enforcement | HTTP 200 + `data` length 0 | `internal/server/rls_policy_mode_test.go:185-187` — `if len(data) != 0 { … }`; DB-level proof at `:213-215` — `if count != 0 { … "RLS deve negar tudo sem policy" }`; provisioner-level proof at `internal/provisioner/table_test.go:184-186` — `if count != 0 { … "fail-closed: RLS enabled, zero policies" }` | ⚠️ Partial — the list half is covered three ways; **`GET /{app}/{table}/{id}` is never exercised under fail-closed conditions** in any test |
| P1-3 / RLSP-01: WHEN a `select` policy without a row-restricting clause exists for a role THEN that user sees all rows, including other users' | 2 rows returned, both seeded IDs present | `internal/server/rls_policy_mode_test.go:256-267` — `if len(data) != 2 { … }` and `if !seen[postAID] \|\| !seen[postBID] { … }` | ✅ PASS |
| P1-4 / RLSP-01: The system SHALL NOT apply `WHERE owner_id = $sub` (or the UPDATE/DELETE equivalent) to any operation on a `rls:"policy"` table | No `owner_id` predicate on **list, get, update, delete** | list/get: `internal/server/handler_test.go:615-617` — `if len(data) != 1 { … "nenhum filtro owner_id em rls:policy" }` and `:626-628` — `if rec.Code != http.StatusOK { … }`; predicate: `internal/server/handler_test.go:486-505` — `filterOwner("user-123", &registry.Table{RLS:"policy"}) == ""` | ❌ **GAP** — no evidence for the UPDATE and DELETE call sites. Sensor mutation 6 (reverting both to `ownerID`) survived the full suite |
| P1-5 / RLSP-03: IF a `rls:"policy"` table receives a valid INSERT THEN `owner_id` is filled with the authenticated `sub` | Response `owner_id` == the JWT `sub` | `internal/server/handler_test.go:670-672` — `if ownerID != creatingUserID { … }`; `internal/server/rls_policy_mode_test.go:327-329` — `if ownerID != userAID { … }`; `internal/server/handler_test.go:471` — `{"policy + user → real owner_id (still populated for INSERT)", "policy", userCtx, "user-123", true}` | ✅ PASS |
| P1-6 / RLSP-04: The system SHALL keep `""`/`"owner"`/`"enabled"` behavior byte-identical | No generated-SQL change for those modes | `internal/provisioner/table_test.go:286-288` — `if rowSecurityEnabled(...) { … "expected RLS NOT enabled at creation (still lazy…)" }`; `internal/config/rls_test.go:59-60` — `{"owner", true}, {"enabled", true}` for `AutoScopesByOwner`; `internal/server/handler_test.go:489-491` — `filterOwner` returns `"user-123"` for owner/enabled; full pre-existing suite (734 tests) green with zero assertion edits | ⚠️ Spec-precision gap — spec says "byte a byte"/T2 says "teste de regressão comparando query antes/depois"; no test compares generated SQL strings before/after. Covered behaviorally, not literally |
| P1-7 / RLSP-09: IF `rls` is not one of `""`/`"owner"`/`"enabled"`/`"policy"` THEN validation rejects with a clear error | Rejection with an error naming the accepted values | `internal/dashboard/handler_test.go:112-115` — `err := validateTableInput(tbl /* RLS:"disabled" */, true, nil); if err == nil { t.Fatal("expected error for unrecognized rls value, got nil") }`; `internal/config/rls_test.go:16-19` — `{"disabled", false}, {"polcy", false}, {"enable", false}, {"OWNER", false}` | ⚠️ Spec-precision gap — the test asserts only `err != nil`, never the message content, while the spec requires "erro claro"; the implementation does list the accepted values (`internal/dashboard/handler.go:130`) |

### P2: `owner_id` referenciável em cláusula de policy

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| P2-1 / RLSP-05: WHEN a policy references `owner_id` THEN `translateClause` accepts it as a valid `uuid` column | Clause translates to `"owner_id" = current_setting('app.jwt_sub', true)::UUID` | `internal/provisioner/policy_test.go:629-632` — `want := "\"owner_id\" = current_setting('app.jwt_sub', true)::UUID"; if !strings.Contains(sql, want) { … }` | ✅ PASS |
| P2-2 / RLSP-06: IF a clause uses an operator incompatible with `uuid` (e.g. `LIKE`) THEN reject with a clear error | Rejection | `internal/provisioner/policy_test.go:648-651` — `if err == nil { t.Fatal("expected error for owner_id with LIKE …") }` | ⚠️ Spec-precision gap — only `err != nil` is asserted, not the message |
| P2-3 / RLSP-06: The system SHALL keep rejecting any column not in `table.Columns` and not `owner_id` | `id`/`updated_at`/`deleted_at` still rejected as unknown column | `internal/provisioner/policy_test.go:671-674` — loop over `{"id","updated_at","deleted_at"}`, `if err == nil { t.Fatalf("column %q: expected error …") }`; pre-existing `TestBuildPolicySQL_RejectsUnknownColumn:157` unchanged | ✅ PASS |

**Scope note (not an AC violation):** `internal/provisioner/policy.go:162` injects `owner_id` into `colByName` **unconditionally**, so a clause on an `rls: ""` table (which has no `owner_id` column) now passes the builder's validation and would only fail later in Postgres. P2-1 scopes the requirement to `"policy"`/`"owner"`/`"enabled"`. Low practical impact (policies only exist on RLS tables) and no test covers the `""` case either way.

### P3: Troca de modo em tabela existente via Dashboard

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| P3-1 / RLSP-07: WHEN the admin switches RLS between `"enabled"` and `"policy"` THEN an explicit warning appears before confirming | Warning shown before the change is applied | `internal/dashboard/ui/src/components/TableCard.tsx:182-184` — `if (!isDraft && isPolicyRLS(val) !== isPolicyRLS(rls)) { if (!confirm(t("tableCard.rlsModeSwitchConfirm"))) return; }`; strings at `src/locales/en.json:737` / `pt-BR.json:737` | ⚠️ No test evidence — accepted per tasks.md Test Coverage Matrix (`Frontend … Required Test Type: none`, build gate only). Zero automated coverage for the only P3 user-facing AC |
| P3-2 / RLSP-07: WHEN confirmed THEN the change applies without recreating the table or losing data | Pre-existing row survives the switch | `internal/dashboard/apps_store_test.go:440-443` — `if count != 1 { t.Fatalf("expected the pre-existing row to survive the mode switch, got count=%d", count) }` | ✅ PASS |
| P3-3 / RLSP-08: IF the target table has no RLS enabled (legacy `"enabled"`, no policies) THEN switching to `"policy"` enables RLS then, preserving P1-2's fail-closed | `relrowsecurity` false before, true after | `internal/dashboard/apps_store_test.go:400-406` — `if relRowSecurityEnabled(...) { t.Fatal("expected RLS disabled before switching…") }` then `if !relRowSecurityEnabled(...) { t.Fatal("expected RLS enabled after switching to policy mode") }`; ratchet at `:477-479` | ✅ PASS (the "all users now see `[]`" half of P3's Independent Test is proven indirectly, via the same mechanism at `internal/provisioner/table_test.go:184-186`, not against a switched table) |

### RLSP-10: superfícies que só reconheciam `"owner"`/`"enabled"`

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --------- | -------------------- | ----------------------- | ------ |
| Auth-email gate treats `"policy"` like `"owner"`/`"enabled"` | Rejected without email auth, accepted with | `internal/dashboard/handler_test.go:132-135` — `err := validateTableInput(tbl /* RLS:"policy" */, false, nil); if err == nil { … }`; `:121-123` — accepted with `authEmailEnabled=true` | ✅ PASS |
| OpenAPI schema exposes `owner_id` for `"policy"` (uuid, readOnly, required) | `{type: string, format: uuid, readOnly: true}` + in `required` | `internal/docs/generator_test.go:23-25` — `if prop.Type != "string" \|\| prop.Format != "uuid" \|\| !prop.ReadOnly { … }`; `:33-35` — present in `schema.Required`; regressions at `:46`, `:59`, negative at `:72` | ✅ PASS |
| Data Browser lists `owner_id` for `"owner"`/`"enabled"`/`"policy"` | `owner_id` in the table's column list | `internal/dashboard/handler_test.go:230-232` (`"policy"`), `:221-223` (`"enabled"`), negative `:240-242` (`""`) — via the real `ListDataBrowserApps` handler | ✅ PASS |
| `resolveTableRLS` never picks `"policy"` implicitly; empty `rls` still defaults to `"enabled"` | Default stays `"enabled"` | `internal/dashboard/table_rls_test.go:13` — `{"omitted + require + auth → enabled", "", true, true, "enabled"}` (pre-existing, untouched; `resolveTableRLS` itself unchanged by the diff) | ⚠️ Partial — the default is pinned, but no case asserts `resolveTableRLS("policy", …) == "policy"` (explicit `"policy"` passes through) |

**Status**: ❌ Gaps present — 1 hard AC gap (P1-4, UPDATE/DELETE), 2 partials (P1-2 get-by-id, RLSP-10 `resolveTableRLS`), 4 spec-precision gaps.

---

## Discrimination Sensor

Scratch: `git worktree add … HEAD --detach` (two throwaway worktrees), mutated there, `git worktree remove --force` after each run. `internal/dashboard/static/` was copied in (it is generated, not tracked) so the `//go:embed` in `internal/dashboard/embed.go` could resolve.

| # | File:line | Description | Killed? |
| - | --------- | ----------- | ------- |
| 1 | `internal/config/rls.go:35` | `AutoScopesByOwner`: added `"policy"` to the true-case (re-enables the auto owner filter for policy mode) | ✅ Killed — `TestAutoScopesByOwner`, `TestFilterOwner/policy…`, `TestPolicyMode_ListAndGetSeeOtherUsersRow/{List,GetByID}`, `TestRLSPolicyMode_EndToEnd/REST_SelectPolicyWithoutRowClauseShowsOtherUsersRows` |
| 2 | `internal/provisioner/table.go:148-152` | Removed the `if rls == "policy" { EnsureRowLevelSecurity(...) }` block from `createTable` | ✅ Killed — `TestCreateTable_PolicyModeEnablesRLSAtCreation` |
| 3 | `internal/provisioner/policy.go:162` | Renamed the injected clause column `owner_id` → `owner_uid` | ✅ Killed — `TestBuildPolicySQL_OwnerIDReferenceableInClause` + 3 subtests of `TestRLSPolicyMode_EndToEnd` |
| 4 | `internal/dashboard/apps_store.go:537` | `if rls == "policy"` → `if rls == "owner"` (mode switch never enables RLS) | ✅ Killed — `TestUpdateAppTable_SwitchToPolicy_EnablesRowLevelSecurity`, `TestUpdateAppTable_SwitchPolicyToEnabled_KeepsRowLevelSecurityEnabled` |
| 5 | `internal/dashboard/handler.go:129` | `if !config.ValidRLS(t.RLS)` → `if false` (enum validation disabled) | ✅ Killed — `TestValidateTableInputRejectsUnknownRLS` |
| 6 | `internal/server/handler.go:278` and `:332` | `filterOwner(ownerID, table)` → `ownerID` in `query.BuildUpdate` and `query.BuildDelete` (re-applies `WHERE owner_id = $sub` to UPDATE/DELETE on `rls:"policy"` tables) | ❌ **Survived** — full suite (`go test -count=1 -p 1 ./...`) green: 734 pass, 0 fail |

**Sensor depth**: P0-full (data-integrity/authorization path) — 6 behavior-level mutations, all branches of the new predicates plus every new call site.
**Result**: 5/6 killed — ❌ FAIL
**Isolation**: `git status --porcelain` before and after is identical (`?? .specs/features/rls-policy-mode/design.md`, `?? internal/dashboard/ui/package-lock.json`). No worktree left behind (`git worktree list` shows only the real tree). `git stash` was never used.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ Two named predicates replacing six duplicated conditions; no enum type ceremony (design.md option B, correctly chosen over C) |
| Surgical changes | ✅ Every touched line traces to a task; `table_policies_store.go` change is the extraction's mandatory other half |
| No scope creep | ✅ The two pre-existing gaps fixed for free (`enabled` missing from the Data Browser and from the OpenAPI schema) are explicitly declared in design.md and covered by their own regression tests |
| Matches patterns | ✅ `Test<Verb><Entity>_<Scenario>` naming, `TEST_DATABASE_URL` skip guard, pool-RLS/pool-owner fixture shape all follow `rls_policy_test.go`; server errors stay in English (AGENTS.md §4) |
| Spec-anchored outcome check | ⚠️ 4 rejection assertions check only `err != nil` where the spec asks for a "clear error" (`dashboard/handler_test.go:114`, `:134`, `provisioner/policy_test.go:650`, `:673`) |
| Per-layer Coverage Expectation met | ❌ `internal/server` route layer does not cover UPDATE/DELETE for `rls:"policy"` (happy/edge path missing for 2 of 5 routes in scope); `internal/config`, `internal/provisioner`, `internal/dashboard`, `internal/docs` all meet theirs |
| Every test maps to a spec requirement | ✅ All 30 new test functions carry a comment naming the AC/task they cover; no unclaimed tests |
| Documented guidelines followed | ❌ AGENTS.md §6: `CHANGELOG.md` has **no** `[Unreleased]` entry for this feature, and `README.md`'s feature table (line 77-78) still describes only `rls: owner` + end-user row policies, so the 3 translated READMEs (`i18n/README.pt-BR.md`, `.pt-PT.md`, `.es.md`) are likewise unchanged. AGENTS.md §3 gate commands were run and are green |

Additional observations (not blocking):

- `internal/server/handler_test.go`'s `setupPolicyModeFixture` and `internal/server/rls_policy_mode_test.go`'s `setupRLSPolicyModeFixture` both use the schema/app name `rls_policy_mode_test_app` in the same package. Safe today only because neither calls `t.Parallel()`; adding parallelism to either would make them destroy each other's fixture.
- `TestRLSPolicyMode_EndToEnd`'s subtests are order-dependent: `DataBrowserOwnerPoolSeesEveryRowRegardlessOfPolicy` hardcodes `count != 3`, which assumes `REST_InsertStillPopulatesOwnerID` ran first.
- T11's fixture builds the `posts` DDL and the `ALTER TABLE … ENABLE ROW LEVEL SECURITY` by hand rather than calling `provisioner.createTable`, so the end-to-end test does not exercise the provisioner path it documents (that path is covered separately in `internal/provisioner/table_test.go`).
- `.specs/features/rls-policy-mode/design.md` is untracked — not committed with the feature.

---

## Edge Cases

- [ ] **IF a `rls:"policy"` table receives DELETE or UPDATE with no matching policy THEN the operation is denied (0 rows affected)** — **NOT covered.** `internal/provisioner/table_test.go:167-186` grants only `SELECT` to `zeep_app_enduser` and asserts only `SELECT COUNT(*) == 0`; no test issues an UPDATE or DELETE as the enduser role against a zero-policy table. T4's "Done when" listed this explicitly.
- [x] IF the admin tries `rls:"policy"` on an app without email auth THEN reject — `internal/dashboard/handler_test.go:132-135`.
- [x] WHEN the Data Browser lists a `rls:"policy"` table THEN all rows are shown, no policy required — `internal/server/rls_policy_mode_test.go:341-343` (`count != 3` via the owner pool) and `internal/dashboard/handler_test.go:230-232` (column list).
- [x] WHEN the OpenAPI generator processes a `rls:"policy"` table THEN the schema includes `owner_id` (uuid, readOnly) — `internal/docs/generator_test.go:23-25`, `:33-35`.
- [~] WHEN `resolveTableRLS` and the auth-email gate evaluate `rls:"policy"` THEN both treat it like `"owner"`/`"enabled"`, and the empty-`rls` default stays `"enabled"` — auth-email gate fully covered (`handler_test.go:132`, `:121`); the `"enabled"` default is pinned by pre-existing `table_rls_test.go:13`; explicit `"policy"` pass-through through `resolveTableRLS` is untested.

---

## Gate Check

- **Gate command** (tasks.md, Build + Full levels):
  - `go build ./... && go test ./... && go vet ./... && gofmt -l $(git diff --name-only a999e9b..HEAD -- '*.go')`
  - `cd internal/dashboard/ui && npx tsc -b && npm run build`
- **Result**: 734 passed, 0 failed, 1 skipped. `go build` exit 0, `go vet` exit 0, `gofmt -l` empty. `npx tsc -b` exit 0, `npm run build` exit 0. Both locale JSONs parse.
- **Environment required to reach green** (not documented in tasks.md, worth adding):
  - `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable` (the repo's own `docker-compose.yml` `db` service, container `zeep-orbit-db-1`)
  - `DASHBOARD_BOOTSTRAP_SECRET` set — without it 42 pre-existing webhook tests fail with `crypto: neither WEBHOOK_TOKEN_ENCRYPTION_KEY nor DASHBOARD_BOOTSTRAP_SECRET is set`
  - `-p 1` — with Go's default parallel package execution, 20 `internal/server` webhook tests fail from cross-package contention on the shared database. **Verified pre-existing**: the identical 20 failures reproduce at the baseline commit `a999e9b` in a clean worktree, so this is not caused by this feature.
- **Test count before feature**: 429 top-level `func Test`
- **Test count after feature**: 459 top-level `func Test`
- **Delta**: +30 test functions (new files: `internal/config/rls_test.go`, `internal/provisioner/table_test.go`, `internal/docs/generator_test.go`, `internal/server/rls_policy_mode_test.go`). No test deleted, no assertion weakened — the only edit to an existing test body is `apps_store_test.go:344`, a mechanical arity fix for `UpdateAppTable`'s new `schemaName` parameter.
- **Skipped tests**: 1 — `TestInstallationAutoAccess` (pre-existing, needs GitHub App credentials). Justified.
- **Failures**: none under the documented gate conditions.

---

## Fix Plans

### Fix 1: `rls:"policy"` UPDATE/DELETE have no test proving the owner filter is gone (surviving mutant)

- **Root cause**: `HandleUpdate` (`internal/server/handler.go:278`) and `HandleDelete` (`:332`) correctly pass `filterOwner(ownerID, table)`, but nothing asserts it. `TestPolicyMode_ListAndGetSeeOtherUsersRow` covers only list and get; `TestFilterOwner` covers the helper in isolation, so a regression at either call site is invisible. Spec AC P1-4 names UPDATE/DELETE explicitly, and T2's "Done when" lists all four operations.
- **Fix task**: extend `TestPolicyMode_ListAndGetSeeOtherUsersRow` (or add `TestPolicyMode_UpdateAndDeleteReachOtherUsersRow`) in `internal/server/handler_test.go`, reusing `setupPolicyModeFixture` (which already seeds a row owned by a different user and enables no RLS): `PATCH /{basePath}/{otherUserRowID}/` from a JWT whose `sub` is a different user must return 200 with the field actually changed, and `DELETE /{basePath}/{otherUserRowID}/` must return success with 1 row affected. Under the old `ownerID` wiring both would 404/0-affected. Verify by re-running sensor mutation 6 and confirming the new tests fail.
- **Priority**: Blocker — this is the authorization behavior the feature exists to change.

### Fix 2: fail-closed is untested for get-by-id, UPDATE and DELETE

- **Root cause**: AC P1-2 covers `GET /{app}/{table}` and `GET /{app}/{table}/{id}`; only the list form is tested (`rls_policy_mode_test.go:171-188`). The spec's first Edge Case ("DELETE ou UPDATE sem policy correspondente … SHALL negar a operação") has no test at all, and `internal/provisioner/table_test.go:167-172` grants only `SELECT` to `zeep_app_enduser`, so the write side of native deny-all is never exercised.
- **Fix task**: (a) add a `REST_NoSelectPolicyReturnsNotFoundForGetByID` subtest to `TestRLSPolicyMode_EndToEnd` using `bearerNoPolicy` against `basePath+"/"+postAID+"/"`; (b) in `TestCreateTable_PolicyModeEnablesRLSAtCreation`, grant `UPDATE, DELETE` alongside `SELECT` and assert both statements report 0 rows affected under `SET LOCAL ROLE zeep_app_enduser`.
- **Priority**: Major.

### Fix 3: rejection assertions do not check the error message

- **Root cause**: `dashboard/handler_test.go:114`, `:134` and `provisioner/policy_test.go:650`, `:673` assert only `err == nil` → fail. AC P1-7 and P2-2 require a "clear error"; a future refactor could return a wrong-but-non-nil error and stay green.
- **Fix task**: assert on the message — e.g. `strings.Contains(err.Error(), "invalid rls value")` for the enum case and `"unknown column"` / operator-allowlist wording for the policy cases.
- **Priority**: Minor.

### Fix 4: documentation out of sync (AGENTS.md §6)

- **Root cause**: `CHANGELOG.md` has no `[Unreleased]` entry for `rls: "policy"`, and `README.md`'s feature table still documents only `rls: owner`, so the 3 translated READMEs are stale by omission too. AGENTS.md §6 requires both in the same change. `design.md` is also untracked.
- **Fix task**: add the `[Unreleased] → Added` CHANGELOG entry; update `README.md`'s Row-Level Security row to name the third mode and mirror it into `i18n/README.pt-BR.md`, `i18n/README.pt-PT.md`, `i18n/README.es.md`; commit `design.md`.
- **Priority**: Major (explicit, repeated project rule).

### Fix 5: tasks.md's gate command is not runnable as written

- **Root cause**: reaching a green `go test ./...` requires `DASHBOARD_BOOTSTRAP_SECRET` and `-p 1` on top of `TEST_DATABASE_URL`; without them 42 and 20 pre-existing tests fail respectively, which reads as a feature regression to any future verifier.
- **Fix task**: record the full command in tasks.md's Gate Check Commands (and, if it generalizes, in AGENTS.md §3).
- **Priority**: Minor.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| RLSP-01 | Implementing | ❌ Needs Fix (UPDATE/DELETE call sites undiscriminated — surviving mutant 6) |
| RLSP-02 | Implementing | ⚠️ Verified with gap (write-side fail-closed and get-by-id untested) |
| RLSP-03 | Implementing | ✅ Verified |
| RLSP-04 | Implementing | ⚠️ Verified with spec-precision gap (behavioral regression only, no SQL-string comparison) |
| RLSP-05 | Implementing | ✅ Verified |
| RLSP-06 | Implementing | ✅ Verified |
| RLSP-07 | Implementing | ⚠️ Verified by build gate only — no automated coverage (accepted per Test Coverage Matrix) |
| RLSP-08 | Implementing | ✅ Verified |
| RLSP-09 | Implementing | ✅ Verified (message content unasserted) |
| RLSP-10 | Implementing | ⚠️ Verified with gap (`resolveTableRLS("policy")` pass-through untested) |

---

## Summary

**Overall**: ❌ Not Ready

**Spec-anchored check**: 9/13 ACs matched their spec-defined outcome; 1 hard gap (P1-4), 2 partials (P1-2, RLSP-10), 4 spec-precision gaps
**Sensor**: 5/6 mutations killed (P0-full depth)
**Gate**: 734 passed, 0 failed, 1 skipped (justified); build/vet/gofmt/tsc/npm all clean

**What works**: the design's root-cause fix is sound and well covered where it is covered. `AutoScopesByOwner` vs `HasOwnerColumn` cleanly separates "has the column" from "filters by it", which is what makes INSERT keep working in policy mode. Fail-closed at creation is proven against a real Postgres with `SET LOCAL ROLE zeep_app_enduser`, not mocked. The cross-user visibility case that motivated the spec is proven end to end. Mode-switch RLS enablement, data preservation, and the one-way ratchet each have their own test. The two pre-existing `enabled` gaps (Data Browser, OpenAPI) were fixed and pinned with regression tests. No existing test was weakened or deleted.

**Issues found**:
1. UPDATE/DELETE on `rls:"policy"` tables have no test proving the owner filter is gone → Fix 1
2. Fail-closed untested for get-by-id, UPDATE and DELETE → Fix 2
3. Four rejection tests assert only "some error" → Fix 3
4. CHANGELOG + 4 READMEs not updated; `design.md` uncommitted → Fix 4
5. tasks.md's gate command omits `DASHBOARD_BOOTSTRAP_SECRET` and `-p 1` → Fix 5

**Next steps**: land Fix 1 and Fix 2 (both are additive tests against fixtures that already exist), then re-run the sensor's mutation 6 to confirm it dies. Fix 4 is a documentation obligation that blocks release, not correctness. Fixes 3 and 5 are cheap and can ride along.
