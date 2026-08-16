# MCP Server for Zeep Orbit Operations Design

**Spec**: `.specs/features/mcp-server/spec.md`
**Status**: Draft

---

## Architecture Overview

A new streamable-HTTP MCP endpoint (`/dashboard/mcp`) is mounted alongside the existing `/dashboard/api/...` routes in `internal/server/server.go`. It authenticates every request with a new bearer artifact — the Personal Access Token (PAT) — instead of the `zeep_session` cookie, resolving to the same `DashboardUser` type `RequireAuth` already produces. Each MCP tool is a thin wrapper around a **shared operation function** extracted from the existing REST handler's body, so the REST endpoint and the MCP tool call the exact same validation/store/audit code — the MCP layer never re-implements business logic, it only re-packages the request/response shape for tool calling. Policy Templates (`policyTemplates.ts`, currently frontend-only) are ported to a small pure Go package so `orbit_create_policy_from_template` can run entirely server-side.

A PAT can be **issued two ways**, converging on the same store: an admin creates one manually in Settings (unchanged from the original design), or an OAuth-only client (Claude Desktop) drives a standard OAuth 2.1 authorization-code-with-PKCE flow that ends in the same `dashboard_pats` row being created. `RequirePAT` and `ResolvePAT` don't know or care which path produced the token they're validating — OAuth is a second *front door*, not a second auth system.

