# End-User Row Policies Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/end-user-row-policies/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`go test` samples in `internal/dashboard/*_test.go`, `internal/provisioner/*_test.go`, `internal/server/*_test.go` — all real-Postgres via `TEST_DATABASE_URL`, skip-if-unset pattern) and project guidelines (`AGENTS.md` §3). No frontend unit-test framework exists in `internal/dashboard/ui` (no `vitest`/`jest`, no `*.test.*`/`*.spec.*` files) — only Playwright e2e (`test:e2e`) and the TS build gate.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Provisioner / migration (`internal/provisioner`) | integration (real Postgres) | Idempotency (run twice, no error) + every listed edge case (existing users default to `member`, role bootstrap, GRANT wiring) | `internal/provisioner/*_test.go` | `TEST_DATABASE_URL=... go test ./internal/provisioner/...` |
| `policy.Builder` (clause validation/translation) | unit | All branches; 1:1 to spec ACs ROWPOL-07/08/09/28/29; every rejected-input edge case (bad column, bad operator, injection-shaped literal, unary operator with a value, missing/invalid `logic`) has a test; AND/OR fold parenthesization asserted exactly, not just semantically | `internal/provisioner/policy_test.go` | `go test ./internal/provisioner/...` |
| `db.Pool.WithRLSContext` | integration (real Postgres) | Role switch + GUCs set/reverted + `statement_timeout` preserved (regression) | `internal/db/client_test.go` | `TEST_DATABASE_URL=... go test ./internal/db/...` |
| `auth.Claims`/`IssueJWT` (role claim) | unit + integration | 1:1 to ROWPOL-02/03/04; ROWPOL-02 additionally proven end-to-end through the real login handler (not just `IssueJWT` called directly) | `internal/auth/jwt_test.go` (unit), `internal/auth/handler_test.go::TestLoginEmbedsDBRoleInJWTClaim` (integration, real Postgres) | `TEST_DATABASE_URL=... go test ./internal/auth/...` |
| Store layer (`TablePolicyStore`, `_auth_users.role` migration) | integration (real Postgres) | Key CRUD paths + constraint/error paths (`UNIQUE`, cascade delete) | `internal/dashboard/table_policies_store_test.go` | `TEST_DATABASE_URL=... go test ./internal/dashboard/...` |
| Dashboard HTTP handlers (policy endpoints) | integration/e2e | All routes in scope: happy path + every listed edge case (403 non-admin, 400 invalid clause, 409 duplicate) + error paths | `internal/dashboard/table_policies_handler_test.go` | `TEST_DATABASE_URL=... go test ./internal/dashboard/...` |
| `internal/server/handler.go` (WithRLSContext swap) | integration (real Postgres) | Regression: existing owner-RLS behavior unchanged + new native-RLS enforcement proven end-to-end (ROWPOL-06/10/11/13/14/15) | `internal/server/handler_test.go` | `TEST_DATABASE_URL=... go test ./internal/server/...` |
| Config / entity (`ColumnConfig`, `TablePolicyRow` structs) | none | build gate only | - | `go build ./...` |
| `internal/config.validateReference` (FK to `_auth_users`) | integration (real Postgres — proves the generated FK is enforced, not just that validation passes) | 1:1 to ROWPOL-21/22/23/24: valid case, wrong type, wrong target column, FK violation on insert | `internal/config/validate_test.go`, `internal/provisioner/table_test.go` | `TEST_DATABASE_URL=... go test ./internal/config/... ./internal/provisioner/...` |
| Dashboard UI (React components, hooks) | none (no unit-test infra in repo) | build gate only (`tsc -b` + `vite build`); behavior verified manually per Independent Test in spec | `internal/dashboard/ui/src/**` | `cd internal/dashboard/ui && npx tsc -b && npm run build` |

## Gate Check Commands

