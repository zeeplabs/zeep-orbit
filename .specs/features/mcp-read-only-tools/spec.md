# MCP Read-Only Tools Specification

## Problem Statement

The MCP server (`.specs/features/mcp-server/`) exposes 7 tools today, all scoped to creating apps/tables/RLS/policies (`orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`, `orbit_create_policy_from_template`) plus 3 read tools (`orbit_list_apps`, `orbit_get_app_schema`, `orbit_list_policy_templates`). An AI agent connected via MCP (Claude Code, Claude Desktop, Cursor) can create things, and can see an app's tables/columns/RLS/policies as one aggregate document — but it cannot inspect most of the rest of an app's configuration (raw app record with storage/auth-provider status, team membership, issued API tokens, webhooks and their delivery history, the calling admin's own PATs) without asking the human operator to go check the Dashboard UI and paste the answer back. This turns simple diagnostic questions (e.g. "who has access to this app", "is this webhook actually firing", "what auth providers are configured") into a manual round-trip that the tool-calling model exists to avoid.

A full survey of the REST API surface (2026-08-17) confirmed this gap is broad and mapped every dashboard endpoint into three buckets: safe read-only candidates, operations that must never become a tool (destructive or credential-revealing), and operations touching end-user PII that need a separate legal/product decision before any MCP exposure. This spec covers only the first bucket.

## Goals

- [ ] An MCP client can read an app's raw configuration record (auth providers configured, storage bucket configured, rate limit, membership-relevant fields), one table's row policies, and the identity's own PATs — through tools that never existed before this spec.
- [ ] An MCP client can inspect an app's operational surface — team membership, issued app API tokens, webhooks (config, event mappings, delivery history), log volume/metrics — read-only, one call each.
- [ ] Every new tool authorizes through the exact same `GetApp(ctx, pool, appID, user)` + role-check path (or equivalent ownership scoping) the corresponding REST handler already uses — no new permission model, no tool that can see more than its caller's Dashboard session could see.
- [ ] Every new tool is a thin wrapper around an existing (or trivially extracted) `*ForUser`-style function, mirroring `mcp-server`'s Shared Operation Functions pattern — never new business logic duplicated between REST and MCP.
- [ ] No new tool in this spec returns a secret, credential, or raw token value that its REST equivalent doesn't already return in redacted form.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---|---|
| Any write/update/delete/rotate/revoke/regenerate tool | This spec is read-only by definition; every mutating endpoint surveyed carries its own risk profile (data loss, credential invalidation, privilege escalation) that deserves a dedicated spec and explicit case-by-case confirmation, not a bundle with read tools. |
| `orbit_list_app_users` (end-user directory) and any tool touching end-user data (`DataBrowserQuery`/`Create`/`Update`, `UpdateAppEnduserRoles`) | Returns PII of an app's own end users (potentially health-adjacent given Starbem's domain). The 2026-08-17 survey flagged this bucket as needing explicit business/legal confirmation before any MCP exposure — including read-only. Not resolved here; tracked as a follow-up decision, not assumed. |
| Platform-level (superadmin, cross-app) read tools: `ListUsers` (dashboard accounts), `ListOAuthClients`, `ListAuditLog`, GitHub integration config/templates, deploy-provider config/status, system config, global auth-provider config | None of these are scoped to "an app the caller already has access to" — they either require superadmin (a privilege tier this spec's tools don't check for) or leak information about the whole platform install, not the caller's own apps. `ListAuditLog` (`internal/dashboard/handler.go:2841`) was initially considered app-scoped during the 2026-08-17 survey; closer reading found `AuditLogFilter` (`internal/dashboard/audit_store.go:78-84`) has no app field at all and the REST handler hard-requires `user.Role == "superadmin"` — it is platform-wide audit, the same category as the others in this row, not a per-app read. Deferred to a possible separate "platform admin MCP tools" spec if a concrete need shows up. |
| Frontend-app tools (`ListFrontendApps`, `GetFrontendApp`, `GetFrontendAppSyncStatus`) | Frontend-app membership/access is a separate entity from backend apps (`frontend-app-entity` spec) with its own role-check surface; bundling it here would require re-verifying an entirely different authorization path instead of reusing one. Deferred to its own follow-up once the backend-app read tools in this spec are proven. |
| Pagination/filtering parameters beyond what each underlying `*ForUser`/store function already accepts | Every listed tool wraps an existing function; none of them gain new query capability in this spec — a tool exposes exactly the shape its wrapped function already returns. |
| Changing what any existing REST endpoint returns | This spec only adds new MCP tool wrappers. No REST handler behavior changes. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| Tool-to-function mapping | One tool per existing read function/handler, no aggregation beyond what already exists (e.g. no new "get everything about this app" super-tool) | Matches `mcp-server`'s established "one tool per existing REST operation" convention (design.md) — keeps each tool's input schema mirroring a request shape a human reviewer can already reason about | n |
| `GetApp` exposure needs a new `*ForUser` wrapper | `GetApp(ctx, pool, appID, user)` already takes `user` and does the role check inline (used directly by `CreateAppTableForUser` etc.) — the MCP tool can call it directly, applying `RedactSecrets()` to the returned `*AppRow` the same way `ListAppsForUser` does, without needing a new named wrapper function | `GetApp` is already the shared operation function; wrapping it in a same-named `*ForUser` alias would be a no-op layer. Redaction must be applied explicitly since `GetApp` itself (used internally by write paths) intentionally returns unredacted secrets for provisioning use | n |
| `ListAppTokens`, `ListPATs`, `ListAppMembers`, `ListTablePolicies`, `ListWebhooks`/`GetWebhook`/`ListDeliveries`/`ListEventMappings`, `ListAppProviders` currently take no `user`/role argument | Each new tool calls `GetApp(ctx, pool, appID, user)` first to resolve+authorize the app, discards the returned `*AppRow` (or reuses fields from it where useful, e.g. table existence check for `ListTablePolicies`), then calls the existing store function — mirroring how `CreateTablePolicyForUser` already composes `GetApp` + `CreateTablePolicy` | These are internal store/handler functions already called from an HTTP handler that does its own `GetApp`-based auth check inline; the MCP tool reproduces that same two-step shape rather than the store function taking on a new auth responsibility it wasn't designed for | n |
| `orbit_get_logs_metrics` scope and authorization mechanism | `LogsMetrics`'s REST handler (`internal/dashboard/handler.go:2026`) takes **no app parameter at all** — it calls `ListOwnedAppNames(ctx, pool, user)` for the set of app names the caller can see (`nil` for superadmin/`CanReadAnyApp`, meaning unfiltered), then returns one aggregate `LogMetrics` object (`internal/dashboard/logs.go:31-38`: total requests, latency, 4xx/5xx counts, method breakdown) for the last 1-minute window across all of those apps at once, with a `RequestsPerApp map[string]int` breakdown inside it. The new tool takes **no input** and returns this same caller-wide aggregate — it is not a per-app tool despite living in this app-scoped spec | Corrects an initial misreading during the 2026-08-17 survey that assumed this endpoint was app-scoped like most others; `RingBuffer.Metrics(allowedApps map[string]bool)` (`internal/dashboard/logs.go:99-124`) confirms the aggregation happens across every allowed app in one pass, with no way to request a single app's slice — the `RequestsPerApp` field is the closest thing to per-app detail, and it's already inside the one response | y — confirmed by reading `logs.go:31-124` |
| `ListPATs` scope | The tool lists only the calling identity's own PATs (`ListPATs(ctx, pool, user.ID)`, already ownership-scoped, no `appID` parameter) — not app-scoped, since PATs aren't tied to one app | Matches the existing REST endpoint `GET /dashboard/api/me/pats`, which is a "my own tokens" view, not an app-management view; no new tool should let a caller enumerate another user's PATs | y — matches REST behavior 1:1 |
| `ListAppTokens` / `ListPATs` field exposure | Tool output includes only metadata already present in `AppTokenRow`/`PATRow` (id, name, kind, expiry, revoked/last-used timestamps) — never the raw token/JTI value, since neither store function nor its REST equivalent ever returns the plaintext token after creation | These are the same rows the existing REST list endpoints already return; no new field is added or removed | y |
| `ListAppProviders` secret redaction | Reuses `redactAuthProviderSecrets` (the same allow-list function `GetAppAuthProviders`/`ListAppProviders`'s REST handler already calls) — the tool never bypasses it | Direct reuse of the credential-leak fix from 2026-08-17 (`CHANGELOG.md` Unreleased/Security) — a new read surface must not reopen that exact class of bug | y |
| Superadmin/cross-app visibility for these new tools | Unchanged from whatever `GetApp`'s existing role check already grants a superadmin (full visibility across apps) — no new restriction and no new grant beyond what the REST endpoint already allows the same identity | Consistent with the stated goal: a tool never sees more than the caller's Dashboard session already could | y |
| Where a wrapped REST endpoint returns a raw Go error / info-leaking distinction (e.g. 403 vs 404) | Tool preserves the same forbidden-vs-not-found distinction the REST handler already returns for that resource, mapped through the existing `mapWriteError`-style structured-error convention (`internal/mcpserver/tools.go`) — never a raw `err.Error()` string, per `AGENTS.md` §4 | No new information-disclosure surface should be introduced by the MCP wrapper that the REST endpoint doesn't already have | n |
| Audit logging for read tools | None of the new tools call `h.audit(...)` — reads are not audited today anywhere in the REST API (only mutations are), so this spec does not introduce a new audit requirement for reads | Matches existing behavior exactly (`ListApps`, `GetApp`, `orbit_list_apps`, `orbit_get_app_schema` are already unaudited reads); inventing audit-on-read here would be a new platform-wide policy decision out of this spec's scope | y |
| Empty-result shape | Every list-returning tool returns `[]` (empty JSON array), never `null`, when the underlying resource has zero rows | Matches the existing convention already validated for `GetAppSchemaForUser` (`tables: []`, not null) per `mcp-server/tasks.md`'s test coverage matrix; several of the underlying store functions (`ListPATs`, `ListTablePolicies`) already `make([]T, 0)` explicitly | y |

**Open questions:** none blocking — all resolved or logged above.

---

## User Stories

### P1: Agent inspects a single app's own configuration and its table-level policies ⭐ MVP

**User Story**: As an operator using an MCP-connected agent, I want to read an app's raw configuration (auth providers, storage, rate limit — redacted) and a specific table's row policies, so that I can diagnose "why is X not working" without switching to the Dashboard UI.

**Why P1**: These are the two gaps closest to the tool set that already exists — `orbit_get_app_schema` aggregates tables+columns+RLS+policy-names, but nothing returns the app's own record, and nothing lets an agent re-check one table's policies without re-fetching the entire schema.

**Acceptance Criteria**:

1. WHEN an MCP client calls a new `orbit_get_app` tool with a valid `app_id` the caller has access to THEN the system SHALL return that app's `AppRow` with `RedactSecrets()` already applied (same shape `orbit_list_apps` returns per-item today).
2. WHEN an MCP client calls a new `orbit_list_table_policies` tool with a valid `app_id` and `table_name` the caller has access to THEN the system SHALL return every row policy on that table in the same shape `ListTablePolicies` already returns to the REST handler.
3. IF the caller has no membership/role on the given `app_id` (and is not superadmin/`CanReadAnyApp`) THEN both tools SHALL return the same forbidden/not-found error `GetApp` already returns for that case, never the underlying resource data.
4. IF `table_name` does not exist on the given app THEN `orbit_list_table_policies` SHALL return a not-found tool error, never an empty list (an empty list means "table exists, zero policies").
5. The system SHALL NOT include `client_secret`, `secret_access_key`, `jwt_secret`, or any other credential value in `orbit_get_app`'s response, under any field name.

**Independent Test**: With an app that has one auth provider configured and one table with two policies, call `orbit_get_app` and confirm the response has no secret value; call `orbit_list_table_policies` for that table and confirm both policies are returned; call it for a nonexistent table name and confirm a not-found error, not `[]`.

---

### P2: Agent inspects who and what has access to an app ⭐

**User Story**: As an operator using an MCP-connected agent, I want to list an app's team members, its issued API tokens, its configured login providers, and my own personal access tokens, so that an agent can answer "who/what can access this app" without me checking four different Dashboard pages.

**Why P2**: Builds on P1's single-app read access; useful for access-review and onboarding/offboarding conversations, but not blocking the core "understand this app's schema/config" use case P1 covers.

**Acceptance Criteria**:

1. WHEN an MCP client calls a new `orbit_list_app_members` tool with a valid `app_id` the caller has access to THEN the system SHALL return every member row (`user_id`, `role`, `created_at`) exactly as `ListAppMembers` already returns to its REST handler.
2. WHEN an MCP client calls a new `orbit_list_app_tokens` tool with a valid `app_id` the caller has access to THEN the system SHALL return every issued app API token's metadata (id, name, expiry, revoked/last-used timestamps) with no raw token/JTI value.
3. WHEN an MCP client calls a new `orbit_list_app_auth_providers` tool with a valid `app_id` the caller has access to THEN the system SHALL return the same redacted shape (`redactAuthProviderSecrets`) `GetAppAuthProviders`'s REST handler already returns.
4. WHEN an MCP client calls a new `orbit_list_my_pats` tool THEN the system SHALL return only personal access tokens owned by the calling identity, with no raw token value, regardless of which app (if any) is passed.
5. IF the caller has no membership/role on the given `app_id` (and is not superadmin/`CanReadAnyApp`) THEN `orbit_list_app_members`, `orbit_list_app_tokens`, and `orbit_list_app_auth_providers` SHALL each return the same forbidden/not-found error their REST equivalents already return.

**Independent Test**: With an app that has two members (one admin, one viewer), one active app token, and Google auth configured, call all three app-scoped tools and confirm the member list, token list (no secret), and provider list (no `client_secret`) each match what the Dashboard UI shows for the same app; call `orbit_list_my_pats` and confirm it returns the caller's PATs regardless of `app_id`.

---

### P3: Agent inspects an app's operational history — webhooks and request volume

**User Story**: As an operator using an MCP-connected agent, I want to read an app's webhook configuration and delivery history, plus a caller-wide request-volume snapshot across the apps I can see, so that an agent can help debug "is this webhook firing" or "why did requests spike" conversations.

**Why P3**: Genuinely useful, but strictly operational/debugging value rather than blocking any create/configure workflow — lowest priority of the three tiers, and the bucket most likely to need pagination/volume tuning once real usage exists.

**Acceptance Criteria**:

1. WHEN an MCP client calls a new `orbit_list_webhooks` tool with a valid `app_id` the caller has access to THEN the system SHALL return every webhook configured for that app in the same shape `ListWebhooks` already returns.
2. WHEN an MCP client calls a new `orbit_get_webhook` tool with a valid `app_id` and `webhook_id` THEN the system SHALL return that webhook's full config (no signing-secret value) plus its event mappings (`ListEventMappings`).
3. WHEN an MCP client calls a new `orbit_list_webhook_deliveries` tool with a valid `app_id` and `webhook_id` THEN the system SHALL return delivery history using the same `limit`/`offset` bounds `ListDeliveries` already enforces (no unbounded new query capability).
4. WHEN an MCP client calls a new `orbit_get_logs_metrics` tool (no input) THEN the system SHALL return the same caller-wide aggregate `LogMetrics` shape `LogsMetrics`'s REST handler already returns, restricted to the app names `ListOwnedAppNames` grants the caller (unrestricted for superadmin/`CanReadAnyApp`, matching the REST handler exactly).
5. IF the caller has no membership/role on the given `app_id` (and is not superadmin/`CanReadAnyApp`) THEN `orbit_list_webhooks`, `orbit_get_webhook`, and `orbit_list_webhook_deliveries` SHALL each return the same forbidden/not-found error their REST equivalents already return.

**Independent Test**: With an app that has one webhook with three past deliveries, call `orbit_list_webhooks`, `orbit_get_webhook`, and `orbit_list_webhook_deliveries` and confirm the returned data matches the Dashboard's Webhooks page for that same app; call `orbit_get_logs_metrics` as a member of only one app and confirm `RequestsPerApp` contains at most that app's entries, then as a superadmin and confirm it can contain other apps' entries too.

---

## Edge Cases

- IF `app_id` does not exist at all (never existed, or was deleted) THEN every tool in this spec SHALL return the same not-found error `GetApp` already returns for that case — no tool distinguishes "never existed" from "you can't see it" beyond what the REST API already discloses.
- IF a list-returning tool's underlying resource has zero rows THEN the tool SHALL return `[]`, never `null` and never an error.
- IF `webhook_id` is valid but belongs to a different app than the given `app_id` THEN `orbit_get_webhook`/`orbit_list_webhook_deliveries` SHALL return a not-found error, mirroring the existing REST route's implicit app-scoping (never returning another app's webhook data through a mismatched `app_id`).
- WHEN the caller is a superadmin or holds `CanReadAnyApp` THEN every tool SHALL behave exactly as the equivalent REST call would for that same identity — no additional restriction introduced by the MCP layer.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| -------------- | ----- | ------ | ------ |
| MROT-01 | P1: Agent inspects app config + table policies | - | Pending |
| MROT-02 | P1: Agent inspects app config + table policies | - | Pending |
| MROT-03 | P1: Agent inspects app config + table policies | - | Pending |
| MROT-04 | P2: Agent inspects access surface | - | Pending |
| MROT-05 | P2: Agent inspects access surface | - | Pending |
| MROT-06 | P2: Agent inspects access surface | - | Pending |
| MROT-07 | P2: Agent inspects access surface | - | Pending |
| MROT-08 | P3: Agent inspects operational history | - | Pending |
| MROT-09 | P3: Agent inspects operational history | - | Pending |

**ID format:** `MROT-[NUMBER]` (MCP Read-Only Tools)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 9 total, 0 mapped to tasks, 9 unmapped ⚠️ (tasks.md not yet written — spec-only phase per current scope)

---

## Success Criteria

- [ ] All 10 new tools listed across P1-P3 (`orbit_get_app`, `orbit_list_table_policies`, `orbit_list_app_members`, `orbit_list_app_tokens`, `orbit_list_app_auth_providers`, `orbit_list_my_pats`, `orbit_list_webhooks`, `orbit_get_webhook`, `orbit_list_webhook_deliveries`, `orbit_get_logs_metrics`) are callable via a real MCP client against a running server and return data matching the equivalent Dashboard UI page.
- [ ] Zero secret/credential values appear in any new tool's response, verified the same boundary-level way `TestOrbitListAppsTool_ResponseNeverContainsSecrets` verifies the existing tools (2026-08-17 credential-leak fix precedent).
- [ ] Every new tool's authorization is proven to reject a caller without access to the given `app_id`, using the same test shape as the existing write tools' forbidden-path tests.