```mermaid
graph TD
    Ext[External MCP client<br/>Claude Code / Cursor / Codex] -->|Bearer PAT, config-file token| McpH[MCP HTTP Handler<br/>internal/mcpserver]
    ClaudeDesktop[Claude Desktop<br/>OAuth-only connector] -->|1. discover metadata| OAuthMeta[/.well-known/oauth-authorization-server/]
    ClaudeDesktop -->|2. register| OAuthReg[POST /dashboard/oauth/register]
    ClaudeDesktop -->|3. browser redirect| OAuthAuthz[GET /dashboard/oauth/authorize]
    OAuthAuthz -->|no session| Login[Existing Dashboard login]
    Login --> Consent[Consent screen]
    Consent -->|admin grants| OAuthAuthz
    OAuthAuthz -->|PKCE code| ClaudeDesktop
    ClaudeDesktop -->|4. exchange code| OAuthToken[POST /dashboard/oauth/token]
    OAuthToken -->|mints| PATStore[(dashboard_pats)]
    ClaudeDesktop -->|Bearer access_token| McpH
    ChatDrawer["Create with AI" chat drawer<br/>internal/dashboard backend] -->|Bearer PAT, server-minted ephemeral| McpH

    McpH -->|resolve PAT| PATAuth[RequirePAT middleware<br/>internal/mcpserver/auth.go]
    PATAuth -->|hash lookup, any issuance kind| PATStore
    PATAuth -->|per-PAT budget| RL[RateLimiter.MiddlewareKeyedBy]

    McpH --> Tools[Tool Registry<br/>internal/mcpserver/tools.go]
    Tools -->|orbit_create_app| OpsApp[CreateAppForUser<br/>internal/dashboard/handler.go]
    Tools -->|orbit_create_table| OpsTable[CreateAppTableForUser]
    Tools -->|orbit_set_table_rls_mode| OpsRLS[UpdateAppTableForUser]
    Tools -->|orbit_list_policy_templates<br/>orbit_create_policy_from_template| OpsTpl[policytemplates package<br/>+ CreateTablePolicyForUser]
    Tools -->|orbit_list_apps / orbit_get_app_schema| OpsRead[ListAppsForUser / GetAppSchemaForUser]

    OpsApp --> Store[(zeep_system catalog tables)]
    OpsTable --> Store
    OpsRLS --> Store
    OpsTpl --> Store
    OpsRead --> Store

    OpsApp --> Audit[h.audit(...) — same call REST uses]
    OpsTable --> Audit
    OpsRLS --> Audit
    OpsTpl --> Audit

    RestH[Existing REST Handlers<br/>CreateApp, CreateAppTable, ...] --> OpsApp
    RestH --> OpsTable
    RestH --> OpsRLS
    RestH --> OpsTpl

    Dash[Dashboard UI — Settings] -->|session cookie, RequireAuth| PatH[PAT Dashboard Handler]
    PatH --> PATStore
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|---|---|---|
| `DashboardUser`, `GetSessionUser` pattern | `internal/dashboard/middleware.go:20-24`, `store.go:22` | `RequirePAT` produces the identical `*DashboardUser` shape `RequireAuth` does, injected into context the same way, so every downstream operation function takes the same `user *DashboardUser` argument regardless of which middleware ran. |
| `generateToken` | `internal/dashboard/handler.go:1798` (`crypto/rand`, 32 bytes) | Reused verbatim for PAT generation — same entropy source already trusted for webhook tokens. |
| One-way hash pattern (pre-`EncryptWebhookToken` design) | `internal/crypto/aes.go` history — webhook tokens were originally SHA-256-hashed before the reversible-encryption change | PATs reuse the **original hash approach**, not the webhook's current encryption: a PAT never needs to be redisplayed after creation (unlike a webhook callback URL), so a one-way SHA-256 hash + constant-time compare is the simpler, more conservative choice — see Tech Decisions. |
| `RateLimiter.MiddlewareKeyedBy` | `internal/dashboard/middleware.go:136` | Reused unmodified, keyed by PAT id instead of webhook id — same rationale (one noisy token can't starve others behind the same LB IP). |
| `h.audit(...)` | `internal/dashboard/handler.go:1665` | Every extracted operation function calls this exactly where the REST handler currently does — no new audit code path, no new `resourceType` values beyond what already exists (`app`, `table`, `table_policy`). |
| `table_policies_store.go` CRUD + `BuildPolicySQL` | `internal/provisioner/policy.go`, `internal/dashboard/table_policies_store.go` | `orbit_create_policy_from_template`'s underlying writes go through the exact same `CreateTablePolicy` store call the REST policy endpoint and the frontend template picker already use. |
| Provisioner migration pattern | `internal/dashboard/provisioner.go` | New `dashboard_pats` catalog table added the same `CREATE TABLE IF NOT EXISTS` way as every other `zeep_system` table. |
| Existing REST handlers as the source of truth for validation | `internal/dashboard/handler.go` (`CreateApp:876`, `CreateAppTable:1106`, `CreateTablePolicy:1372`, table RLS update inside `UpdateAppTable:1180`) | Their bodies are **extracted, not duplicated** — see Components below. |

### Integration Points

| System | Integration Method |
|---|---|
| Router (`internal/server/server.go`) | New route group mounted at `/dashboard/mcp`, wrapped with `RequirePAT(pool)` instead of `RequireAuth(pool)`, plus its own `RateLimiter.MiddlewareKeyedBy` instance — sibling to the existing `/dashboard` route group, not nested inside it (different auth mechanism). |
| `audit_log` | Every operation function's audit call already tags `userID`/`userEmail` from the resolved `DashboardUser` — a PAT-originated action is indistinguishable in the audit log from a session-originated one today; **open question for `design.md` review**: whether audit metadata should note the calling PAT id for forensics (see Risks & Concerns). |
| Dashboard Settings UI | New "Personal Access Tokens" section, same tab pattern as other Settings sub-sections (`Settings.tsx`), authenticated by the existing `zeep_session` cookie — a PAT is *managed* over the session, never used to manage itself. |
| MCP SDK dependency | `github.com/modelcontextprotocol/go-sdk` (official Go SDK) — new dependency, no MCP library exists in this codebase today. Provides `mcp.Server`, tool registration with Go-struct-derived JSON schemas, and a `StreamableHTTPHandler` — matches the "streamable HTTP, not stdio" decision from `spec.md`. |

---

## Components

### `PATStore`

- **Purpose**: CRUD for personal access tokens — create (hash + store), resolve by presented token, list for an admin, revoke.
- **Location**: `internal/dashboard/pat_store.go`
- **Interfaces**:
  - `CreatePAT(ctx, pool, userID string, name string, kind string, expiresAt *time.Time) (plaintextToken string, PAT, error)` — generates via `generateToken`, stores `sha256(token)` hex, returns the plaintext exactly once. `kind` is `"manual"` (Settings UI, `expiresAt=nil`), `"ephemeral"` (chat drawer), or `"oauth"` (token endpoint, also sets `refresh_token_hash` via a follow-up call — see `OAuthServer`).
  - `ResolvePAT(ctx, pool, presentedToken string) (*DashboardUser, error)` — hashes the presented token, looks up by hash, joins to `dashboard_users`, rejects if revoked, past `expires_at`, or the user no longer exists/is deactivated — same check regardless of `kind`. No caching — every call hits the DB, consistent with `AGENTS.md`'s no-in-memory-session-state rule.
  - `ListPATs(ctx, pool, userID string) ([]PATRow, error)` — never returns the token value, only id/name/created_at/last_used_at.
  - `RevokePAT(ctx, pool, userID, patID string) error` — scoped to the requesting user's own tokens (mirrors the webhook-mapping ownership-scoping fix — a PAT id must never be revocable by guessing it).
  - `TouchLastUsed(ctx, pool, patID string) error` — best-effort, fire-and-forget update for the Settings UI's "last used" column; failure here must never fail the underlying tool call.
- **Dependencies**: `db.Pool`.
- **Reuses**: Structural pattern from `table_policies_store.go` (catalog row + ownership-scoped mutations + not-found sentinel error).

### `RequirePAT` middleware

- **Purpose**: MCP-layer equivalent of `RequireAuth` — resolves a `Bearer` token to a `DashboardUser` instead of a cookie.
- **Location**: `internal/mcpserver/auth.go`
- **Interfaces**: `RequirePAT(pool *db.Pool) func(http.Handler) http.Handler` — reads `Authorization: Bearer <token>`, calls `dashboard.ResolvePAT`, injects the same `userCtxKey` context value `RequireAuth` uses (exported accessor added to `dashboard` package so both middlewares interoperate with the same `UserFromContext` reader).
- **Dependencies**: `PATStore`.
- **Reuses**: `dashboard.UserFromContext` reader, same context-key convention.

### Shared Operation Functions (extracted from existing handlers)

- **Purpose**: The single code path both the REST JSON handler and the MCP tool handler call — this is the mechanism that satisfies spec goal "every MCP tool call executes through the exact same authorization and validation path."
- **Location**: same files as today's handlers (`internal/dashboard/handler.go`, `internal/dashboard/table_policies_handler.go` or equivalent) — new unexported-to-exported function extractions, not new files.
- **Interfaces** (illustrative signatures; exact param structs match today's decoded request bodies):
  - `CreateAppForUser(ctx, pool, user *DashboardUser, input CreateAppInput) (*App, error)`
  - `CreateAppTableForUser(ctx, pool, user *DashboardUser, appID string, input CreateTableInput) (*Table, error)`
  - `UpdateTableRLSModeForUser(ctx, pool, user *DashboardUser, appID, tableName, rlsMode string) (*Table, error)` — extracted from the RLS-relevant slice of `UpdateAppTable`, not the whole endpoint (that endpoint also handles column/index changes out of this feature's scope).
  - `CreateTablePolicyForUser(ctx, pool, user *DashboardUser, appID, tableName string, def PolicyDef) (*PolicyRow, error)`
  - `ListAppsForUser(ctx, pool, user *DashboardUser) ([]App, error)`
  - `GetAppSchemaForUser(ctx, pool, user *DashboardUser, appID string) (*AppSchema, error)` — new read shape (tables + columns + RLS mode per table); no existing single endpoint returns exactly this, so this one **is** new aggregation logic, calling the same underlying store reads `GetApp`/`ListAppTables`/`ListTablePolicies` already use elsewhere.
  - Each existing REST handler (`CreateApp`, `CreateAppTable`, the RLS-mode branch of `UpdateAppTable`, `CreateTablePolicy`) is refactored to: decode body → call the `*ForUser` function → `writeJSON`/`writeError`. Behavior is unchanged; the function boundary just moves.
- **Dependencies**: same store/audit dependencies the original handlers had.
- **Reuses**: This *is* the reuse mechanism — no new logic beyond what's already validated in the existing handlers, apart from `GetAppSchemaForUser`'s aggregation.

### `policytemplates` (new package, ported from frontend)

- **Purpose**: Server-side, pure-Go equivalent of `internal/dashboard/ui/src/lib/policyTemplates.ts`, so `orbit_create_policy_from_template` can build `PolicyDef`s without a browser.
- **Location**: `internal/policytemplates/templates.go`
- **Interfaces**: Mirrors the TS file function-for-function — `List() []TemplateDefinition`, `BuildOwnerOnlyPolicies(actions, roles []string) []PolicyDef`, `BuildOpenReadPolicy(roles []string) PolicyDef`, `BuildReadOnlyPolicy(roles []string) PolicyDef`, `BuildValueMatchPolicy(column, value string, roles []string) PolicyDef`, `BuildOpenReadOwnerWritePolicies(readRoles []string) []PolicyDef`. Same `TemplateId` constants (`owner_only`, `open_read`, `read_only`, `value_match`, `open_read_owner_write`, `blocked_by_default`), same generated-name convention (`tpl_<templateId>_<action>`).
- **Dependencies**: none (pure), matches the TS file's own "no JSX, no network calls" framing.
- **Reuses**: Logic ported 1:1 from `policyTemplates.ts` — **not** a shared codegen source in V1 (see Risks & Concerns: this creates a two-copies-must-stay-in-sync liability, explicitly accepted for now, flagged for a follow-up ticket).

### `OAuthClientStore`

- **Purpose**: Stores dynamically-registered OAuth clients (Claude Desktop and equivalents), keyed by a server-issued `client_id`.
- **Location**: `internal/dashboard/oauth_client_store.go`
- **Interfaces**:
  - `RegisterClient(ctx, pool, input RegisterClientInput) (OAuthClient, error)` — issues a random `client_id` (public, no secret required for a PKCE-only public client, per standard practice for native/desktop apps), stores the client's declared name/redirect URIs.
  - `GetClient(ctx, pool, clientID string) (OAuthClient, error)` — validates a `redirect_uri` at the authorize step actually belongs to the registered client (prevents an authorization code being redirected somewhere the client never declared).
- **Dependencies**: `db.Pool`.
- **Reuses**: Structural pattern from `table_policies_store.go` (catalog row, not_found sentinel).

### `OAuthServer`

- **Purpose**: Implements the four OAuth-facing endpoints — metadata discovery, dynamic registration, authorization (login+consent), and token exchange — that let an OAuth-only MCP client obtain a token resolvable by the same `ResolvePAT` every other consumer uses.
- **Location**: `internal/dashboard/oauth_server.go` (handlers), `internal/dashboard/ui/src/components/OAuthConsent.tsx` (consent screen)
- **Interfaces**:
  - `GetMetadata(w, r)` — serves `GET /.well-known/oauth-authorization-server`, a static JSON document pointing at the three endpoints below.
  - `RegisterClient(w, r)` — `POST /dashboard/oauth/register`, wraps `OAuthClientStore.RegisterClient`.
  - `Authorize(w, r)` — `GET /dashboard/oauth/authorize`. If no `zeep_session` cookie, redirects to the existing login page with a return-to URL; once logged in, renders `OAuthConsent.tsx` naming the requesting client (looked up via `client_id`) and its requested scope. On grant, generates a single-use authorization code bound to the PKCE `code_challenge`, the `client_id`, and the granting `DashboardUser`'s id, stored short-lived (e.g. 10 minutes) in a small `oauth_auth_codes` table.
  - `Token(w, r)` — `POST /dashboard/oauth/token`. `grant_type=authorization_code`: validates the code (unused, unexpired, PKCE `code_verifier` matches the stored `code_challenge`), marks it used, mints an access-token PAT row (`kind="oauth"`, short `expires_at`) and a refresh token (`refresh_token_hash` on the same row), returns both. `grant_type=refresh_token`: validates the presented refresh token, rotates it (issues a new refresh token, invalidates the old one — standard rotation), mints a fresh access-token PAT row.
- **Dependencies**: `PATStore` (T1's `CreatePAT`-equivalent, extended — see Data Models), `OAuthClientStore`, existing Dashboard login/session mechanism.
- **Reuses**: Existing login page and `zeep_session` mechanism for the "who is granting consent" step — no new identity system. `generateToken`/hash pattern from `PATStore` for both the authorization code and the refresh token.

### MCP Tool Registry

- **Purpose**: Registers every tool with the MCP SDK, translating tool-call args into the shared operation functions' input structs and their results into MCP tool results (or structured errors).
- **Location**: `internal/mcpserver/tools.go`
- **Interfaces**: `RegisterTools(server *mcp.Server, deps ToolDeps)` where `ToolDeps` bundles `*db.Pool` and any other shared dependency. One registration call per tool: `orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`, `orbit_list_policy_templates`, `orbit_create_policy_from_template`, `orbit_list_apps`, `orbit_get_app_schema`. Each tool's input schema is derived from the same Go struct the REST handler decodes into (`CreateAppInput`, etc.) via the SDK's struct-tag-based schema generation — one struct, one schema, one validation set, used by both transports.
- **Dependencies**: Shared Operation Functions, `policytemplates`.
- **Reuses**: Request/response structs already defined for the REST handlers.

### MCP HTTP Handler

- **Purpose**: Wires the MCP SDK's `Server` + `StreamableHTTPHandler` into the existing chi router.
- **Location**: `internal/mcpserver/server.go`
- **Interfaces**: `NewHandler(pool *db.Pool, rl *RateLimiter) http.Handler` — constructs the `mcp.Server`, calls `RegisterTools`, wraps with `RequirePAT(pool)` then `rl.MiddlewareKeyedBy(patIDFromContext)`.
- **Dependencies**: `PATStore`, MCP SDK, Tool Registry.
- **Reuses**: `RateLimiter` type as-is (see Integration Points).

### Dashboard PAT Handler + Settings UI

- **Purpose**: Session-authenticated CRUD for an admin's own PATs.
- **Location**: `internal/dashboard/pat_handler.go` (backend), `internal/dashboard/ui/src/components/PersonalAccessTokens.tsx` (frontend), mounted under `/dashboard/api/me/pats` (self-scoped, no app id — a PAT belongs to the admin, not to one app) behind the existing `RequireAuth(pool)`.
- **Interfaces**: `CreatePAT`, `ListPATs`, `RevokePAT` handlers; React Query hooks `usePATs()`, `useCreatePAT`, `useRevokePAT` in `src/lib/api.ts`, following the exact mutation/`toast.error` pattern already used by `Webhooks.tsx`'s token rotation UI, including a `ConfirmDialog` for revoke (same component introduced for RLS-mode-switch/delete confirmations per the latest CHANGELOG entries).
- **Dependencies**: `PATStore`.
- **Reuses**: `ConfirmDialog`, TanStack Query mutation pattern, i18n key structure (new keys in both `en.json`/`pt-BR.json`).

### "Create with AI" Chat Drawer Backend (P3, `mcp-server` spec's last story)

- **Purpose**: Mediates the chat conversation and drives the same MCP tools an external client would, using a token minted for the admin's own live session.
- **Location**: `internal/dashboard/ai_app_creation.go` (new) + `internal/dashboard/ui/src/components/CreateWithAiDrawer.tsx` (replaces the disabled placeholder).
- **Interfaces**: `MintEphemeralPAT(ctx, pool, user *DashboardUser) (token string, error)` — a PAT variant with a short, fixed TTL (see Tech Decisions) created server-side, never returned to the frontend; the backend uses it as its own MCP client (in-process HTTP call to `/dashboard/mcp`, or a direct in-process `mcp.Client`/tool-registry call bypassing the network hop entirely — **left as an implementation choice for `tasks.md`**, both satisfy "same tool set" since the tool registry itself is the shared code, not the transport).
- **Dependencies**: MCP Tool Registry (or the MCP HTTP endpoint), `PATStore`.
- **Reuses**: Whatever LLM-calling infrastructure the org standardizes on for tool-calling loops (model, orchestration library) — **open question, not decided by this design**; this component only needs "something that can hold a tool-calling conversation against an MCP-compatible tool set," which is exactly what was just built for external clients.

---

## Data Models

### `dashboard_pats` (zeep_system)

```typescript
interface PersonalAccessToken {
  id: string
  user_id: string        // FK → dashboard_users.id
  name: string            // admin-chosen label, e.g. "Claude Code laptop"; for kind="oauth", the OAuth client's declared name
  token_hash: string       // SHA-256 hex of the plaintext token; plaintext never stored
  kind: "manual" | "ephemeral" | "oauth"  // manual = Settings-created; ephemeral = chat-drawer-minted; oauth = issued via the token endpoint
  oauth_client_id: string | null    // FK → oauth_clients.id; set only when kind="oauth"
  refresh_token_hash: string | null  // SHA-256 hex of the current refresh token; set only when kind="oauth"
  expires_at: string | null  // set for ephemeral and oauth tokens; null for manual PATs (no forced expiry in V1)
  revoked_at: string | null
  last_used_at: string | null
  created_at: string
}
```

**Relationships**: `user_id → dashboard_users.id`, `ON DELETE CASCADE` (a deleted dashboard user's tokens are meaningless and must stop resolving immediately — no soft-delete concern here, unlike app-facing resources). `oauth_client_id → oauth_clients.id`, `ON DELETE CASCADE`.

**Note**: `ephemeral: boolean` from the original design collapses into `kind` above — `kind="ephemeral"` replaces it 1:1, since a third `kind="oauth"` value needed the same "is this a short-lived, non-Settings-created token" concept, and a boolean can't express three cases.

### `oauth_clients` (zeep_system)

```typescript
interface OAuthClient {
  id: string           // the client_id handed back at registration
  name: string           // client-declared name, shown on the consent screen
  redirect_uris: string[]  // validated against the redirect_uri param at /authorize
  created_at: string
}
```

**Relationships**: none inbound; referenced by `dashboard_pats.oauth_client_id` and `oauth_auth_codes.client_id`.

### `oauth_auth_codes` (zeep_system)

```typescript
interface OAuthAuthCode {
  id: string
  code_hash: string        // SHA-256 hex of the authorization code
  client_id: string          // FK → oauth_clients.id
  user_id: string            // FK → dashboard_users.id — who granted consent
  code_challenge: string     // PKCE challenge, base64url(SHA-256(code_verifier))
  redirect_uri: string       // must match exactly at token exchange (standard OAuth requirement)
  used_at: string | null     // set on first exchange; a second exchange attempt is rejected
  expires_at: string         // short-lived, e.g. 10 minutes
  created_at: string
}
```

**Relationships**: `client_id → oauth_clients.id`, `user_id → dashboard_users.id`, both `ON DELETE CASCADE`. Purged the same 30-day-style retention pattern as `webhook_deliveries` (or more aggressively, since these are only ever useful for ~10 minutes — exact retention decided in `tasks.md`).

### `AppSchema` (response shape, not a stored table)

```typescript
interface AppSchema {
  app_id: string
  app_name: string
  tables: Array<{
    name: string
    rls_mode: "" | "owner" | "enabled" | "policy"
    columns: Array<{ name: string; type: string; nullable: boolean }>
    policies: Array<{ name: string; action: string; roles: string[] }>
  }>
}
```

**Relationships**: none — assembled at read time from `apps`, the app's provisioned table metadata, and `table_policies`; not a new persisted structure.

---

## Error Handling Strategy

| Error Scenario | Handling | User/Caller Impact |
|---|---|---|
| Missing/malformed `Authorization` header on `/dashboard/mcp` | `401`, MCP-level auth error, no tool executed | External client sees a clear auth failure before any tool call is attempted. |
| PAT hash not found, revoked, or expired | `401`, same as above | Same; revocation is immediate (no cache to invalidate). |
| PAT's owning `dashboard_users` row deleted/deactivated after mint | `401` (resolved live against the current user row every call, per `ResolvePAT`) | Same — no stale-identity window. |
| Tool input fails the shared struct's existing validation (same one the REST handler already runs) | Structured tool-error result naming the failing field, no partial write | LLM receives an actionable message it can react to (e.g. re-ask the admin for a valid table name), not a generic failure. |
| `orbit_create_policy_from_template` partway failure (composite template, sequential creates) | Same partial-failure contract as the frontend template picker: stop at first error, report which policies succeeded and which step failed | LLM can decide to retry only the missing piece or surface the partial state — never silently retries the whole sequence (would double-create the already-succeeded policies). |
| Underlying DB/internal error (500-class) | Generic message returned to the MCP caller; real error logged server-side only, per `AGENTS.md` §4 — the operation function itself already enforces this since it's the same function the REST handler uses | Caller never sees a raw Postgres error through the MCP transport, same guarantee the REST API already gives. |
| Rate limit exceeded for a PAT | MCP tool call rejected with a rate-limit error; connection itself stays open | Matches the webhook route's existing `429`-equivalent behavior, just keyed by PAT instead of webhook id. |
| `orbit_get_app_schema` called for an app the PAT's user has no access to | Same `404`/authorization-error the REST `GetApp` already returns for this case | No information disclosure beyond what the REST API already permits. |
| OAuth: unknown/malformed `client_id` at `/authorize` | `400` with a standard OAuth `error=invalid_client`, no redirect performed (redirecting to an unregistered URI would be an open-redirect risk) | Client-side integration bug is surfaced immediately, not silently swallowed. |
| OAuth: `redirect_uri` at `/authorize` doesn't match the client's registered URIs | `400` `error=invalid_request`, no code issued | Prevents an authorization code from being delivered to an attacker-controlled redirect. |
| OAuth: admin denies consent | Redirect back to the client's `redirect_uri` with `error=access_denied`, no code issued | Standard OAuth denial flow; client shows its own "connection cancelled" state. |
| OAuth: authorization code reused, expired, or PKCE verifier mismatch at `/token` | `400` `error=invalid_grant`, no token issued | Matches OAuth 2.1's mandatory single-use-code + PKCE verification; a reused code is treated as a compromise signal (see Risks & Concerns). |
| OAuth: refresh token reused after rotation (already exchanged once) | `400` `error=invalid_grant`, **and** the entire token family (the access token issued alongside it, and any tokens minted from later rotations) is revoked | Refresh-token-reuse detection — the standard signal that a refresh token has leaked; revoking the whole family limits the blast radius instead of only rejecting the one reuse attempt. |

---

## Risks & Concerns

| Concern | Location (file:line) | Impact | Mitigation |
|---|---|---|---|
| Policy Templates exist only as frontend TypeScript today — no server-side representation | `internal/dashboard/ui/src/lib/policyTemplates.ts` | Porting them to Go (`internal/policytemplates`) creates two copies of the same logic that can silently drift if one is edited without the other | Accepted for V1 (redesigning `policy-templates` into a shared/generated source is out of scope for this spec). Flag a follow-up ticket to either generate the TS from the Go package or vice versa once both exist; until then, any change to one **must** be manually mirrored — call this out explicitly in `tasks.md` and in the PR description whenever either file changes. |
| Extracting `*ForUser` functions touches already-shipped, tested REST handlers (`CreateApp`, `CreateAppTable`, `CreateTablePolicy`, part of `UpdateAppTable`) | `internal/dashboard/handler.go` | Refactor risk on stable, production code — a mistake here breaks the Dashboard UI, not just the new MCP path | Extraction must be behavior-preserving (pure move, not a rewrite); existing handler tests must keep passing unmodified as the acceptance bar, plus new tests added for the extracted function called directly. |
| Audit log entries from PAT-driven calls are indistinguishable from session-driven ones (no PAT id recorded in `metadata`) | `internal/dashboard/handler.go:1665` (`audit` signature) | An admin investigating "who created this app" can't tell whether it was a human click or an LLM tool call, or which PAT/token did it | V1 accepts this (same audit shape, no schema change to `audit_log`); if this proves to matter operationally, add `metadata.pat_id` in a follow-up — deliberately deferred rather than changing a widely-used shared function's signature in this pass. |
| `RequirePAT` re-validates against the DB on every single tool call (per spec, no caching, for revocation correctness) | `internal/mcpserver/auth.go` | An LLM issuing many rapid tool calls (e.g. one per column while building a table) adds one extra DB round-trip per call versus a cached session | Accepted — matches this codebase's existing stance against in-memory session state across replicas (`AGENTS.md`); the query is a single indexed hash lookup, not a meaningfully expensive one. |
| Ephemeral PAT for the "Create with AI" drawer is a second token *kind* sharing the same table/validation path as admin-created PATs | `dashboard_pats.kind = "ephemeral"` | A bug in expiry handling could let a chat-drawer token outlive its intended single-conversation lifetime | `ResolvePAT` must reject any row past its `expires_at` (any `kind` that sets one) with the same 401 path as a revoked token — covered by the same function, not a separate branch per kind. |
| Dynamic client registration is unauthenticated by nature (any caller can register a "client" before ever touching a user's data) | `internal/dashboard/oauth_client_store.go`, `POST /dashboard/oauth/register` | Someone could register thousands of fake clients — not a data-access risk (registration alone grants nothing), but a resource/spam concern, and a registered client's *name* is shown verbatim on the consent screen, so a malicious name could be crafted to look trustworthy (phishing-adjacent) | Rate-limit the registration endpoint by IP (reuse `RateLimiter.Middleware`, the plain per-IP variant — registration has no logical-caller identity yet to key by). Consent screen must display the redirect URI's origin alongside the client's self-declared name, so an admin isn't relying solely on a spoofable string. |
| Authorization code / refresh token compromise via logs, browser history, or a malicious redirect target | `oauth_auth_codes`, `dashboard_pats.refresh_token_hash` | A leaked code/refresh token grants the same access a stolen PAT would | Codes are single-use and short-lived (10 min) by design; refresh tokens rotate on every use with reuse-detection revoking the full family (see Error Handling Strategy) — same mitigations OAuth 2.1 itself mandates, not a bespoke scheme. |
| Consent screen is a **new** unauthenticated-adjacent surface (reachable by any registered client, showing an admin's own identity context) | `OAuthConsent.tsx`, `Authorize` handler | A convincing fake "client name" combined with a real Orbit login page could be used in a phishing attempt against an admin who doesn't scrutinize the redirect URI | Mitigated by the redirect-URI-origin display above; this is an inherent property of the OAuth authorization-code flow itself (every OAuth provider carries this same UX risk) and not something specific to this implementation to solve differently. |

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
|---|---|---|
| MCP transport | Streamable HTTP via `github.com/modelcontextprotocol/go-sdk`'s `StreamableHTTPHandler`, mounted as one more route in the existing chi router | Forced by the multi-replica, non-sticky-LB deployment model (`AGENTS.md`) — stdio assumes a single local process pair and doesn't apply to a hosted control plane. |
| PAT storage | One-way SHA-256 hash of a `crypto/rand`-generated token, never redisplayed after creation (same original design webhook tokens used before their reversible-encryption change) | A PAT, unlike a webhook callback URL, is never re-displayed for "paste into a provider's panel" reasons — there's no product need to decrypt it later, so the simpler, more conservative one-way hash is correct here and avoids re-litigating the webhook token's later encryption tradeoff. |
| Shared code path between REST and MCP | Extract `*ForUser` functions from existing handlers rather than having the MCP tool call the REST endpoint over HTTP internally | An internal HTTP round-trip (tool handler → loopback → REST handler) would add latency and a second serialization layer for no benefit; a direct Go function call achieves the spec's "same path" requirement with less moving parts. |
| Policy Templates duplication | Port `policyTemplates.ts` to a new pure Go package instead of trying to share one implementation across TS and Go in V1 | No existing cross-language codegen infrastructure in this repo; building one just for this feature would be a disproportionate new investment. Accepted as a tracked risk (see Risks & Concerns), not silently ignored. |
| Ephemeral PAT for "Create with AI" | Same `dashboard_pats` table, `kind="ephemeral"` + `expires_at` set, minted server-side, never shown to the admin | Reuses one auth artifact and one resolution function for all consumer types (external client via PAT, external client via OAuth, internal chat) instead of inventing a second token system — directly satisfies the spec's "one execution path regardless of caller" goal. |
| `orbit_get_app_schema` as new aggregation logic | Accepted as genuinely new code (not a pure extraction), assembling existing store reads (`GetApp`, table list, policy list) into one response | No existing endpoint returns this exact shape; inventing it is required for spec goal MCP-09 (LLM verifies state without re-deriving it from prior tool responses) and is small/read-only, low risk relative to the write-path extractions. |
| OAuth tokens land in `dashboard_pats`, not a separate table | Extend `dashboard_pats` with `kind`/`oauth_client_id`/`refresh_token_hash` instead of building a parallel `oauth_access_tokens` table | `RequirePAT`/`ResolvePAT` stay a single code path regardless of how a token was minted — the entire point of choosing OAuth-as-a-second-front-door over OAuth-as-a-second-auth-system (per spec Assumptions). A separate table would mean `RequirePAT` has to check two places, or a sync step to keep them coherent, for no benefit. |
| OAuth client authentication model | Public client, PKCE-only, no `client_secret` | Claude Desktop and equivalents are native/desktop apps — they cannot keep a `client_secret` confidential (it would ship inside the installed app). This matches OAuth 2.1's own guidance for native app clients: PKCE replaces the confidential-secret requirement entirely, it isn't an additional layer on top of one. |
| Dynamic client registration left open (no admin pre-approval gate) | Any client can call `/dashboard/oauth/register` and receive a `client_id` | Matches how Claude Desktop actually behaves — it registers itself the first time an admin adds the connector, with no out-of-band step to request a `client_id` in advance. Gating this behind manual approval would break that flow; the actual security boundary is the consent screen (a human admin must still grant access), not client registration. |
| Refresh token rotation + reuse detection | Every refresh exchange invalidates the presented refresh token and issues a new one; a second attempt to use an already-rotated token revokes the whole token family | Standard OAuth 2.1 mitigation against a leaked refresh token being used silently alongside the legitimate client — without rotation, a one-time token leak (e.g. from a compromised machine) grants indefinite access until manually revoked. |
| Authorization code / PKCE storage | New minimal `oauth_auth_codes` table, hashed like every other token in this design, not an in-memory map | Consistent with `AGENTS.md`'s no-in-memory-cross-request-state rule — a code issued by one replica must be exchangeable against a different replica handling the token-exchange request. |

---

## Tips

None — see feature Tips in `references/design.md` for general guidance; nothing feature-specific to add beyond what's captured in Risks & Concerns.