> Generated from `Makefile`, `internal/dashboard/ui/package.json`, and `AGENTS.md` §3 ("Before considering any change done").

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After tasks touching only pure-unit Go code (no DB) | `go test ./internal/provisioner/... ./internal/auth/...` |
| Full | After tasks touching DB-backed Go code (store/handler/provisioner integration) | `TEST_DATABASE_URL=<dsn> go test ./... && go vet ./... && gofmt -l <changed files>` |
| Build | After phase completion, or any task touching the dashboard UI | Full gate, plus: `cd internal/dashboard/ui && npx tsc -b && npm run build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Identity Foundation

```
T1 → T2
T3 → T4
```

### Phase 2: RLS Enforcement Engine

```
T3 → T5
T2 → T6
T5 → T6
T4 → T8
T7 → T8
T8 → T9
```

### Phase 3: Policy API

```
T8 → T10
```

### Phase 4: End-to-End Security Proof

```
T6 → T11
T9 → T11
T10 → T11
```

### Phase 5: Dashboard UI

```
T10 → T12
T12 → T13
T13 → T14
T14 → T15
```

### Phase 6: Docs

```
T15 → T16
```

### Phase 7: FK explícito para `_auth_users`

```
T17
```

---

## Task Breakdown

### T1: Add `role` column to `_auth_users`

**What**: Idempotent migration adding `role TEXT NOT NULL DEFAULT 'member'` to `_auth_users` for every app schema, run from the same code path as the rest of auth-user column migrations.
**Where**: `internal/provisioner/auth.go`
**Depends on**: None
**Reuses**: `addMissingAuthUserColumns` pattern (idempotent `ADD COLUMN IF NOT EXISTS`)
**Requirement**: ROWPOL-01, ROWPOL-03, ROWPOL-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Running the migration twice on the same schema does not error (idempotency)
- [x] Existing `_auth_users` rows read back `role = 'member'` after migration, with no manual backfill step
- [x] `role` accepts any string value (no CHECK/enum constraint) — set via the existing generic Data Browser row-edit modal, no new UI needed for this task

**Tests**: integration
**Gate**: full

---

### T2: Add `role` claim to `auth.Claims` and `IssueJWT`

**What**: Extend `Claims` struct with `Role string`; `IssueJWT` takes the user's current `role` and embeds it; login/OAuth call sites pass the value read from `_auth_users.role`.
**Where**: `internal/auth/jwt.go`, `internal/auth/handler.go` (call sites of `IssueJWT`)
**Depends on**: T1
**Reuses**: existing `Claims`/`IssueJWT` structure, no signature pattern invented
**Requirement**: ROWPOL-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A JWT issued after login for a user with `role = 'approver'` decodes with claim `role: "approver"`
- [x] A JWT issued for a user still on the `role` default (`member`) decodes with claim `role: "member"`
- [x] All existing `IssueJWT` call sites updated (login, Google OAuth) — no call site left passing a stale/empty role

**Tests**: unit
**Gate**: quick

---

### T3: Bootstrap `zeep_app_enduser` Postgres role

**What**: One-time idempotent bootstrap (`DO $$ ... IF NOT EXISTS $$`) creating `zeep_app_enduser` (`NOSUPERUSER NOBYPASSRLS NOLOGIN`) and granting membership to the connecting/principal role, wired into `ProvisionZeepSystem`.
**Where**: `internal/dashboard/provisioner.go` (or the file housing `ProvisionZeepSystem`'s `stmts` array)
**Depends on**: None
**Reuses**: `ProvisionZeepSystem`'s existing idempotent orchestration pattern
**Requirement**: ROWPOL-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Running `ProvisionZeepSystem` twice does not error and does not duplicate the role
- [x] `pg_roles` shows `zeep_app_enduser` with `rolbypassrls = false`, `rolsuper = false`, `rolcanlogin = false`
- [x] The connecting/principal role can successfully `SET ROLE zeep_app_enduser` (membership granted)

**Tests**: integration
**Gate**: full

---

### T4: Grant `zeep_app_enduser` access on every provisioned schema/table

**What**: Extend schema/table provisioning so every app schema gets `GRANT USAGE ON SCHEMA` + `ALTER DEFAULT PRIVILEGES ... GRANT SELECT/INSERT/UPDATE/DELETE ... TO zeep_app_enduser`, covering both existing schemas (migration pass) and schemas created after this feature ships.
**Where**: `internal/provisioner/provisioner.go` (schema creation step), `internal/provisioner/table.go` (per-table grant for pre-existing schemas)
**Depends on**: T3
**Reuses**: existing schema/table creation orchestration in `provisioner.go`
**Requirement**: ROWPOL-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Creating a brand-new app (new schema) results in `zeep_app_enduser` able to `SELECT`/`INSERT`/`UPDATE`/`DELETE` on a table created afterward in that schema, with no RLS enabled yet (identical behavior to today for that role, since no policy exists)
- [x] An existing app's existing tables also grant access to `zeep_app_enduser` after the migration pass (no app silently excluded)

**Tests**: integration
**Gate**: full

---

### T5: Implement `Pool.WithRLSContext`

**What**: New method on `db.Pool` mirroring `WithTimeout`'s transaction structure, adding `SET LOCAL ROLE zeep_app_enduser` and `SET LOCAL "app.jwt_role"`/`"app.jwt_sub"`/`"app.jwt_email"` before `fn(tx)`, while preserving the existing `statement_timeout` behavior.
**Where**: `internal/db/client.go`
**Depends on**: T3
**Reuses**: `WithTimeout`'s `Begin`/`SET LOCAL`/`fn(tx)`/`Commit`/`defer Rollback` structure
**Requirement**: ROWPOL-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A query run inside `WithRLSContext` executes as `zeep_app_enduser` (verified via `SELECT current_user`)
- [x] The session GUCs (`app.jwt_role`, `app.jwt_sub`, `app.jwt_email`) are readable via `current_setting()` inside `fn`, and unset again on a connection reused from the pool afterward (no leakage between pooled connections)
- [x] `statement_timeout` still applies when `timeoutMs > 0` — passing the same regression test `WithTimeout` already has
- [x] `WithRLSContext` returns an explicit error (not silently no-op) if `zeep_app_enduser` doesn't exist / connecting role lacks membership (per design's Risks & Concerns — never fail open)

**Tests**: integration
**Gate**: full

---

### T6: Swap `internal/server/handler.go` to use `WithRLSContext`

**What**: Replace `pool.WithTimeout` calls in `HandleList`/`HandleInsert`/`HandleUpdate`/`HandleDelete` with `pool.WithRLSContext`, sourcing `RLSClaims` from the `auth.AuthUser`/`Claims` already in request context.
**Where**: `internal/server/handler.go`
**Depends on**: T5, T2
**Reuses**: existing `auth.AuthUser` context extraction already used for `resolveOwner`
**Requirement**: ROWPOL-14, ROWPOL-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] All four handlers use `WithRLSContext` instead of `WithTimeout` — no call site left on the old method
- [x] Existing owner-RLS tests (`rls: owner` tables) still pass unchanged — proves the swap didn't regress the current filter mechanism
- [x] A table with no policy at all still returns/writes rows exactly as before this feature (backward-compat goal from spec)

**Tests**: integration
**Gate**: full

---

### T7: Implement `policy.Builder` (clause validation + SQL translation)

**What**: `BuildPolicySQL(schema, table string, def PolicyDef, tableColumns []config.ColumnConfig) (string, error)` validating clauses against `identRe` + operator allowlist (`=`,`!=`,`IN`,`NOT IN`,`>`,`<`,`>=`,`<=`,`IS NULL`,`IS NOT NULL`) + real column existence, casting via `pgType()`, `quoteLiteral` for literal values via `SELECT quote_literal($1)` round-trip, and folding clauses left-to-right by each clause's `Logic` (`AND`/`OR`) into a fully-parenthesized expression (`((c1 AND c2) OR c3)` — never relies on SQL operator precedence).
**Where**: `internal/provisioner/policy.go` (new file)
**Depends on**: None
**Reuses**: `identRe` (`internal/dashboard/handler.go:85`), `pgType()` (`internal/provisioner/table.go:24-45`)
**Requirement**: ROWPOL-05, ROWPOL-07, ROWPOL-08, ROWPOL-09, ROWPOL-28, ROWPOL-29

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Valid clause set (`requester_id != claim:sub`) produces a `CREATE POLICY` string with correct `USING`/`WITH CHECK` per action
- [x] Column name failing `identRe`, non-existent column, or operator outside the allowlist is rejected with a descriptive error and zero DDL executed
- [x] `value_source: claim` accepts only `role`/`sub`/`email`, rejecting anything else
- [x] Literal values containing SQL metacharacters (`'`, `;`, `--`) are safely embedded via `quote_literal()` round-trip — test asserts the generated SQL is syntactically valid and the metacharacters don't break out of the literal
- [x] Each of `>`,`<`,`>=`,`<=` produces correct SQL against a numeric/date column (claim and literal value sources both tested)
- [x] `IS NULL`/`IS NOT NULL` produce correct unary SQL (no operand); a clause supplying `value_source`/`value` alongside either is rejected
- [x] A 3-clause policy mixing `AND`/`OR` (e.g. `c1 AND c2 OR c3`, `logic` on c2="AND", c3="OR") folds to `((c1 AND c2) OR c3)`, not `(c1 AND (c2 OR c3))` — test asserts the exact parenthesization, not just semantic equivalence
- [x] First clause with a non-empty `Logic`, or any non-first clause with `Logic` outside `{AND, OR}`, is rejected with a descriptive error

