# MCP Server for Zeep Orbit Operations Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/mcp-server/design.md`
**Status**: Draft

---

## Scope Note

This task breakdown covers spec P1 (PAT auth + transport, including the OAuth 2.1 flow added for Claude-Desktop-class clients) and P2 (create-app/table/RLS/policy tools) — MCP-01 through MCP-14 and MCP-19 through MCP-24. **P3 ("Create with AI" chat drawer, MCP-15 through MCP-18) is deliberately excluded from this file.** Design's own component note for the chat drawer leaves the LLM-orchestration mechanism as "an implementation choice for `tasks.md`" — that choice (which model, which tool-calling loop library) isn't decided yet, and starting P3 tasks before it is would mean building against an unconfirmed foundation. Once P1/P2 ship and prove the tool set against a real external MCP client, open a follow-up tasks file for P3 alone.

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate commands), §4 (error-string/CSRF/session rules — directly relevant here since PAT resolution is new auth surface). Backend testing convention sampled from `internal/dashboard/webhooks_store_test.go`, `internal/dashboard/table_policies_handler_test.go`: integration tests against a real ephemeral Postgres (no mocking layer exists in this codebase for `db.Pool`), `-race` enabled, `-p 1` when a test touches shared schema state across packages (established as the fix for the pre-existing webhook test-isolation issue, D-175/S1206).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `PATStore` (`pat_store.go`) | integration (real Postgres) | Create/resolve/list/revoke happy path; resolve rejects unknown hash, revoked token, expired ephemeral token, deleted owning user; revoke scoped to owning user only (mirrors the webhook-mapping ownership-scoping fix) | `internal/dashboard/pat_store_test.go` | `go test ./internal/dashboard/...` |
| `RequirePAT` middleware (`internal/mcpserver/auth.go`) | integration | Valid token passes through with correct `DashboardUser` in context; missing/malformed header, unknown token, revoked token all reject with 401 before any tool executes | `internal/mcpserver/auth_test.go` | `go test ./internal/mcpserver/...` |
| `policytemplates` package (`internal/policytemplates/templates.go`) | unit | Every builder's output cross-checked field-for-field against `policyTemplates.ts`'s equivalent function for the same inputs (same `PolicyDef` shape, same generated names) — this is the drift check the design's Risks & Concerns table calls for, made concrete as an actual test rather than a manual promise | `internal/policytemplates/templates_test.go` | `go test ./internal/policytemplates/...` |
| Extracted `*ForUser` operation functions (`handler.go`, `table_policies_handler.go`) | integration | Existing REST-level tests for `CreateApp`/`CreateAppTable`/`CreateTablePolicy`/the RLS-mode branch of `UpdateAppTable` must keep passing unmodified (behavior-preserving-extraction bar from design's Risks & Concerns); plus new tests calling the extracted function directly (no HTTP layer) for the same cases | `internal/dashboard/handler_test.go` (existing, unmodified expectations), `internal/dashboard/*_foruser_test.go` (new) | `go test ./internal/dashboard/...` |
| `GetAppSchemaForUser` / `ListAppsForUser` (new aggregation) | integration | Schema aggregation matches what `GetApp` + table list + policy list would each return independently; empty app (no tables) returns `tables: []`, not null/error | `internal/dashboard/app_schema_test.go` | `go test ./internal/dashboard/...` |
| MCP tool registry (`internal/mcpserver/tools.go`) | integration, real MCP client roundtrip | Each tool callable via an actual MCP client (SDK's own test client, not a hand-rolled JSON fixture) against a running server backed by real Postgres; validation-failure and partial-failure (template) paths return structured tool errors, never a raw Go error string | `internal/mcpserver/tools_test.go` | `go test ./internal/mcpserver/...` |
| Dashboard PAT REST handlers (`pat_handler.go`) | integration | Happy path + validation errors, same depth as `TestCreateWebhookHandler_*`; every action audited | `internal/dashboard/pat_handler_test.go` | `go test ./internal/dashboard/...` |
| OAuth server (`oauth_server.go`, `oauth_client_store.go`) | integration | Every branch from the OAuth Error Handling Strategy table: unknown/mismatched client, denied consent, reused/expired/PKCE-mismatched code, refresh rotation, refresh reuse → family revocation | `internal/dashboard/oauth_server_test.go`, `internal/dashboard/oauth_client_store_test.go` | `go test ./internal/dashboard/...` |
| OAuth consent screen (`OAuthConsent.tsx`) | e2e (no frontend unit runner — see rationale below) | Client name + redirect origin both visible; grant and deny paths both exercised | `internal/dashboard/ui/e2e/oauth-consent.spec.ts` (folded into T21's Go-level test where the redirect chain is easier to assert precisely — see T19/T21) | `go test ./internal/dashboard/...` (primary), `npm run test:e2e` (optional UI-level pass) |
| Frontend (`PersonalAccessTokens.tsx`, Settings wiring) | e2e (this repo has no frontend unit runner — see `policy-templates/tasks.md`'s Test Coverage Matrix for the same finding) | Create a PAT (shown once), list shows it without the value, revoke removes it from the active list | `internal/dashboard/ui/e2e/personal-access-tokens.spec.ts` | `npm run test:e2e` (from `internal/dashboard/ui`, real backend running) |
| i18n JSON (`en.json`, `pt-BR.json`) | none | `AGENTS.md` §5: validated by JSON parse + consumed by `tsc`/`vite build` | `src/locales/en.json`, `src/locales/pt-BR.json` | `python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json'))"` |

**Coverage Expectation rationale**: this repo has zero mocking infrastructure for `db.Pool` — every existing backend test already runs against a real ephemeral Postgres. New MCP-layer tests follow the same convention rather than introducing a mock, which would test a fiction instead of the real DB round-trip PAT resolution depends on.

## Gate Check Commands

> Generated from codebase - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Backend-only task, no frontend touched | `go build ./... && go test ./internal/dashboard/... ./internal/mcpserver/... ./internal/policytemplates/... && go vet ./internal/dashboard/... ./internal/mcpserver/... ./internal/policytemplates/...` |
| Full | Task touches frontend types/hooks/UI, no new/changed e2e spec yet | Quick gate + `cd internal/dashboard/ui && npx tsc -b && npm run build` |
| Build | Phase completion, or task adds/changes the Playwright e2e spec | Full gate + `cd internal/dashboard/ui && npx playwright test personal-access-tokens` + `gofmt -l <changed .go files>` + `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json')); json.load(open('internal/dashboard/ui/src/locales/pt-BR.json'))"` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation

New primitives with no user-visible behavior yet: the auth artifact, the auth middleware, and the ported template logic. Nothing here touches an existing shipped handler.

T1 has no dependency. T2 depends on T1 (needs `PATStore.ResolvePAT`). T3 has no dependency (independent of T1/T2).

### Phase 2: Shared Operation Extraction

Behavior-preserving refactors of already-shipped REST handlers — the highest-risk phase per design's Risks & Concerns, so each task is scoped to exactly one handler.

T4, T5, T6, T7 each depend only on Phase 1 completing (they don't depend on each other — different handlers, no shared code between them beyond what already exists). T8 (new aggregation logic) depends on T4-T7 being done, since it reads the same resources they now expose via `*ForUser` functions.

### Phase 3: MCP Server Wiring

Stands up the actual MCP transport and registers tools against Phase 1/2's building blocks.

T9 depends on T1, T2. T10 depends on T9 (needs the server/registry skeleton) and T8 (read tools). T11 depends on T9 and T4-T6 (write tools). T12 depends on T9, T3, T7 (template tools).

### Phase 4: OAuth Authorization Server

Adds the second token-issuance front door (Claude-Desktop-class clients) onto the same `dashboard_pats` store T1 built. Independent of Phase 3 — can run in parallel with it — but the final end-to-end OAuth test (T21) needs Phase 3's `/dashboard/mcp` route to actually call a tool with the resulting token.

T17 depends on T1. T18 depends on T17 (needs a registered client to validate `redirect_uri` against). T19 depends on T18 (consent screen is part of the `Authorize` flow T18 builds). T20 depends on T17 and T1 (mints/rotates PAT rows). T21 depends on T19, T20, and Phase 3 (T9-T12, to prove the OAuth-issued token actually drives a real tool call).

### Phase 5: PAT Management UI

Session-authenticated CRUD so an admin can actually get a token without touching a database console.

T13 depends on T1. T14 depends on T13.

### Phase 6: Integration & Verification

T15 depends on T10, T11, T12 (needs every tool registered to exercise the full P1+P2 story end-to-end). T16 depends on T14.

---

## Task Breakdown

### T1: `PATStore` + `dashboard_pats` catalog table

**What**: New catalog table `dashboard_pats` (id, user_id FK → `dashboard_users.id` ON DELETE CASCADE, name, token_hash, `kind` — `"manual"|"ephemeral"|"oauth"`, `oauth_client_id` nullable, `refresh_token_hash` nullable, expires_at, revoked_at, last_used_at, created_at), provisioned via the existing `CREATE TABLE IF NOT EXISTS` pattern. `PATStore` with `CreatePAT(ctx, pool, userID, name, kind string, expiresAt *time.Time)` (generates via `generateToken`, stores `sha256(token)` hex, returns plaintext once), `ResolvePAT` (hash lookup, joins `dashboard_users`, rejects revoked/expired/owning-user-gone — same check regardless of `kind`), `ListPATs` (never returns token value), `RevokePAT` (scoped to the requesting user's own tokens), `TouchLastUsed` (best-effort, failure never propagates). `oauth_client_id`/`refresh_token_hash` columns exist now so T17-T20 (OAuth) don't need a second migration.
**Where**: `internal/dashboard/pat_store.go` (new), `internal/dashboard/provisioner.go` (add table migration)
**Depends on**: None
**Reuses**: `generateToken` (`handler.go:1798`), structural pattern from `table_policies_store.go` (catalog row + ownership-scoped mutations + not-found sentinel).
**Requirement**: MCP-01, MCP-04, MCP-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `CreatePAT` returns the plaintext token exactly once; the stored row contains only its SHA-256 hash
- [x] `ResolvePAT` with the correct plaintext returns the owning `DashboardUser`; with a wrong/unknown token returns a not-found error; with a revoked token's plaintext returns a revoked error; with an expired `ephemeral=true` token returns an expired error
- [x] `CreatePAT` with `kind="manual"` sets `expires_at=nil`; with `kind="ephemeral"` or `kind="oauth"` requires a non-nil `expiresAt`
- [x] `RevokePAT` called with another user's PAT id returns a not-found/forbidden error, not success (ownership-scoping test, mirrors the webhook-mapping IDOR fix)
- [x] Deleting the owning `dashboard_users` row cascades to delete its PATs (FK `ON DELETE CASCADE` exercised directly)
- [x] `ListPATs` output never includes `token_hash` in its JSON shape
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `feat(dashboard): add PATStore and dashboard_pats catalog table`

---

### T2: `RequirePAT` middleware + context interop

**What**: `RequirePAT(pool *db.Pool) func(http.Handler) http.Handler` reading `Authorization: Bearer <token>`, calling `PATStore.ResolvePAT`, injecting the resolved `DashboardUser` under the same context key `RequireAuth` uses (export an accessor from `dashboard` if the key itself stays unexported), then firing `TouchLastUsed` asynchronously (never blocking the request on it). Missing header, malformed header, or resolve failure all return a `401` JSON body matching `writeUnauthorized`'s existing shape.
**Where**: `internal/mcpserver/auth.go` (new package+file), `internal/dashboard/middleware.go` (export a context accessor if needed), `internal/dashboard/pat_store.go` (already has `ResolvePAT` from T1)
**Depends on**: T1
**Reuses**: `dashboard.UserFromContext` reader, `writeUnauthorized` response shape.
**Requirement**: MCP-02, MCP-03, MCP-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] A request with a valid PAT reaches the wrapped handler with `dashboard.UserFromContext(ctx)` returning the correct user
- [x] A request with no `Authorization` header, a non-`Bearer` scheme, or an unresolvable token all return `401` before the wrapped handler runs (assert via a spy handler that must never be called)
- [x] `TouchLastUsed` failure (simulate via a closed pool or similar) does not change the response — request still succeeds
- [x] Gate check passes: `go test ./internal/mcpserver/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(mcpserver): add RequirePAT auth middleware`

---

### T3: `policytemplates` Go package, ported from `policyTemplates.ts`

**What**: New pure Go package mirroring `internal/dashboard/ui/src/lib/policyTemplates.ts` function-for-function: `List() []TemplateDefinition`, `GeneratedPolicyName(templateID, action string) string`, `BuildOwnerOnlyPolicies`, `BuildOpenReadPolicy`, `BuildReadOnlyPolicy`, `BuildValueMatchPolicy`, `BuildOpenReadOwnerWritePolicies` — same `TemplateID` constants, same `tpl_<id>_<action>` naming convention, same dummy `owner_id IS NOT NULL` clause for non-filtering templates.
**Where**: `internal/policytemplates/templates.go` (new package)
**Depends on**: None
**Reuses**: `PolicyDef`/`PolicyClause`-equivalent Go types already used by `table_policies_store.go`/`internal/provisioner/policy.go` — no new type, just the existing policy request shape.
**Requirement**: MCP-12 (supports), design's Risks & Concerns (TS/Go drift mitigation — this task's test *is* the mitigation)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Every builder's output, given the same inputs used in `policyTemplates.ts`'s own doc comments (e.g. `BuildOwnerOnlyPolicies(["select","update"], []string{"member"})`), matches the TS function's documented output field-for-field (column/operator/value_source/value, roles, generated name)
- [x] `List()` returns the same 6 template ids in the same order as `TEMPLATE_DEFINITIONS`
- [x] A comment at the top of the file points to `policyTemplates.ts` by path and states explicitly: "keep these in sync manually — see design.md Risks & Concerns" (the drift-prevention documentation the design flagged as a manual obligation)
- [x] Gate check passes: `go test ./internal/policytemplates/...`

**Status**: Complete

**Tests**: unit
**Gate**: quick

**Commit**: `feat(policytemplates): port policy template builders from TypeScript to Go`

---

### T4: Extract `CreateAppForUser` from `CreateApp`

**What**: Move `CreateApp`'s body (validation, store call, audit) into `CreateAppForUser(ctx, pool, user *DashboardUser, input CreateAppInput) (*App, error)`. `CreateApp` (the HTTP handler) becomes: resolve user from context → decode body into `CreateAppInput` → call `CreateAppForUser` → `writeJSON`/`writeError`. No behavior change.
**Where**: `internal/dashboard/handler.go` (modify `CreateApp`, add `CreateAppForUser`)
**Depends on**: Phase 1 complete
**Reuses**: Nothing new — this *is* the reuse mechanism (see design's Shared Operation Functions).
**Requirement**: MCP-06, MCP-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Every existing test for `CreateApp` (`internal/dashboard/handler_test.go`) passes unmodified — zero test file changes, confirming behavior preservation
- [x] New test calls `CreateAppForUser` directly (no `httptest.Request`) for the same success/validation-failure cases the HTTP-level tests already cover, confirming the extraction is callable without an HTTP layer
- [x] `CreateAppForUser` still calls `h.audit(...)` (or the store-level equivalent) exactly where `CreateApp` did — same `audit_log` row shape
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration (existing, unmodified expectations + new direct-call tests)
**Gate**: quick

**Commit**: `refactor(dashboard): extract CreateAppForUser from CreateApp handler`

---

### T5: Extract `CreateAppTableForUser` from `CreateAppTable`

**What**: Same extraction pattern as T4, applied to `CreateAppTable` → `CreateAppTableForUser(ctx, pool, user *DashboardUser, appID string, input CreateTableInput) (*Table, error)`.
**Where**: `internal/dashboard/handler.go` (modify `CreateAppTable`, add `CreateAppTableForUser`)
**Depends on**: Phase 1 complete
**Reuses**: Same mechanism as T4.
**Requirement**: MCP-07, MCP-08, MCP-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Every existing test for `CreateAppTable` passes unmodified
- [x] New test calls `CreateAppTableForUser` directly for success + the existing validation-failure cases (duplicate name, reserved column, bad type)
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `refactor(dashboard): extract CreateAppTableForUser from CreateAppTable handler`

---

### T6: Extract `UpdateTableRLSModeForUser` from `UpdateAppTable`

**What**: Extract only the RLS-mode-relevant slice of `UpdateAppTable`'s body (validation of the RLS value against `""/"owner"/"enabled"/"policy"`, the store call that applies it) into `UpdateTableRLSModeForUser(ctx, pool, user *DashboardUser, appID, tableName, rlsMode string) (*Table, error)`. `UpdateAppTable` itself is otherwise untouched — it still handles column/index changes inline, only delegating the RLS-mode branch to the new function.
**Where**: `internal/dashboard/handler.go` (modify `UpdateAppTable`, add `UpdateTableRLSModeForUser`)
**Depends on**: Phase 1 complete
**Reuses**: Same mechanism as T4, scoped to a slice of a larger handler rather than the whole thing.
**Requirement**: MCP-11, MCP-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Every existing test for `UpdateAppTable` (including its column/index-change paths, untouched by this extraction) passes unmodified
- [x] New test calls `UpdateTableRLSModeForUser` directly for all 4 valid RLS values and the invalid-value rejection case
- [x] `UpdateAppTable`'s non-RLS behavior (column/index updates) is unchanged — confirmed by the existing tests covering those paths still passing
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `refactor(dashboard): extract UpdateTableRLSModeForUser from UpdateAppTable handler`

---

### T7: Extract `CreateTablePolicyForUser` from `CreateTablePolicy`

**What**: Same extraction pattern as T4, applied to `CreateTablePolicy` → `CreateTablePolicyForUser(ctx, pool, user *DashboardUser, appID, tableName string, def PolicyDef) (*PolicyRow, error)`.
**Where**: `internal/dashboard/handler.go` or `table_policies_handler.go` (wherever `CreateTablePolicy` currently lives; modify it, add `CreateTablePolicyForUser`)
**Depends on**: Phase 1 complete
**Reuses**: Same mechanism as T4.
**Requirement**: MCP-13, MCP-14, MCP-10

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Every existing test for `CreateTablePolicy` passes unmodified
- [x] New test calls `CreateTablePolicyForUser` directly for success + the existing validation-failure cases (invalid operator, invalid claim, malformed clause)
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `refactor(dashboard): extract CreateTablePolicyForUser from CreateTablePolicy handler`

---

### T8: `ListAppsForUser` + `GetAppSchemaForUser` (new aggregation logic)

**What**: `ListAppsForUser(ctx, pool, user *DashboardUser) ([]App, error)` (thin wrapper — same query the existing `ListApps` handler already runs, extracted the same way as T4-T7). `GetAppSchemaForUser(ctx, pool, user *DashboardUser, appID string) (*AppSchema, error)` — new aggregation combining `GetApp`, the app's table list (name, rls_mode, columns), and each table's policies (name, action, roles) into one `AppSchema` response. Explicitly new code, not a pure extraction (per design's Tech Decisions), since no existing endpoint returns this exact shape.
**Where**: `internal/dashboard/app_schema.go` (new file)
**Depends on**: T4, T5, T6, T7 (reads the same resources those tasks' extractions manage, needs their final shape settled first to avoid rebasing)
**Reuses**: `GetApp`, the table-listing and policy-listing store calls already used elsewhere.
**Requirement**: MCP-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `GetAppSchemaForUser` on an app with 2 tables (one `rls: "policy"` with 1 policy, one `rls: ""` with none) returns both tables with correct `rls_mode`, `columns`, and `policies` (empty array, not null, for the second table)
- [x] `GetAppSchemaForUser` on an app with zero tables returns `tables: []`, not an error
- [x] `GetAppSchemaForUser` for an app the user has no access to returns the same authorization error `GetApp` already returns for that case
- [x] `ListAppsForUser` output matches the existing `ListApps` handler's output for the same user (extraction, no behavior change)
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add GetAppSchemaForUser aggregation and ListAppsForUser`

---

### T9: MCP server bootstrap + `/dashboard/mcp` route (no tools registered yet)

**What**: Add `github.com/modelcontextprotocol/go-sdk` dependency. `internal/mcpserver/server.go`: `NewHandler(pool *db.Pool, rl *dashboard.RateLimiter) http.Handler` constructing an `mcp.Server`, wrapping its `StreamableHTTPHandler` with `RequirePAT(pool)` then `rl.MiddlewareKeyedBy(patIDFromContext)`. Mount at `/dashboard/mcp` in `internal/server/server.go`, sibling to the existing `/dashboard` route group. `patIDFromContext` extracts the resolved PAT's id (added to context by `RequirePAT`, alongside the `DashboardUser`) for rate-limiter keying.
**Where**: `internal/mcpserver/server.go` (new), `internal/server/server.go` (mount route), `go.mod`/`go.sum` (new dependency)
**Depends on**: T1, T2
**Reuses**: `RateLimiter.MiddlewareKeyedBy` (`dashboard/middleware.go:136`), `RequirePAT` (T2).
**Requirement**: Design's MCP HTTP Handler component (supports all MCP-0x)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `/dashboard/mcp` responds to an MCP client's initialize handshake with a valid PAT, using the SDK's own streamable-HTTP client in the test (not a hand-rolled HTTP fixture)
- [x] The same handshake without a valid PAT is rejected before reaching the MCP protocol layer (401, per T2)
- [x] A PAT that exceeds its rate-limit window is rejected on the Nth call within the window, keyed by PAT id (two different PATs each get their own budget)
- [x] Zero tools are registered yet — this task only proves transport+auth
- [x] Gate check passes: `go build ./... && go test ./internal/mcpserver/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(mcpserver): add MCP server bootstrap and /dashboard/mcp route`

---

### T10: Register read-only tools — `orbit_list_apps`, `orbit_get_app_schema`

**What**: `RegisterTools` (new `internal/mcpserver/tools.go`) registers these two tools first (lowest risk, read-only, proves the registration pattern end-to-end before any write tool). Input schemas derived from the same param shapes `ListAppsForUser`/`GetAppSchemaForUser` (T8) accept.
**Where**: `internal/mcpserver/tools.go` (new)
**Depends on**: T9, T8
**Reuses**: `ListAppsForUser`, `GetAppSchemaForUser` (T8).
**Requirement**: MCP-09 (tool-level), design's MCP Tool Registry component

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Calling `orbit_list_apps` via a real MCP client returns the same apps `ListAppsForUser` would for that PAT's owning user
- [x] Calling `orbit_get_app_schema` for a known app id returns the same shape T8's tests already verified
- [x] Calling `orbit_get_app_schema` for an app the caller can't access returns a structured tool error, not a raw Go error string
- [x] Gate check passes: `go test ./internal/mcpserver/...`

**Status**: Complete

**Tests**: integration, real MCP client roundtrip
**Gate**: quick

**Commit**: `feat(mcpserver): register orbit_list_apps and orbit_get_app_schema tools`

---

### T11: Register write tools — `orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`

**What**: Register the three core write tools, each a thin wrapper calling `CreateAppForUser` (T4), `CreateAppTableForUser` (T5), `UpdateTableRLSModeForUser` (T6) respectively. Input schemas derived from `CreateAppInput`/`CreateTableInput`/the RLS-mode param shape.
**Where**: `internal/mcpserver/tools.go` (modify)
**Depends on**: T9, T4, T5, T6
**Reuses**: T4/T5/T6's extracted functions.
**Requirement**: MCP-06, MCP-07, MCP-08, MCP-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `orbit_create_app` via a real MCP client creates an app identical to what the REST endpoint would for the same input, and produces the same `audit_log` entry
- [x] `orbit_create_table` via a real MCP client on that app creates a table with columns exactly as specified
- [x] `orbit_set_table_rls_mode` via a real MCP client sets the table's RLS mode; invalid RLS value returns a structured tool error, table unchanged
- [x] A malformed `orbit_create_table` call (e.g. duplicate column name) returns a structured tool error naming the problem, no partial table created
- [x] Gate check passes: `go test ./internal/mcpserver/...`

**Status**: Complete

**Tests**: integration, real MCP client roundtrip
**Gate**: quick

**Commit**: `feat(mcpserver): register orbit_create_app, orbit_create_table, orbit_set_table_rls_mode tools`

---

### T12: Register template tools — `orbit_list_policy_templates`, `orbit_create_policy_from_template`

**What**: `orbit_list_policy_templates` wraps `policytemplates.List()` (T3). `orbit_create_policy_from_template` resolves a template id + inputs (roles, column/value for `value_match`) via `policytemplates`'s builders (T3), then creates the resulting `PolicyDef`(s) sequentially via `CreateTablePolicyForUser` (T7) — stop-on-first-error, report which succeeded, matching the frontend template picker's existing partial-failure contract (`policy-templates` spec P2 AC2/AC3).
**Where**: `internal/mcpserver/tools.go` (modify)
**Depends on**: T9, T3, T7
**Reuses**: `policytemplates` builders (T3), `CreateTablePolicyForUser` (T7).
**Requirement**: MCP-12, MCP-13, MCP-14

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `orbit_list_policy_templates` returns the same 6 templates `policytemplates.List()` does, with enough structure (id, description, required inputs) for an LLM to pick one without free-form clause syntax
- [x] `orbit_create_policy_from_template` for a single-action template (e.g. `owner_only`) creates the expected policy via `CreateTablePolicyForUser`
- [x] `orbit_create_policy_from_template` for the composite template (`open_read_owner_write`) creates all 3 policies in sequence; forcing the 2nd to fail (pre-existing colliding policy name) stops before the 3rd and reports created/failed/pending per policy, matching the design's Error Handling Strategy
- [x] Missing/invalid required input (e.g. no roles for `owner_only`) returns a structured tool error naming the missing input, zero policies created
- [x] Gate check passes: `go test ./internal/mcpserver/...`

**Status**: Complete

**Tests**: integration, real MCP client roundtrip
**Gate**: quick

**Commit**: `feat(mcpserver): register orbit_list_policy_templates and orbit_create_policy_from_template tools`

---

### T17: `OAuthClientStore`, dynamic registration, and metadata discovery endpoint

**What**: New catalog table `oauth_clients` (id, name, redirect_uris, created_at). `OAuthClientStore.RegisterClient` issues a random `client_id`, stores the client's declared name and redirect URIs. `POST /dashboard/oauth/register` wraps it (rate-limited per-IP via `RateLimiter.Middleware`, plain per-IP variant — no logical caller identity exists yet at registration time). `GET /.well-known/oauth-authorization-server` serves a static JSON document pointing at `/dashboard/oauth/authorize`, `/dashboard/oauth/token`, and `/dashboard/oauth/register`.
**Where**: `internal/dashboard/oauth_client_store.go` (new), `internal/dashboard/oauth_server.go` (new, `RegisterClient`/`GetMetadata` handlers), `internal/dashboard/provisioner.go` (add `oauth_clients` migration), `internal/server/server.go` (mount both routes, unauthenticated)
**Depends on**: T1
**Reuses**: `generateToken` for `client_id`, structural pattern from `table_policies_store.go`.
**Requirement**: MCP-19, MCP-20

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `GET /.well-known/oauth-authorization-server` returns a valid discovery document with all 3 endpoint URLs
- [x] `POST /dashboard/oauth/register` with a name and redirect URI returns a `client_id`; no prior manual setup required
- [x] Registration endpoint rejects a request once its per-IP rate limit is exceeded
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add OAuth client registration and metadata discovery endpoints`

---

### T18: `oauth_auth_codes` store + `Authorize` handler (login redirect, PKCE-bound code issuance)

**What**: New table `oauth_auth_codes` (id, code_hash, client_id FK, user_id FK, code_challenge, redirect_uri, used_at, expires_at, created_at), short retention (purge codes older than e.g. 1 hour via the existing ticker pattern). `GET /dashboard/oauth/authorize` handler: validates `client_id`/`redirect_uri` against `OAuthClientStore.GetClient` (rejects unknown client or mismatched redirect URI with `400`, no redirect performed — avoids an open-redirect); if no `zeep_session` cookie, redirects to the existing login page with a return-to URL preserving the OAuth params; once authenticated, hands off to T19's consent screen instead of issuing a code directly.
**Where**: `internal/dashboard/oauth_server.go` (modify, add `Authorize`), `internal/dashboard/oauth_auth_codes_store.go` (new), `internal/dashboard/provisioner.go` (add migration), `cmd/zeep/main.go` (extend existing purge ticker)
**Depends on**: T17
**Reuses**: Existing login page/`zeep_session` mechanism, purge-ticker pattern from `webhook_deliveries`.
**Requirement**: MCP-21 (partial — login+redirect half), MCP-22 (partial — code storage)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `/authorize` with an unknown `client_id` or a `redirect_uri` not matching the registered client returns `400`, no redirect
- [x] `/authorize` with no active session redirects to login, preserving OAuth params for after login completes
- [x] `/authorize` with an active session reaches the consent step (hands off to T19; this task's own test asserts the handoff happens, not the consent UI itself)
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add oauth_auth_codes store and Authorize handler login/validation flow`

---

### T19: Consent screen + authorization code issuance

**What**: `OAuthConsent.tsx` — renders after login, naming the requesting client (from T17's stored `name`) and showing the redirect URI's origin (mitigates the phishing-adjacent risk flagged in design's Risks & Concerns — an admin sees where they're actually being sent, not just a self-declared name). On grant, `Authorize` generates a single-use code bound to the PKCE `code_challenge` (stored via T18's store), redirects back to the client's `redirect_uri` with the code. On deny, redirects back with `error=access_denied`, no code issued.
**Where**: `internal/dashboard/ui/src/components/OAuthConsent.tsx` (new), `internal/dashboard/oauth_server.go` (modify `Authorize` — grant/deny branches), `en.json`/`pt-BR.json` (new keys)
**Depends on**: T18
**Reuses**: Existing Dashboard UI layout/button patterns.
**Requirement**: MCP-21 (consent screen), MCP-22 (code issuance/PKCE binding)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Consent screen displays the client's declared name and the redirect URI's origin
- [x] Granting issues a single-use code, redirects to `redirect_uri` with `code` param, code row stores the presented `code_challenge`
- [x] Denying redirects to `redirect_uri` with `error=access_denied`, confirmed no code row was created
- [x] All new strings present in `en.json`/`pt-BR.json`
- [x] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Status**: Complete

**Tests**: none — merge-forward to T21 (needs a real browser session to test the redirect chain meaningfully)
**Gate**: full

**Commit**: `feat(dashboard-ui): add OAuth consent screen`

---

### T20: `Token` endpoint — code exchange, refresh rotation, reuse detection

**What**: `POST /dashboard/oauth/token`. `grant_type=authorization_code`: looks up the code by hash, rejects if already used/expired/PKCE-verifier-mismatched (`400 invalid_grant`), else marks it used and calls `PATStore.CreatePAT(kind="oauth", expiresAt=<short TTL>)` plus stores a `refresh_token_hash` on the same row via a new `PATStore.SetRefreshToken` helper. `grant_type=refresh_token`: hashes the presented refresh token, looks it up; if it matches the *current* `refresh_token_hash` for its PAT row, rotates (new access token + new refresh token, same row updated); if it matches an *already-rotated* (superseded) hash, treats as reuse — revokes that PAT row and every row descended from the same original grant (tracked via a shared `oauth_client_id`+`user_id`+original-code lineage, exact bookkeeping decided during implementation).
**Where**: `internal/dashboard/oauth_server.go` (modify, add `Token`), `internal/dashboard/pat_store.go` (modify, add `SetRefreshToken`/refresh-lookup helpers)
**Depends on**: T17
**Reuses**: `PATStore.CreatePAT` (T1), `generateToken`.
**Requirement**: MCP-22, MCP-23, MCP-24

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Valid code + matching PKCE verifier exchanges for an access token that `ResolvePAT` resolves to the consenting admin (same identity path a manual PAT resolves through)
- [x] Reused code, expired code, or mismatched PKCE verifier all return `400 invalid_grant`, no token issued
- [x] Valid refresh token exchange issues a new access+refresh pair and invalidates the old refresh token
- [x] Reusing an already-rotated refresh token is rejected AND revokes the access token issued alongside it (confirm a subsequent `orbit_list_apps` call with that access token now fails)
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add OAuth token endpoint with refresh rotation and reuse detection`

---

### T21: End-to-end OAuth integration test (discovery → registration → consent → tool call → refresh → reuse-revocation)

**What**: A single Go integration test driving the full OAuth story: fetch metadata (T17) → register a client (T17) → hit `/authorize` (T18) with a PKCE challenge → simulate login+consent grant (T19) → exchange the code at `/token` (T20) → call `orbit_list_apps` against `/dashboard/mcp` (Phase 3) with the resulting access token → refresh it → attempt to reuse the old (now-superseded) refresh token and confirm the resulting revocation blocks a further tool call.
**Where**: `internal/dashboard/oauth_integration_test.go` (new)
**Depends on**: T19, T20, T9-T12 (Phase 3, to call a real tool with the issued token)
**Reuses**: Every OAuth component from T17-T20, plus the MCP tool registry from Phase 3.
**Requirement**: MCP-19 through MCP-24 (closes end-to-end coverage for the OAuth story)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Full sequence above passes against a real ephemeral Postgres
- [x] The access token obtained via OAuth drives `orbit_list_apps` identically to a manually-created PAT would for the same admin
- [x] Refresh rotation succeeds once; reusing the superseded refresh token is rejected and the associated access token stops working immediately after
- [x] Gate check passes (Build): `go build ./... && go test ./... && go vet ./... && gofmt -l $(git diff --name-only -- '*.go')`

**Status**: Complete

**Tests**: integration
**Gate**: build

**Commit**: `test(dashboard): add end-to-end OAuth authorization flow integration test`

---

### T13: Dashboard PAT REST handlers (`CreatePAT`, `ListPATs`, `RevokePAT`)

**What**: Session-authenticated (`RequireAuth`, not `RequirePAT` — an admin manages tokens over their browser session, never uses a PAT to manage PATs) handlers under `/dashboard/api/me/pats`. `POST` creates and returns the plaintext once; `GET` lists (no token value); `DELETE /{patId}` revokes, scoped to the caller's own tokens. Every action audited.
**Where**: `internal/dashboard/pat_handler.go` (new), `internal/server/server.go` (mount routes)
**Depends on**: T1
**Reuses**: `PATStore` (T1), `h.audit(...)` pattern.
**Requirement**: MCP-01, MCP-04, MCP-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `POST /dashboard/api/me/pats` with a name creates a PAT, response includes the plaintext token exactly once
- [x] `GET /dashboard/api/me/pats` lists the caller's tokens, no token value in the response
- [x] `DELETE /dashboard/api/me/pats/{id}` for another user's PAT id returns `404`/`403`, not success
- [x] Each action produces an `audit_log` entry (`pat.create`, `pat.revoke`)
- [x] Gate check passes: `go test ./internal/dashboard/...`

**Status**: Complete

**Tests**: integration
**Gate**: quick

**Commit**: `feat(dashboard): add personal access token REST handlers`

---

### T14: `PersonalAccessTokens.tsx` Settings UI

**What**: New Settings sub-section: list of existing PATs (name, created_at, last_used_at, revoke action via `ConfirmDialog`), "Create token" form (name input) whose result dialog shows the plaintext once with a copy button and an explicit "you won't see this again" warning, closing which permanently hides it. React Query hooks `usePATs()`, `useCreatePAT`, `useRevokePAT` in `src/lib/api.ts`, following `Webhooks.tsx`'s token-rotation UI pattern (mutation + `toast.error` on failure).
**Where**: `internal/dashboard/ui/src/components/PersonalAccessTokens.tsx` (new), `internal/dashboard/ui/src/pages/SettingsPage.tsx` (or wherever Settings tabs are registered), `internal/dashboard/ui/src/lib/api.ts` (new hooks), `en.json`/`pt-BR.json` (new keys, same change per `AGENTS.md` §5)
**Depends on**: T13
**Reuses**: `ConfirmDialog`, TanStack Query mutation pattern, i18n key structure.
**Requirement**: MCP-01, MCP-04

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Creating a token shows the plaintext exactly once in a dismissible dialog; after dismissal, no UI surface shows it again (only refetching `usePATs()` would, and that never includes the value)
- [ ] Revoking a token prompts `ConfirmDialog`; confirming removes it from the list without a manual reload
- [ ] Mutation failures surface via `toast.error`
- [ ] All new strings present in `en.json` and `pt-BR.json`
- [ ] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Status**: Not Started

**Tests**: none — merge-forward to T16 (needs to be reachable through a real browser session to test create/list/revoke meaningfully)
**Gate**: full

**Commit**: `feat(dashboard-ui): add Personal Access Tokens settings section`

---

### T15: End-to-end MCP tool-calling integration test (P1+P2 full story)

**What**: A single Go integration test, using the MCP SDK's real client against a running test server + real Postgres, driving the entire P1+P2 story in one sequence: connect with a PAT → `orbit_create_app` → `orbit_create_table` → `orbit_set_table_rls_mode` (to `"policy"`) → `orbit_create_policy_from_template` (`owner_only`) → `orbit_get_app_schema` to verify final state → revoke the PAT → confirm the same client is now rejected. This is the MVP-level proof the spec's Success Criteria describes ("go from no MCP client connected to app created with a table, RLS mode, and a policy using only MCP tool calls").
**Where**: `internal/mcpserver/integration_test.go` (new)
**Depends on**: T10, T11, T12
**Reuses**: Every tool registered in T10-T12; `PATStore` (T1) for the setup/revoke steps.
**Requirement**: All of MCP-01 through MCP-14 (closes end-to-end coverage)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Full sequence above passes against a real ephemeral Postgres, same setup convention as `enduser-roles.spec.ts`/`webhooks.spec.ts` at the Go-test level
- [ ] Final `orbit_get_app_schema` result matches exactly what was created across the sequence (table present, `rls_mode: "policy"`, one policy named `tpl_owner_only_<action>` per action)
- [ ] Post-revoke call with the same client's token fails auth (401-equivalent tool-call rejection)
- [ ] Gate check passes (Build): `go build ./... && go test ./... && go vet ./... && gofmt -l $(git diff --name-only -- '*.go')`

**Status**: Not Started

**Tests**: integration, real MCP client roundtrip
**Gate**: build

**Commit**: `test(mcpserver): add end-to-end MCP tool-calling integration test`

---

### T16: End-to-end Playwright coverage for PAT management UI

**What**: `internal/dashboard/ui/e2e/personal-access-tokens.spec.ts` (Playwright, `bootstrapOrSkip`/`login` helpers, single sequential `test()` with commented stages): create a token (assert plaintext shown once), reload the page (assert value no longer visible, entry still listed), revoke it (assert removed from list), confirm a PAT minted this way actually authenticates against `/dashboard/mcp` (via a raw HTTP request in the test, not a full MCP client — proving the UI-created token round-trips into a real usable credential).
**Where**: `internal/dashboard/ui/e2e/personal-access-tokens.spec.ts` (new)
**Depends on**: T14
**Reuses**: `bootstrapOrSkip`/`login` helpers (`e2e/helpers.ts`), same multi-stage single-`test()` pattern as `webhooks.spec.ts`.
**Requirement**: MCP-01, MCP-04 (closes UI-level coverage)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Full sequence above passes against a running `zeep` binary with `DATABASE_URL`/`DASHBOARD_BOOTSTRAP_SECRET` set
- [ ] The UI-created token is confirmed to authenticate a real (non-MCP-client) HTTP request to `/dashboard/mcp` before revocation, and to fail the same request after revocation
- [ ] Gate check passes (Build): `cd internal/dashboard/ui && npx tsc -b && npm run build && npx playwright test personal-access-tokens`, plus `go build ./... && go vet ./...` from repo root as a backend-regression sanity check

**Status**: Not Started

**Tests**: e2e
**Gate**: build

**Commit**: `test(dashboard-ui): add end-to-end coverage for personal access tokens settings`

---

## Phase Execution Map

Phases run in sequence; tasks within a phase run in order except where noted as independent.

Phase 1 (Foundation): T1, then T2 (depends on T1), then T3 (independent, runs after T1-T2 only because tasks in a phase execute in listed order, not because of a real dependency).

Phase 2 (Shared Operation Extraction): T4, T5, T6, T7 (all independent of each other, each depends only on Phase 1 completing), then T8 (depends on T4-T7).

Phase 3 (MCP Server Wiring): T9 (depends on T1, T2), then T10 (depends on T9, T8), then T11 (depends on T9, T4-T6), then T12 (depends on T9, T3, T7).

Phase 4 (OAuth Authorization Server): T17 (depends on T1), then T18 (depends on T17), then T19 (depends on T18), then T20 (depends on T17 — independent of T18/T19, runs after them only because tasks in a phase execute in listed order), then T21 (depends on T19, T20, and Phase 3's T9-T12).

Phase 5 (PAT Management UI): T13 (depends on T1), then T14 (depends on T13).

Phase 6 (Integration & Verification): T15 (depends on T10-T12), then T16 (depends on T14).

**How phase-based execution works:** at Execute, the agent counts total tasks (21 here) and packs phases into task-budgeted batches (~7-8 tasks per worker, whole phases only — the cut lands on a phase boundary, never mid-phase). 21 tasks exceeds the single-batch threshold, so batch sub-agents will be offered before Execute starts: Batch 1 = Phase 1 + Phase 2 (8 tasks: T1-T8), Batch 2 = Phase 3 (4 tasks: T9-T12), Batch 3 = Phase 4 + Phase 5 (7 tasks: T17-T21, T13-T14), Batch 4 = Phase 6 (2 tasks: T15-T16). Batches run sequentially; a worker executes all its tasks in order, then reports before the next batch starts. Note Phase 4 (OAuth) has a real dependency on Phase 3 only at its final task (T21) — Batches 2 and 3 could in principle run concurrently up through T17-T20, but batching stays phase-sequential per the standard rule (whole phases only), so this is left as a known opportunity, not exploited automatically.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: `PATStore` + catalog table | 1 store + 1 migration, cohesive auth-artifact primitive | Granular |
| T2: `RequirePAT` middleware | 1 middleware | Granular |
| T3: `policytemplates` Go port | 1 new package, mirrors 1 existing TS file | Granular |
| T4: Extract `CreateAppForUser` | 1 function extraction from 1 handler | Granular |
| T5: Extract `CreateAppTableForUser` | 1 function extraction from 1 handler | Granular |
| T6: Extract `UpdateTableRLSModeForUser` | 1 function extraction from a slice of 1 handler | Granular |
| T7: Extract `CreateTablePolicyForUser` | 1 function extraction from 1 handler | Granular |
| T8: New aggregation functions | 2 cohesive read functions, bundled because `GetAppSchemaForUser` needs `ListAppsForUser`'s sibling context and both are trivial reads | Granular (justified bundling) |
| T9: MCP server bootstrap | 1 new server + 1 route mount, zero tools (deliberately scoped to just transport+auth) | Granular |
| T10: Read-only tools | 2 tools, both trivial read wrappers, bundled as "prove the pattern with the lowest-risk tools first" | Granular (justified bundling) |
| T11: Write tools | 3 tools, all thin wrappers over Phase 2's extractions, bundled as "the create-app-then-table-then-rls sequence is one coherent story" | Granular (justified bundling) |
| T12: Template tools | 2 tools, tightly coupled (list feeds create's input choices) | Granular (justified bundling) |
| T13: PAT REST handlers | 3 handlers, 1 resource | Granular |
| T14: PAT Settings UI | 1 component + wiring | Granular |
| T15: MCP integration test | 1 test file, 1 end-to-end story | Granular |
| T16: PAT e2e test | 1 test file, 1 end-to-end story | Granular |
| T17: `OAuthClientStore` + registration + metadata | 1 store + 2 tightly-coupled unauthenticated endpoints (metadata just describes what registration creates) | Granular (justified bundling) |
| T18: `oauth_auth_codes` store + `Authorize` validation/redirect | 1 store + 1 handler's login/validation half, deliberately stopping before consent (T19) | Granular |
| T19: Consent screen + code issuance | 1 component + the other half of `Authorize` (grant/deny branches) — split from T18 because this half is genuinely new UI, not just backend validation | Granular |
| T20: `Token` endpoint | 1 handler, 2 grant types (code exchange, refresh) — bundled because both branches share the same PAT-row mutation logic and reuse-detection is only meaningful in the context of both | Granular (justified bundling) |
| T21: OAuth end-to-end integration test | 1 test file, 1 end-to-end story | Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | Match |
| T2 | T1 | PATAuth → PATStore | Match |
| T3 | None | OpsTpl → policytemplates package | Match |
| T4 | Phase 1 | RestH → OpsApp | Match |
| T5 | Phase 1 | RestH → OpsTable | Match |
| T6 | Phase 1 | RestH → OpsRLS | Match |
| T7 | Phase 1 | RestH → OpsTpl | Match |
| T8 | T4-T7 | OpsRead (new, not in RestH chain) | Match |
| T9 | T1, T2 | McpH → PATAuth, RL | Match |
| T10 | T9, T8 | Tools → OpsRead | Match |
| T11 | T9, T4-T6 | Tools → OpsApp, OpsTable, OpsRLS | Match |
| T12 | T9, T3, T7 | Tools → OpsTpl | Match |
| T13 | T1 | PatH → PATStore | Match |
| T14 | T13 | Dash → PatH | Match |
| T15 | T10, T11, T12 | Full tool chain | Match |
| T16 | T14 | Dash → PatH chain | Match |
| T17 | T1 | ClaudeDesktop → OAuthMeta, OAuthReg → PATStore (via OAuthClientStore) | Match |
| T18 | T17 | ClaudeDesktop → OAuthAuthz → Login | Match |
| T19 | T18 | Login → Consent → OAuthAuthz | Match |
| T20 | T17 | ClaudeDesktop → OAuthToken → PATStore | Match |
| T21 | T19, T20, T9-T12 | Full OAuth chain → McpH | Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `PATStore` | Backend store | integration | integration | OK |
| T2: `RequirePAT` | Backend middleware | integration | integration | OK |
| T3: `policytemplates` | Backend pure logic | unit | unit | OK |
| T4-T7: `*ForUser` extractions | Backend (refactor) | integration (existing unmodified + new direct) | integration | OK |
| T8: aggregation functions | Backend (new) | integration | integration | OK |
| T9: MCP bootstrap | Backend (new transport) | integration | integration | OK |
| T10-T12: tool registrations | Backend (new) | integration, real MCP client roundtrip | integration, real MCP client roundtrip | OK |
| T13: PAT REST handlers | Backend | integration | integration | OK |
| T14: PAT Settings UI | Frontend UI | e2e | none (merge-forward → T16) | OK (compilation-dependency, justified — not meaningfully testable until reachable through a real browser session) |
| T15: MCP integration test | Test file itself | integration | integration | OK |
| T16: PAT e2e test | Test file itself | e2e | e2e | OK |
| T17: OAuth client store + registration/metadata | Backend | integration | integration | OK |
| T18: `oauth_auth_codes` + `Authorize` validation | Backend | integration | integration | OK |
| T19: Consent screen | Frontend UI | e2e | none (merge-forward → T21) | OK (compilation-dependency, justified — the redirect chain this UI participates in only makes sense end-to-end) |
| T20: `Token` endpoint | Backend | integration | integration | OK |
| T21: OAuth integration test | Test file itself | integration | integration | OK |

**Merge-forward justification (T14):** the Settings UI can't be driven through a real browser until it's mounted in the Settings page and wired to real backend hooks — same reasoning `policy-templates/tasks.md` used for its own frontend tasks. T16 is the earliest point this code is reachable at all.

**Merge-forward justification (T19):** the consent screen is one leg of a multi-hop redirect chain (client → authorize → login → consent → client's redirect_uri) — no meaningful assertion exists until T21 drives the full chain with a real client-equivalent test.

---

## Task Verification Standards

Every task above follows `Done when` + `Tests` + `Gate`, each entry specific and binary pass/fail, referencing the exact commands from **Gate Check Commands**.
