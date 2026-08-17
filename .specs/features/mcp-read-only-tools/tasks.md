# MCP Read-Only Tools Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/mcp-read-only-tools/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate commands: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`), §4 (error-string rule — directly relevant, every new function must map internal errors to a fixed message). Testing convention sampled from `internal/dashboard/table_policies_handler_test.go`, `internal/dashboard/apps_store_test.go`, `internal/mcpserver/tools_test.go`, and `mcp-server/tasks.md`'s own matrix: integration tests against a real ephemeral Postgres (no mocking layer exists for `db.Pool` anywhere in this codebase), `-race` enabled, real MCP-client round-trip for tool-registry tests (not hand-rolled JSON fixtures).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| New `*ForUser` functions (`ListTablePoliciesForUser`, `ListAppMembersForUser`, `ListAppTokensForUser`, `ListWebhooksForUser`, `GetWebhookForUser`, `ListWebhookDeliveriesForUser`, `LogsMetricsForUser`) | integration (real Postgres) | Happy path matching the REST handler's existing happy-path test; every distinct error branch from the Error Handling Strategy table (not-found, each function's specific authorization tier, business-rule errors like `ErrAppTokensNotSupported`); empty-result returns `[]`/equivalent, never `nil` | `internal/dashboard/*_test.go` (co-located with the function, same file as its existing store/handler tests) | `go test ./internal/dashboard/...` |
| Refactored REST handlers (`ListTablePolicies`, `ListAppMembers`, `ListAppTokens`, `ListWebhooks`, `GetWebhook`, `ListWebhookDeliveries`, `LogsMetrics`) | integration | Existing tests for these handlers MUST keep passing unmodified (behavior-preserving-extraction bar — same standard `mcp-server/tasks.md` set for its own handler extractions) | Existing `*_test.go` files, unchanged expectations | `go test ./internal/dashboard/...` |
| MCP tool registry additions (`internal/mcpserver/tools.go`) | integration, real MCP client roundtrip | Each of the 10 new tools callable via the SDK's own test client against a running server backed by real Postgres (same harness `orbit_list_apps`/`orbit_get_app_schema` already use); authorization-failure path tested at the **specific tier that tool actually enforces** (not a shared "same as GetApp" assertion — three tiers exist per design.md's Authorization Matrix: `CanManage()` for policies/members/webhooks, plain visibility for app/auth-providers, ownership-only for PATs/logs-metrics); secret-boundary test for every tool whose underlying type can carry a credential field | `internal/mcpserver/tools_test.go` | `go test ./internal/mcpserver/...` |
| Sentinel errors (`ErrAppTokensNotSupported`) | covered by the function's own integration test above | No standalone test — asserted as part of `ListAppTokensForUser`'s error-branch coverage | n/a | n/a |

**Coverage Expectation rationale**: this repo has zero mocking infrastructure for `db.Pool` — every existing backend test already runs against a real ephemeral Postgres. New tests follow the same convention.

## Gate Check Commands

> Generated from codebase - confirm before Execute.

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick | After a task that only touches `internal/dashboard` | `go test ./internal/dashboard/... -race` |
| Full | After a task that also touches `internal/mcpserver` | `go test ./internal/dashboard/... ./internal/mcpserver/... -race` |
| Build | After the final task of each phase, and before the phase's last commit | `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && go test ./... -race` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Simple wrapper tools (no new dashboard function needed)

```
T1 → T2 → T3
```

### Phase 2: Table policies read path

```
T4 → T5
```

### Phase 3: App members read path

```
T6 → T7
```

### Phase 4: App tokens read path

```
T8 → T9
```

### Phase 5: Webhook read paths

```
T10 → T11 → T12 → T13
T10 → T13
T11 → T13
```

### Phase 6: Logs metrics read path

```
T14 → T15
```

---

## Task Breakdown

### T1: `orbit_get_app` MCP tool