**Deviation**: `quoteLiteral` is a pure-Go equivalent of Postgres's `quote_literal()` (doubles `'`, uses `E''` + doubled `\` when a backslash is present) instead of a `SELECT quote_literal($1)` DB round-trip. `BuildPolicySQL`'s signature (per design) takes no `pool`/`ctx`, and this task's Gate is `quick` (no `TEST_DATABASE_URL`) — a DB round-trip inside `BuildPolicySQL` would be both a signature mismatch and untestable under this task's own gate. The escaping is safe regardless (dedicated injection-shaped test asserts exact escaped output).

**Tests**: unit
**Gate**: quick

---

### T8: Implement `TablePolicyStore` + `zeep_system.table_policies`

**What**: New table + Go store (`CreateTablePolicy`, `ListTablePolicies`, `DeleteTablePolicy`) persisting policy metadata and invoking `policy.Builder` + `ENABLE ROW LEVEL SECURITY` (first policy only) + `CREATE POLICY`/`DROP POLICY` execution.
**Where**: `internal/dashboard/table_policies_store.go` (new), migration in `internal/dashboard/provisioner.go`
**Depends on**: T7, T4
**Reuses**: `apps_store.go` transactional CRUD patterns, `UNIQUE` constraint style already used elsewhere in `zeep_system`
**Requirement**: ROWPOL-05, ROWPOL-06, ROWPOL-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Creating the first policy on a table executes `ENABLE ROW LEVEL SECURITY` exactly once (idempotent on subsequent policies for the same table)
- [x] Deleting the last policy for a table's action leaves `ROW LEVEL SECURITY` enabled (default-deny) — no automatic `DISABLE`
- [x] Duplicate `(app_id, table_name, action, pg_policy_name)` rejected by the `UNIQUE` constraint, mapped to a store-level error
- [x] Deleting the parent `app_tables` row cascades to delete its `table_policies` rows

**Deviations**:
- `CREATE POLICY ... TO <roles>` is invalid SQL for app-defined business roles (Postgres's `TO` clause names a real database role). Fixed during implementation: every native policy targets `TO zeep_app_enduser` (the fixed end-user role), and `def.Roles` is folded into the `USING`/`WITH CHECK` expression itself as `current_setting('app.jwt_role', true) = ANY (ARRAY[...])`, ANDed with the clause fold. `internal/provisioner/policy.go` (T7) was amended accordingly — see its updated tests.
- Postgres also enforces a unique policy name per table natively (SQLSTATE `42710`), independent of and prior to our own `UNIQUE` constraint (the DDL runs before the metadata INSERT) — `CreateTablePolicy` maps both to `ErrPolicyAlreadyExists`.
- The cascade-on-table-delete bullet required a small, necessary edit to `apps_store.go`'s existing `DeleteAppTable` (outside this task's listed `Where`) since `table_policies` has no DB-level FK to `app_tables` — noted here rather than silently expanding scope.
- `PolicyDef.Name` (the real Postgres policy name) is admin-supplied in the create-policy payload, not auto-generated — the spec's duplicate-name-on-same-table/action edge case is only meaningful if the name is caller-controlled.

**Tests**: integration
**Gate**: full

---

### T9: Verify RLS enablement doesn't disturb internal routines

**What**: Regression test proving Data Browser reads / purge deletes / provisioner DDL on a table that just had its first policy created (and RLS enabled) behave identically to before — because those code paths never call `WithRLSContext`/`SET ROLE`.
**Where**: `internal/dashboard/purge_test.go` (extend), `internal/provisioner/provisioner_test.go` (extend)
**Depends on**: T8
**Reuses**: existing purge/provisioner test fixtures
**Requirement**: ROWPOL-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Purge job deletes expired rows in a table with an active deny-all policy for some role, run as the principal/owner role, unaffected
- [x] Provisioner can still `ALTER TABLE`/add columns on a table with RLS enabled, run as the principal/owner role, unaffected

**Tests**: integration
**Gate**: full

---

### T10: Policy CRUD HTTP endpoints

**What**: `POST/GET /dashboard/api/apps/{id}/tables/{table}/policies`, `DELETE .../policies/{policyId}` — gated by `ResolveAppRole(...).CanManage()`, wired to `TablePolicyStore`, `InsertAuditLog` on success.
**Where**: `internal/dashboard/handler.go`, `internal/dashboard/server.go` (route registration)
**Depends on**: T8
**Reuses**: `ResolveAppRole`/`CanManage()` gate pattern already used by `app_tables` handlers, `InsertAuditLog` signature
**Requirement**: ROWPOL-05, ROWPOL-11, ROWPOL-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `AppRoleAdmin`/`superadmin` can create/list/delete policies; `AppRoleEditor`/`AppRoleViewer`/non-member get 403
- [x] Invalid clause payload returns 400 with the `policy.Builder` error message (English, per `AGENTS.md` §4)
- [x] Duplicate policy returns 409
- [x] Every successful create/delete writes one `audit_log` row with the acting user, app, table, action

**Deviations**:
- Route uses the table's **name** (`{table}`), not `tableId`, per the spec's own path shape (`.../tables/{table}/policies`) — a separate lookup (`findAppTableByName`) resolves the table's real column list from the already-loaded `AppRow` before calling `CreateTablePolicy`.
- `AppRoleViewer`/non-member: a non-member with **no** `app_members` row at all gets 404, not 403 — `GetApp` returns `ErrNotFound` before the `CanManage()` check ever runs when `ResolveAppRole` yields no effective role. This is existing, pre-established behavior identical to `CreateAppTable`/`UpdateAppTable`/`DeleteAppTable`, not something new introduced here. An actual member below `CanManage` (editor/viewer) does get 403, as required.
- Added `provisioner.ValidationError` (mirrors the existing `provisioner.TypeChangeError` pattern) so the handler can safely distinguish "policy.Builder rejected this input" (400, message is safe to expose) from any other error (500, generic message) via `errors.As`, instead of string-prefix matching — a small, backward-compatible amendment to T7's `BuildPolicySQL` (same error text, now wrapped).
- List is also gated by `CanManage()` (not just read access) — per this task's own Done-when bullet grouping list with create/delete.

**Tests**: integration
**Gate**: full

---

### T11: End-to-end proof of the motivating case

**What**: Full-stack test reproducing the spec's Independent Test for the "policy nativa" story: create `requests` table, policy `FOR UPDATE` role `approver` clause `requester_id != claim:sub`, prove enforcement both via the Orbit REST API and via a raw connection authenticated as `zeep_app_enduser` with the same session GUCs set manually — and that the same table remains fully readable/writable by the Data Browser (principal/owner role) throughout.
**Where**: `internal/server/handler_test.go` (extend) or a new `internal/server/rls_policy_test.go`
**Depends on**: T6, T9, T10
**Reuses**: existing real-Postgres test harness (`db.New(ctx, dsn)`, `TEST_DATABASE_URL` skip pattern)
**Requirement**: ROWPOL-06, ROWPOL-10, ROWPOL-11, ROWPOL-13, ROWPOL-14, ROWPOL-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] User A (`role=approver`) `UPDATE`ing a row where `requester_id = A.id` is denied — via the REST API
- [x] The same denial reproduces on a raw `pgx` connection authenticated as `zeep_app_enduser` with `app.jwt_role='approver'`/`app.jwt_sub=A.id` set manually — proves enforcement is DB-level, not HTTP-level
- [x] User A updating a row where `requester_id != A.id` succeeds via the REST API
- [x] Data Browser (principal/owner role) still lists and edits 100% of rows in `requests` throughout, regardless of the policy

**Deviation**: the fixture also creates a paired, intentionally-broad `SELECT` policy for role `approver` (`requester_id IS NOT NULL`, always true) alongside the `UPDATE` policy under test. Reason: Postgres requires a row to satisfy a `SELECT` policy for `UPDATE ... RETURNING` to return anything at all, even when the `UPDATE` itself succeeded — without it, the "allowed" REST case (rowB) would come back as an empty `RETURNING` (surfacing as 404 despite a successful write), masking the very behavior this task proves. This is what a real admin would also configure in practice (a role that can update rows generally also needs to read them), not a workaround specific to the test.

**Tests**: integration
**Gate**: full

---

### T12: Frontend API client for policies

**What**: `useTablePolicies(appId, table)`, `useCreateTablePolicy()`, `useDeleteTablePolicy()` React Query hooks calling the T10 endpoints.
**Where**: `internal/dashboard/ui/src/lib/api.ts`
**Depends on**: T10
**Reuses**: existing `react-query` hook patterns in `api.ts`, `toast.error(error.message)` convention
**Requirement**: ROWPOL-16, ROWPOL-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Hooks call the correct endpoints with correct payload shapes matching T10's contract
- [x] `onError` present on the mutation hooks, calling `toast.error(error.message)` (per `AGENTS.md` §5 — a mutation hook without `onError` is incomplete)
- [x] `npx tsc -b` passes with no type errors

**Tests**: none
**Gate**: build

---

### T13: "Policies" tab — list + delete

**What**: New tab in the table detail page listing existing policies (action, roles, clauses) with a delete action per row.
**Where**: `internal/dashboard/ui/src/pages/` (table detail page, new tab component)
**Depends on**: T12
**Reuses**: existing tab layout pattern already used on the table detail page
**Requirement**: ROWPOL-16, ROWPOL-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Tab renders the list of policies for the currently selected table, fetched via `useTablePolicies`
- [x] Delete action removes a policy and refreshes the list without a full page reload
- [x] `npx tsc -b && npm run build` passes

**Deviation**: the tab lives inside `TableCard.tsx` (per-table edit view), not a separate route/page — the design's "página de detalhe da tabela" is `TableCard`'s expanded/editing state, there is no standalone table-detail page in this codebase. Added an `appId` prop to `TableCardProps` (not listed in T12's `Where`) since the policy hooks need it and `TableCard` previously had no app context — a required, minimal wiring change, not scope creep. The tab is only shown for a persisted table (`!isDraft`); a draft table has no `name`/`id` yet for the policy endpoints to address.

**Tests**: none
**Gate**: build

---

### T14: Policy builder form

**What**: Form to create a policy — table columns/operators as fixed dropdowns (no free text), `value_source: claim` restricted to `role`/`sub`/`email`, a per-clause `AND`/`OR` connector select from the second clause onward, submits via `useCreateTablePolicy`.
**Where**: `internal/dashboard/ui/src/pages/` (same tab as T13, form component)
**Depends on**: T13
**Reuses**: `filterRules`/`draftCol`/`draftOp`/`draftValue` pattern from `DataBrowserPage.tsx`
**Requirement**: ROWPOL-17, ROWPOL-18, ROWPOL-19, ROWPOL-28, ROWPOL-29

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Column dropdown is populated only from the table's real columns (no free-text input)
- [x] Operator dropdown only offers `=`/`!=`/`IN`/`NOT IN`/`>`/`<`/`>=`/`<=`/`IS NULL`/`IS NOT NULL`
- [x] Selecting `IS NULL`/`IS NOT NULL` hides the value field for that clause (unary operator, nothing to compare)
- [x] Claim dropdown only offers `role`/`sub`/`email` when `value_source` is `claim`
- [x] From the second clause onward, an `AND`/`OR` connector select is shown and required; the first clause never shows it
- [x] Successful submit shows a success toast and refreshes the policy list (T13); failed submit shows `toast.error(error.message)`
- [x] `npx tsc -b && npm run build` passes

**Deviation**: `roles` (business roles, free strings per app — not an Orbit-defined vocabulary, per spec Assumptions) is a comma-separated text input, not a fixed dropdown — there is no allowlist of roles to select from, unlike columns/operators/claims which the spec explicitly restricts to allowlists. `columns` prop added to `TablePoliciesTabProps` (T13 didn't need it); `TableCard` now passes the table's persisted `table.columns` (not the local unsaved-edit draft `columns` state), since the Policies tab addresses the table as it exists in Postgres today.

**Tests**: none
**Gate**: build

---

### T15: i18n strings for the policies UI

**What**: All labels/buttons/messages introduced in T13/T14 added to both locale files.
**Where**: `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T14
**Reuses**: existing `react-i18next` `t()` usage pattern
**Requirement**: ROWPOL-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Every string introduced in T13/T14 uses `t()`, none hardcoded
- [x] Both `en.json` and `pt-BR.json` have the new keys, no key present in one but missing in the other
- [x] `python3 -c "import json; json.load(open('en.json'))"` and same for `pt-BR.json` both succeed
- [x] `npx tsc -b && npm run build` passes