**What**: Register `orbit_get_app` in `internal/mcpserver/tools.go`, calling `dashboard.GetApp(ctx, pool, appID, user)` directly and applying `RedactSecrets()` to the result before returning — no new `dashboard` package function needed (spec's own assumption: `GetApp` is already the shared operation function).
**Where**: `internal/mcpserver/tools.go` (new `registerAppConfigReadTools` function, first tool in it)
**Depends on**: None
**Reuses**: `dashboard.GetApp`, `AppRow.RedactSecrets()`, the `orbit_list_apps`/`orbit_get_app_schema` registration pattern (`tools.go:81-112`), `errInternal`/`errUnauthorized`
**Requirement**: MROT-01, MROT-03, MROT-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `orbit_get_app` returns a redacted `AppRow` for a caller with access
- [x] Returns `"not found"` tool error for an invisible/nonexistent app (matching `GetApp`'s `ErrNotFound`)
- [x] Response contains no `client_secret`/`secret_access_key`/`jwt_secret` under any field name (boundary test, same style as `TestOrbitListAppsTool_ResponseNeverContainsSecrets`)
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_get_app read-only tool`

---

### T2: `orbit_list_app_auth_providers` MCP tool

**What**: Register `orbit_list_app_auth_providers`, wrapping `dashboard.GetAppAuthProviders(ctx, pool, appID, user)` directly — already does `GetApp` + `redactAuthProviderSecrets` internally, no new function needed.
**Where**: `internal/mcpserver/tools.go` (`registerAppConfigReadTools`, or a new `registerAccessReadTools` per design — place per design's grouping)
**Depends on**: T1
**Reuses**: `dashboard.GetAppAuthProviders`, `redactAuthProviderSecrets` (already applied internally)
**Requirement**: MROT-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Returns the same redacted shape the REST endpoint (`GET /apps/{id}/auth/providers`) returns for a known app
- [x] Response never contains `client_secret` under any provider key (boundary test)
- [x] Returns not-found for an invisible/nonexistent app
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_list_app_auth_providers read-only tool`

---

### T3: `orbit_list_my_pats` MCP tool

**What**: Register `orbit_list_my_pats` (no input), wrapping `dashboard.ListPATs(ctx, pool, user.ID)` directly.
**Where**: `internal/mcpserver/tools.go` (`registerAccessReadTools`)
**Depends on**: T2
**Reuses**: `dashboard.ListPATs` (already ownership-scoped)
**Requirement**: MROT-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Returns only PATs owned by the calling identity (test with two distinct users' PATs seeded, confirm no cross-user leakage)
- [x] Response never contains a raw token/JTI value, only metadata (id, name, kind, expiry, revoked/last-used timestamps)
- [x] Empty PAT list returns `[]`, not `null`
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_list_my_pats read-only tool`

---

### T4: `ListTablePoliciesForUser` dashboard function + REST handler refactor

**What**: Add `ListTablePoliciesForUser(ctx, pool, user, appID, tableName)` to `internal/dashboard/table_policies_store.go`, composing `GetApp` (require `role.CanManage()`, matching `handler.go:1518`'s existing check exactly) + `findAppTableByName` (→ `ErrTableNotFound` if absent) + the existing `ListTablePolicies` query. Refactor the `ListTablePolicies` REST handler (`handler.go:1500`) to call this new function instead of inlining the same three steps.
**Where**: `internal/dashboard/table_policies_store.go` (new function), `internal/dashboard/handler.go:1500` (refactor)
**Depends on**: None
**Reuses**: `GetApp`, `findAppTableByName`, `ListTablePolicies`, `ErrNotFound`/`ErrForbidden`/`ErrTableNotFound` sentinels
**Requirement**: MROT-02, MROT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ListTablePoliciesForUser` returns the same policies `ListTablePolicies`'s existing REST test expects for the same fixture
- [x] Returns `ErrForbidden` when caller's role fails `CanManage()` (viewer/editor role tested explicitly, not just "some non-admin")
- [x] Returns `ErrTableNotFound` for a valid app + nonexistent table name (never `[]`)
- [x] Existing `ListTablePolicies` REST handler test(s) still pass unmodified
- [x] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract ListTablePoliciesForUser shared operation function`

---

### T5: `orbit_list_table_policies` MCP tool

**What**: Register `orbit_list_table_policies`, wrapping `ListTablePoliciesForUser` from T4.
**Where**: `internal/mcpserver/tools.go` (`registerAppConfigReadTools`)
**Depends on**: T4
**Reuses**: `ListTablePoliciesForUser`
**Requirement**: MROT-02, MROT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Returns policies for a table the caller can manage
- [x] Returns forbidden tool error for a caller whose role fails `CanManage()` on that app (explicit tier test, not reused from `orbit_get_app`'s tier)
- [x] Returns not-found tool error for a nonexistent table name on a visible app
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_list_table_policies read-only tool`

---

### T6: `ListAppMembersForUser` dashboard function + REST handler refactor

**What**: Add `ListAppMembersForUser(ctx, pool, user, appID)` to `internal/dashboard/app_members_store.go`, composing `AppRef{BackendAppID: appID}` + `ResolveAppRole` (require `role.CanManage()`, matching `app_members.go:93`) + `ListAppMembers`. Refactor the `ListAppMembers` REST handler (`app_members.go:68`) to call it.
**Where**: `internal/dashboard/app_members_store.go` (new function), `internal/dashboard/app_members.go:68` (refactor)
**Depends on**: None
**Reuses**: `AppRef`, `ResolveAppRole`, `ListAppMembers`
**Requirement**: MROT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ListAppMembersForUser` returns the same member list `ListAppMembers`'s existing REST test expects
- [x] Returns forbidden for a caller whose role fails `CanManage()` (tested with an editor/viewer member, not just "no membership")
- [x] Returns not-found for an invisible/nonexistent app — **SPEC_DEVIATION**: see commit body; this endpoint's REST handler deliberately returns 403 (not 404) for a nonexistent/invisible app (`app_members.go:64-67`'s own stated reasoning — "403 doesn't leak existence"), unlike GetApp-based tools. `ListAppMembersForUser` mirrors that exactly; `ErrNotFound` is reachable only via a malformed `AppRef` (empty `appID`), tested directly.
- [x] Existing `ListAppMembers` REST handler test(s) still pass unmodified
- [x] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract ListAppMembersForUser shared operation function`

---

### T7: `orbit_list_app_members` MCP tool

**What**: Register `orbit_list_app_members`, wrapping `ListAppMembersForUser` from T6.
**Where**: `internal/mcpserver/tools.go` (`registerAccessReadTools`)
**Depends on**: T6
**Reuses**: `ListAppMembersForUser`
**Requirement**: MROT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Returns member rows (`user_id`, `role`, `created_at`) for a caller who can manage the app
- [x] Returns forbidden for a caller whose role fails `CanManage()` on that app
- [x] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_list_app_members read-only tool`

---

### T8: `ListAppTokensForUser` dashboard function + REST handler refactor

**What**: Add `ErrAppTokensNotSupported` sentinel and `ListAppTokensForUser(ctx, pool, user, appID)` to `internal/dashboard/app_tokens_store.go`, composing `GetApp` (matching `handler.go:2911-2914`'s not-found-only check — no explicit role tier beyond visibility, preserved as-is even though this looks unusual) + the `AuthEmailEnabled` business-rule check (→ `ErrAppTokensNotSupported`, not a generic validation error) + `ListAppTokens`. Refactor the `ListAppTokens` REST handler (`handler.go:2903`) to call it.
**Where**: `internal/dashboard/app_tokens_store.go` (new sentinel + function), `internal/dashboard/handler.go:2903` (refactor)
**Depends on**: None
**Reuses**: `GetApp`, `ListAppTokens`
**Requirement**: MROT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ListAppTokensForUser` returns the same token metadata rows `ListAppTokens`'s existing REST test expects, with no raw token/JTI value
- [x] Returns `ErrAppTokensNotSupported` (not `[]`, not a generic error) for an app with `AuthEmailEnabled == true` — dedicated test, this is the risk flagged in design.md's Risks & Concerns
- [x] Returns not-found for an invisible/nonexistent app
- [x] Existing `ListAppTokens` REST handler test(s) still pass unmodified
- [x] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract ListAppTokensForUser shared operation function`

---

### T9: `orbit_list_app_tokens` MCP tool

**What**: Register `orbit_list_app_tokens`, wrapping `ListAppTokensForUser` from T8.
**Where**: `internal/mcpserver/tools.go` (`registerAccessReadTools`)
**Depends on**: T8
**Reuses**: `ListAppTokensForUser`
**Requirement**: MROT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Returns token metadata for a visible app, no raw token value in the response (boundary test)
- [ ] Returns a distinct tool error (not a generic 500-shaped one) for an app with email auth enabled, surfacing `ErrAppTokensNotSupported`'s message
- [ ] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add orbit_list_app_tokens read-only tool`

---

### T10: `ListWebhooksForUser` dashboard function + REST handler refactor

**What**: Add `ListWebhooksForUser(ctx, pool, user, appID)` to `internal/dashboard/webhooks_store.go`, composing the same auth check `webhookRBACGate` performs (`GetApp` + `role.CanManage()`, matching `webhooks_handler.go:74-93`) + `ListWebhooks`. Refactor the `ListWebhooks` REST handler (`webhooks_handler.go:212`) to call it (handler keeps calling `webhookRBACGate` for its own HTTP-specific error writing, or is refactored to call the new function directly — implementer's call, as long as REST behavior is unchanged).
**Where**: `internal/dashboard/webhooks_store.go` (new function), `internal/dashboard/webhooks_handler.go:212` (refactor)
**Depends on**: None
**Reuses**: `GetApp`, `role.CanManage()`, `ListWebhooks`
**Requirement**: MROT-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `ListWebhooksForUser` returns the same webhook list `ListWebhooks`'s existing REST test expects
- [ ] Returns forbidden for a caller whose role fails `CanManage()` on that app
- [ ] Existing `ListWebhooks` REST handler test(s) still pass unmodified
- [ ] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract ListWebhooksForUser shared operation function`

---

### T11: `GetWebhookForUser` dashboard function + REST handler refactor

**What**: Add `GetWebhookForUser(ctx, pool, user, appID, webhookID)` to `internal/dashboard/webhooks_store.go`, composing the `webhookRBACGate`-equivalent auth check + the same webhook-belongs-to-app scoping `getScopedWebhook` (`webhooks_handler.go:237`) enforces + `GetWebhook` + `ListEventMappings`, returning both in one result (per spec AC2 / design's combined-tool decision). Refactor the `GetWebhook` REST handler (`webhooks_handler.go:231`) to call it (event mappings can stay REST-handler-local if the REST response shape doesn't need them — only the MCP tool's response needs the combination; confirm against the existing REST response shape before deciding whether to change it).
**Where**: `internal/dashboard/webhooks_store.go` (new function), `internal/dashboard/webhooks_handler.go:231` (refactor, scope-preserving only)
**Depends on**: T10
**Reuses**: `GetApp`, `role.CanManage()`, `getScopedWebhook`, `GetWebhook`, `ListEventMappings`
**Requirement**: MROT-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GetWebhookForUser` returns the webhook config plus its event mappings for a `webhook_id` that belongs to the given `app_id`
- [ ] Returns not-found for a `webhook_id` that belongs to a **different** app than the given `app_id` (explicit cross-app scoping test — this is the edge case the spec calls out)
- [ ] Returns forbidden for a caller whose role fails `CanManage()`
- [ ] Existing `GetWebhook` REST handler test(s) still pass unmodified
- [ ] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract GetWebhookForUser shared operation function`

---

### T12: `ListWebhookDeliveriesForUser` dashboard function + REST handler refactor

**What**: Add `ListWebhookDeliveriesForUser(ctx, pool, user, appID, webhookID, limit, offset)` to `internal/dashboard/webhooks_store.go`, composing the same auth+scoping as T11 + `ListDeliveries` (`webhook_deliveries_store.go:90`, already bounds `limit`/`offset`). Refactor the `ListWebhookDeliveries` REST handler (`webhooks_handler.go:536`) to call it.
**Where**: `internal/dashboard/webhooks_store.go` (new function), `internal/dashboard/webhooks_handler.go:536` (refactor)
**Depends on**: T11
**Reuses**: `GetApp`, `role.CanManage()`, `getScopedWebhook`, `ListDeliveries`
**Requirement**: MROT-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `ListWebhookDeliveriesForUser` returns delivery history respecting the same `limit`/`offset` bounds `ListDeliveries` already enforces (test with an out-of-bounds request confirming it's clamped/rejected the same way the REST handler already does)
- [ ] Returns not-found for a `webhook_id` belonging to a different app
- [ ] Existing `ListWebhookDeliveries` REST handler test(s) still pass unmodified
- [ ] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres)
**Gate**: quick

**Commit**: `refactor(dashboard): extract ListWebhookDeliveriesForUser shared operation function`

---

### T13: `orbit_list_webhooks`, `orbit_get_webhook`, `orbit_list_webhook_deliveries` MCP tools

**What**: Register all three webhook-read tools in one cohesive task (same file, same `registerOperationalReadTools` group, same underlying auth tier — matches the "2-3 related things in same file = OK if cohesive" granularity allowance), wrapping T10/T11/T12's functions respectively.
**Where**: `internal/mcpserver/tools.go` (new `registerOperationalReadTools` function)
**Depends on**: T10, T11, T12
**Reuses**: `ListWebhooksForUser`, `GetWebhookForUser`, `ListWebhookDeliveriesForUser`
**Requirement**: MROT-08, MROT-09, MROT-10, MROT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `orbit_list_webhooks` returns webhooks for a caller who can manage the app
- [ ] `orbit_get_webhook` returns config + event mappings for a valid webhook, and not-found for a webhook belonging to a different `app_id`
- [ ] `orbit_list_webhook_deliveries` returns delivery history within the enforced `limit`/`offset` bounds
- [ ] All three return forbidden for a caller whose role fails `CanManage()` on that app
- [ ] Gate passes: `go test ./internal/mcpserver/... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: full

**Commit**: `feat(mcp): add webhook read-only tools (list, get, deliveries)`

---

### T14: `LogsMetricsForUser` dashboard function + REST handler refactor

**What**: Add `LogsMetricsForUser(ctx, pool, logs *RingBuffer, user)` (co-located in `internal/dashboard/handler.go` near `LogsMetrics`, or moved into `logs.go` — implementer's call), composing `ListOwnedAppNames` + `RingBuffer.Metrics`. Refactor the `LogsMetrics` REST handler (`handler.go:2026`) to call it.
**Where**: `internal/dashboard/handler.go:2026` (refactor + new function, or extracted into `internal/dashboard/logs.go`)
**Depends on**: None
**Reuses**: `ListOwnedAppNames`, `RingBuffer.Metrics`
**Requirement**: MROT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `LogsMetricsForUser` returns the same `LogMetrics` shape `LogsMetrics`'s existing REST test expects, for both a superadmin (unrestricted `allowedApps`) and a regular member (restricted set) — both branches of `ListOwnedAppNames` tested explicitly
- [ ] Existing `LogsMetrics` REST handler test(s) still pass unmodified
- [ ] Gate passes: `go test ./internal/dashboard/... -race`

**Tests**: integration (real Postgres, seeded `RingBuffer` entries)
**Gate**: quick

**Commit**: `refactor(dashboard): extract LogsMetricsForUser shared operation function`

---

### T15: `orbit_get_logs_metrics` MCP tool

**What**: Register `orbit_get_logs_metrics` (no input), wrapping `LogsMetricsForUser` from T14 via `deps.DashH.Logs` (already-exported field — no `ToolDeps` change). Tool description text explicitly notes the metrics reflect "the last minute, on whichever server instance handled this request" (design.md's per-replica caveat).
**Where**: `internal/mcpserver/tools.go` (`registerOperationalReadTools`)
**Depends on**: T14
**Reuses**: `LogsMetricsForUser`, `deps.DashH.Logs`
**Requirement**: MROT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Returns `RequestsPerApp` restricted to the caller's own apps for a regular member, unrestricted for a superadmin (both seeded and asserted explicitly)
- [ ] Tool description text includes the per-replica caveat
- [ ] Gate passes: `go test ./internal/mcpserver/... -race`
- [ ] Full feature build gate passes: `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && go test ./... -race`

**Tests**: integration (real MCP client roundtrip)
**Gate**: build

**Commit**: `feat(mcp): add orbit_get_logs_metrics read-only tool`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

Phase 1:  T1 ------→ T2 ------→ T3
Phase 2:  T4 ------→ T5
Phase 3:  T6 ------→ T7
Phase 4:  T8 ------→ T9
Phase 5:  T10 -----→ T11 -----→ T12 -----→ T13
Phase 5:  T10 -----→ T13
Phase 5:  T11 -----→ T13
Phase 6:  T14 -----→ T15
```

Execution is strictly sequential within a phase — no intra-phase parallelism. Phases 1-3 pack into one ~7-task batch; Phases 4-6 pack into a second ~8-task batch (see Sub-Agent Offer below).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: `orbit_get_app` tool | 1 tool registration | ✅ Granular |
| T2: `orbit_list_app_auth_providers` tool | 1 tool registration | ✅ Granular |
| T3: `orbit_list_my_pats` tool | 1 tool registration | ✅ Granular |
| T4: `ListTablePoliciesForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T5: `orbit_list_table_policies` tool | 1 tool registration | ✅ Granular |
| T6: `ListAppMembersForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T7: `orbit_list_app_members` tool | 1 tool registration | ✅ Granular |
| T8: `ListAppTokensForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T9: `orbit_list_app_tokens` tool | 1 tool registration | ✅ Granular |
| T10: `ListWebhooksForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T11: `GetWebhookForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T12: `ListWebhookDeliveriesForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T13: 3 webhook MCP tools | 3 tool registrations, same file, same auth tier, same design group | ⚠️ OK — cohesive (explicitly allowed: "2-3 related things in same file") |
| T14: `LogsMetricsForUser` + REST refactor | 1 function + its 1 call site | ✅ Granular |
| T15: `orbit_get_logs_metrics` tool | 1 tool registration | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ----------------------- | -------------- | ------ |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | None | (start of Phase 2) | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | None | (start of Phase 3) | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | None | (start of Phase 4) | ✅ Match |
| T9 | T8 | T8 → T9 | ✅ Match |
| T10 | None | (start of Phase 5) | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |
| T12 | T11 | T11 → T12 | ✅ Match |
| T13 | T10, T11, T12 | T10 → T13, T11 → T13, T12 → T13 | ✅ Match |
| T14 | None | (start of Phase 6) | ✅ Match |
| T15 | T14 | T14 → T15 | ✅ Match |

No task depends on a task in a later phase. All dependencies point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ---------------------------- | ---------------- | ---------- | ------ |
| T1 | MCP tool registry | integration (MCP roundtrip) | integration | ✅ OK |
| T2 | MCP tool registry | integration | integration | ✅ OK |
| T3 | MCP tool registry | integration | integration | ✅ OK |
| T4 | `*ForUser` function + REST handler | integration (Postgres) | integration | ✅ OK |
| T5 | MCP tool registry | integration | integration | ✅ OK |
| T6 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T7 | MCP tool registry | integration | integration | ✅ OK |
| T8 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T9 | MCP tool registry | integration | integration | ✅ OK |
| T10 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T11 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T12 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T13 | MCP tool registry | integration | integration | ✅ OK |
| T14 | `*ForUser` function + REST handler | integration | integration | ✅ OK |
| T15 | MCP tool registry | integration | integration | ✅ OK |

No `Tests: none` used anywhere — every task modifies a code layer the matrix requires integration coverage for.

---

## Sub-Agent Offer

15 tasks packs into 2 task-budgeted batches (~7 tasks each): **Batch 1** = Phases 1-3 (T1-T7, 7 tasks), **Batch 2** = Phases 4-6 (T8-T15, 8 tasks). Offer to dispatch one batch worker per group — confirm before Execute dispatches anything.