**Deviation**: AGENTS.md §5 requires every new UI string to land in both locale files "in the same change" that introduces it — T13 and T14's own commits already added every key they used to both `en.json`/`pt-BR.json` (verified above: all 62 `t()` keys across `TablePolicies.tsx`/`TableCard.tsx` exist in both files, no hardcoded string found). This task's own diff is therefore just the audit + status update recorded here; no source/locale changes were pending by the time T15 started.

**Tests**: none
**Gate**: build

---

### T16: Update `CHANGELOG.md`

**What**: Add an entry under `## [Unreleased]` describing the new native RLS row-policy feature.
**Where**: `CHANGELOG.md`
**Depends on**: T15
**Reuses**: existing changelog entry style/format
**Requirement**: N/A (documentation, per `AGENTS.md` §6)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Entry added under `## [Unreleased]` in the same change, following the repo's existing entry format

**Tests**: none
**Gate**: build

**Commit**: `docs: add changelog entry for end-user row policies`

---

### T17: Allow FK to `_auth_users.id` on custom columns

**What**: Special-case `_auth_users` in `validateReference` so a column can declare `references: {table: "_auth_users", column: "id"}` without needing `_auth_users` in the app's own table set; require `column == "id"` and the referencing column's `Type == "uuid"`.
**Where**: `internal/config/validate.go`
**Depends on**: None
**Reuses**: `columnDDL`'s existing generic `REFERENCES %q.%q(%q)` generation (`internal/provisioner/table.go:88-89`) — unchanged, already works once validation allows it
**Requirement**: ROWPOL-21, ROWPOL-22, ROWPOL-23, ROWPOL-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A column declared with `references: {table: "_auth_users", column: "id"}` and `type: "uuid"` passes validation and provisions with a real FK to `_auth_users(id)`
- [x] The same declaration with `type != "uuid"` is rejected with 400
- [x] `references: {table: "_auth_users", column: "role"}` (or any column other than `id`) is rejected with 400
- [x] Inserting a row with a `requester_id` not present in `_auth_users` fails with a real Postgres FK violation (not a soft app-level check)
- [x] The relationship appears in the dashboard's existing relationships UI (`schema-relationships-and-indexes`) exactly like any other declared FK — no special-casing needed there since it reads the same `references` metadata

**Note**: the 400-on-invalid-input requirement is satisfied at the `ValidateTables`/`validateReference` layer (returns an error the dashboard's existing create/update-table handlers already turn into 400 — same path every other `references` validation error already takes, unchanged by this task). The UI-relationships bullet needed no code change, exactly as design predicted: that UI reads the same `references` metadata already returned for every column, with no `_auth_users`-specific branch.

**Tests**: integration
**Gate**: full

**Commit**: `feat(config): allow explicit FK from custom columns to _auth_users`

---

## Phase Execution Map

Phase order: Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6.

Full dependency edge list (one edge per line — this is the source of truth, identical to each task's `Depends on`):

```
T1 → T2
T3 → T4
T3 → T5
T2 → T6
T5 → T6
T4 → T8
T7 → T8
T8 → T9
T8 → T10
T6 → T11
T9 → T11
T10 → T11
T10 → T12
T12 → T13
T13 → T14
T14 → T15
T15 → T16
```

T17 has no dependency (`Depends on: None`) and no incoming/outgoing edges — it is a standalone phase.

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add `role` column | 1 migration | ✅ Granular |
| T2: JWT role claim | 1 struct + call sites (cohesive) | ✅ Granular |
| T3: Bootstrap Postgres role | 1 migration | ✅ Granular |
| T4: Grant access to schemas/tables | 1 provisioning step | ✅ Granular |
| T5: `WithRLSContext` | 1 method | ✅ Granular |
| T6: Swap handler.go to `WithRLSContext` | 1 file, 1 concern (method swap) | ✅ Granular |
| T7: `policy.Builder` | 1 component (1 new file) | ✅ Granular |
| T8: `TablePolicyStore` | 1 component (1 new file + 1 migration) | ✅ Granular |
| T9: Internal-routine regression | 1 concern (regression proof) | ✅ Granular |
| T10: Policy CRUD endpoints | 1 API surface (3 related routes, cohesive) | ✅ Granular |
| T11: E2E motivating-case proof | 1 test scenario | ✅ Granular |
| T12: Frontend API client | 1 file (3 related hooks, cohesive) | ✅ Granular |
| T13: Policies tab list+delete | 1 component | ✅ Granular |
| T14: Policy builder form | 1 component | ✅ Granular |
| T15: i18n strings | 2 files (locale pair, cohesive) | ✅ Granular |
| T16: Changelog entry | 1 file | ✅ Granular |
| T17: FK to `_auth_users` | 1 function (`validateReference`) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1→T2 | ✅ Match |
| T3 | None | None | ✅ Match |
| T4 | T3 | T3→T4 | ✅ Match |
| T5 | T3 | T3→T5 | ✅ Match |
| T6 | T5, T2 | T5→T6, T2→T6 | ✅ Match |
| T7 | None | None | ✅ Match |
| T8 | T7, T4 | T7→T8, T4→T8 | ✅ Match |
| T9 | T8 | T8→T9 | ✅ Match |
| T10 | T8 | T8→T10 | ✅ Match |
| T11 | T6, T9, T10 | T6→T11, T9→T11, T10→T11 | ✅ Match |
| T12 | T10 | T10→T12 | ✅ Match |
| T13 | T12 | T12→T13 | ✅ Match |
| T14 | T13 | T13→T14 | ✅ Match |
| T15 | T14 | T14→T15 | ✅ Match |
| T16 | T15 | T15→T16 | ✅ Match |
| T17 | None | None (standalone phase) | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Provisioner/migration | integration | integration | ✅ OK |
| T2 | `auth.Claims`/`IssueJWT` (unit-level logic) | unit | unit | ✅ OK |
| T3 | Provisioner/migration | integration | integration | ✅ OK |
| T4 | Provisioner/migration | integration | integration | ✅ OK |
| T5 | `db.Pool.WithRLSContext` | integration | integration | ✅ OK |
| T6 | `internal/server/handler.go` (request path) | integration | integration | ✅ OK |
| T7 | `policy.Builder` | unit | unit | ✅ OK |
| T8 | Store layer | integration | integration | ✅ OK |
| T9 | Server request path (regression) | integration | integration | ✅ OK |
| T10 | Dashboard HTTP handlers | integration/e2e | integration | ✅ OK |
| T11 | Server request path (e2e proof) | integration | integration | ✅ OK |
| T12 | Dashboard UI | none | none | ✅ OK |
| T13 | Dashboard UI | none | none | ✅ OK |
| T14 | Dashboard UI | none | none | ✅ OK |
| T15 | Dashboard UI (i18n, config-like) | none | none | ✅ OK |
| T16 | Docs | none | none | ✅ OK |
| T17 | `internal/config` (validation logic) | integration | integration | ✅ OK |

All ✅ — no restructuring needed.
