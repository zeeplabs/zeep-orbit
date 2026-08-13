# MCP Server for Zeep Orbit Operations Specification

## Problem Statement

Zeep Orbit apps and tables are managed only through the Dashboard's REST API (`internal/dashboard/handler.go` and friends), consumed today by the Dashboard UI and by hand-written SDK calls. There is no tool-calling surface an LLM can drive directly. This blocks two things on the roadmap:

1. **AI-assisted app creation** ("Create with AI" — `handoff/README.md:23`, currently a disabled placeholder button, `CHANGELOG.md:117`) — an admin describes an app conversationally, an LLM asks clarifying questions, then creates the app/tables/RLS/policies on confirmation.
2. **External AI coding tools** (Claude Code, Cursor, Lovable — `ROADMAP.md:521`) driving Orbit directly from a developer's own agent, instead of the developer hand-writing REST calls against the SDK.

Both need the same thing: a stable, scoped, auditable set of tools an LLM can call to perform Orbit operations, without giving the LLM either raw SQL or an unscoped bearer token. This spec defines that surface as an MCP server, built once and consumed by both the internal "Create with AI" feature and any external MCP client.

## Goals

- [ ] An admin can mint a personal access token scoped to their own dashboard account and connect an MCP client (external tool, or the internal "Create with AI" chat) to Zeep Orbit's MCP server using it.
- [ ] An admin can connect an MCP client that only supports OAuth-based remote server auth (Claude Desktop's native connector flow, which has no field to paste a static bearer token) by completing an OAuth 2.1 authorization-code-with-PKCE flow against the Dashboard, without a separate account/credential system from the PAT flow above.
- [ ] Through MCP tools alone (no Dashboard UI), an admin's LLM can create an app, add one or more tables with columns, set a table's RLS mode, and create a row policy from an existing policy template (`policy-templates` feature) — the minimum path from "describe an app" to "app exists and is secured."
- [ ] Every MCP tool call executes through the exact same authorization and validation path as the equivalent Dashboard REST endpoint — the MCP server is a new transport onto existing handlers, not a parallel permission model.
- [ ] Every MCP tool call is recorded in the existing `audit_log` (who, which tool, on which app), same as the Dashboard UI's actions today.

## Out of Scope

| Feature | Reason |
|---|---|
| Destructive tools (delete app, delete table, delete policy) in V1 | An LLM-driven delete is a materially different risk than an LLM-driven create; excluding it removes an entire class of "AI deleted my production table" incident without blocking the create-focused use case that's actually driving this spec. Revisit once V1 is proven and if a concrete story needs it. |
| Raw/free-form SQL tool | Would bypass every structural safeguard this codebase already enforces (schema validation, RLS, policy structure) — same reasoning `end-user-row-policies` used to reject free-SQL policies. Tools are one-to-one with existing structured operations only. |
| Webhook, storage, deploy-provider, frontend-app, or member/RBAC tools | Not needed for the "describe an app, get an app" story that's driving V1. Same tool-registration mechanism extends to these later without a redesign. |
| External end-users (an app's own users, as opposed to dashboard admins) getting any MCP access | Out of scope by definition — MCP here is an admin/operator surface, not an app-facing API. |
| Granular per-tool RBAC (which specific tools a token can call) beyond the app-level role checks the underlying handlers already do | `rbac-per-app` (admin/editor/viewer) is not yet shipped. Token inherits whatever the underlying handler already checks today (any authenticated dashboard user with access to the app) and will automatically inherit `rbac-per-app` once that ships, same as the Dashboard UI does. |
| stdio transport | Orbit runs multiple stateless replicas behind a non-sticky load balancer (`AGENTS.md`); stdio assumes a single local process pair. Streamable HTTP is the only transport compatible with this deployment model. |
| Third-party OAuth clients beyond dynamic registration (pre-negotiated `client_id`/`client_secret` pairs, a client management UI for admins to review/revoke connected apps) | Dynamic client registration (any MCP client can self-register, per the MCP Authorization spec's expectation) covers Claude Desktop and equivalents without needing Orbit to pre-approve specific vendors. A client-management/review UI is real product surface that isn't needed to prove the flow works — tracked as a natural follow-up once real usage exists. |
| Third-party identity providers as the OAuth *authorization* server (e.g. "Sign in with Google" federating into the MCP OAuth flow) | The OAuth authorization server built here issues tokens against the admin's **existing Dashboard identity** (session login, whatever that already is — email/password or Google SSO) — it is not a second identity system, just an OAuth-shaped front door onto the same login. No separate IdP integration needed. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
|---|---|---|---|
| Auth artifact for MCP clients | New "Personal Access Token" (PAT) type, distinct from the existing per-app end-user `AppToken` (`CreateAppToken`, `internal/dashboard/handler.go:2742` — that token is for an app's own end users, unrelated). PAT is minted by a dashboard admin for themselves, tied to their `dashboard_users` identity, not to one specific app. | No existing mechanism lets a script/LLM authenticate as a dashboard admin — today's only admin auth is the `zeep_session` cookie (`internal/dashboard/middleware.go:18`), which is browser-only. A new bearer-token artifact is required regardless of MCP; MCP is just its first consumer. | n |
| PAT scope | Same effective permissions as the admin's live session (all apps they already have access to) — no separate scope-selection UI in V1 | Matches "inherits `rbac-per-app` later" default above; a scoping UI is real work that isn't needed to prove the tool-calling path | n |
| Transport | Streamable HTTP, stateless per request (PAT re-validated every call, no server-side session object) | Forced by the multi-replica constraint (see Out of Scope); also matches the existing signed-stateless-token pattern this codebase already uses for OAuth state (`internal/auth/google.go`) rather than introducing an in-memory session map | n |
| How "Create with AI" (internal chat drawer) consumes the MCP server | The chat drawer's backend acts as an MCP client itself, calling the same MCP server over HTTP using a PAT minted server-side for the admin's active dashboard session (short-lived, auto-rotated, never shown to the admin) — not a separate internal-only code path that bypasses MCP | Keeps exactly one execution path for "LLM creates an app" regardless of whether the LLM is Orbit's own chat drawer or an external Claude Code session; avoids maintaining two implementations of the same operations | n |
| Tool granularity | One MCP tool per existing REST operation (`orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`, `orbit_list_policy_templates`, `orbit_create_policy_from_template`, `orbit_list_apps`, `orbit_get_app_schema`), not a single generic "call any endpoint" tool | Generic passthrough tool would defeat the "structural safeguard" reasoning above — each tool's input schema is the LLM's only interface, so it must mirror the structured request body the handler already validates | n |
| Rate limiting | Reuse the existing per-app rate limiter pattern (`RateLimiter.MiddlewareKeyedBy`, `internal/dashboard/middleware.go:136`), keyed by PAT id instead of app id | Same mechanism, same reasoning as the inbound-webhooks fix that moved rate limiting from per-IP to per-webhook-id — avoid one noisy token starving others behind the same LB IP | n |
| Tool call failure surfaced to the LLM | Structured error object (`{error: "..."}`) mirroring the handler's existing JSON error responses, never a raw Go `err.Error()` for 500s | Same rule as `AGENTS.md` §4 for HTTP responses — MCP tool results are just another response surface, not an exception to it | n |
| OAuth vs PAT-only for V1 | Both, in V1 — a static-bearer PAT flow (Settings-generated, for clients that accept a config-file header: Claude Code, Cursor, Codex) **and** an OAuth 2.1 authorization-code-with-PKCE flow (for clients whose native UX only speaks OAuth: Claude Desktop's remote connector). OAuth-issued tokens resolve through the exact same `ResolvePAT` lookup as a manually-created PAT — OAuth is a second *issuance* path onto one token store, not a second auth system. | Claude Desktop's connector setup has no field to paste a static token — it performs OAuth metadata discovery and fails without it. Deferring OAuth to a later spec would ship an MCP server that doesn't work with one of the three named target clients from day one; the user confirmed OAuth should be in V1 once this gap was raised. | y — confirmed by user 2026-08-13 |
| Dynamic client registration | Support it (RFC 7591-style: any client can `POST` to a registration endpoint and receive a `client_id` without prior manual setup) | Claude Desktop registers itself this way — it doesn't ship with a pre-shared `client_id` for arbitrary MCP servers. Skipping this would require Orbit to hand-provision a client_id per installation, which doesn't match how the client actually behaves. | y |
| Consent screen | Reuses the existing Dashboard login — if no `zeep_session` cookie is present, `/dashboard/oauth/authorize` redirects to the normal login page first, then shows a simple "grant access" consent screen naming the requesting client | No new identity/credential system — OAuth here is a front door onto the same login every admin already has, not a parallel account | y |

**Open questions:** none blocking a first design pass — all above are proposed defaults for confirmation, not resolved facts. Flag any disagreement before `design.md`.

---

## User Stories

### P1: Admin mints a personal access token and connects an MCP client ⭐ MVP

**User Story**: As a dashboard admin, I want to generate a token tied to my own account and use it to authenticate an MCP client, so that an LLM can act as me against Zeep Orbit without me sharing my session cookie or password.

**Why P1**: Nothing else in this spec is reachable without auth and transport working first — this is the foundation every other story depends on.

**Acceptance Criteria**:

1. WHEN an admin creates a personal access token from the Dashboard (Settings → new "Personal Access Tokens" section) THEN the system SHALL generate a token shown once, never retrievable again in plaintext, stored hashed.
2. WHEN an MCP client sends a request with a valid, non-revoked PAT THEN the system SHALL resolve it to the issuing admin's `dashboard_users` identity and authorize subsequent tool calls exactly as if that admin were making the equivalent REST call.
3. IF a request carries a missing, malformed, expired, or revoked PAT THEN the system SHALL reject the MCP connection/tool call with an authentication error, without executing any tool logic.
4. WHEN an admin revokes a personal access token THEN the system SHALL reject that token on every subsequent request immediately (no propagation delay tolerated beyond normal read-after-write consistency).
5. The system SHALL record every PAT create/revoke action in the existing `audit_log`.

**Independent Test**: Create a PAT via the Dashboard, connect an MCP client with it, call the simplest read-only tool (`orbit_list_apps`) and confirm it returns the admin's own apps; revoke the PAT and confirm the same call now fails auth.

---

### P1: Admin connects Claude Desktop (or any OAuth-only client) via OAuth 2.1 ⭐ MVP

**User Story**: As a dashboard admin, I want to authorize an MCP client through the same "sign in and grant access" flow other OAuth-based integrations use, so that a client like Claude Desktop — which has no field to paste a static token — can still connect to Zeep Orbit.

**Why P1**: Equally foundational as the PAT story above — without this, an entire class of MCP client (anything that only implements the standard OAuth connector flow) cannot connect at all, regardless of how good the tool set is.

**Acceptance Criteria**:

1. WHEN an MCP client fetches Zeep Orbit's OAuth authorization server metadata THEN the system SHALL expose the standard discovery document describing its authorization, token, and registration endpoints.
2. WHEN a not-yet-known MCP client registers itself dynamically THEN the system SHALL issue it a `client_id` without requiring any manual, pre-existing setup for that specific client.
3. WHEN an MCP client redirects an admin's browser to the authorization endpoint AND the admin has no active Dashboard session THEN the system SHALL first require normal Dashboard login, then show a consent screen naming the requesting client before issuing an authorization code.
4. WHEN an admin grants consent THEN the system SHALL issue a PKCE-bound authorization code, exchangeable exactly once for an access token and a refresh token.
5. WHEN an MCP client exchanges a valid authorization code (with the matching PKCE verifier) THEN the system SHALL issue an access token that resolves through the same identity-and-permission path a manually-created PAT does — same admin, same effective permissions, same tool behavior.
6. WHEN an MCP client's access token expires THEN the system SHALL allow it to obtain a new one via the refresh token without requiring the admin to repeat the consent screen.
7. IF an authorization code is reused, expired, or its PKCE verifier doesn't match THEN the system SHALL reject the token exchange without issuing any token.

**Independent Test**: Using a real OAuth-capable MCP client (or a script driving the same HTTP calls such a client would make), complete the full discovery → dynamic registration → browser consent → code exchange flow, then call `orbit_list_apps` with the resulting access token and confirm it returns the consenting admin's own apps — with no PAT ever manually created in the Dashboard UI for this flow.

---

### P2: Admin's LLM creates an app and a table with columns through MCP tools

**User Story**: As a dashboard admin, I want my LLM to call MCP tools that create an app and add a table with columns, so that describing an app conversationally actually produces a working app.

**Why P2**: This is the concrete value the whole spec exists for — the "Create with AI" story cannot demo anything without it — but it's testable independently of the auth story once a PAT already exists.

**Acceptance Criteria**:

1. WHEN an MCP client calls `orbit_create_app` with a valid name and configuration THEN the system SHALL create the app via the same path as `POST /dashboard/api/apps` and return the created app's id and name.
2. WHEN an MCP client calls `orbit_create_table` with an app id, table name, and column definitions THEN the system SHALL create the table via the same path as `POST /dashboard/api/apps/{id}/tables`, including the same validation (duplicate names, reserved columns, type checks) as the REST endpoint.
3. IF a tool call's input fails the same validation the REST endpoint already enforces THEN the system SHALL return a structured tool error describing the specific validation failure, not a generic failure.
4. WHEN an MCP client calls `orbit_get_app_schema` for an app THEN the system SHALL return that app's current tables/columns/RLS modes, so the LLM can verify state after each step without re-deriving it from prior tool responses alone.
5. Every successful create call SHALL produce the same `audit_log` entry the REST path already produces for that operation (no new/duplicate audit code path).

**Independent Test**: Using only MCP tool calls (no Dashboard UI), create an app, add a table with at least two columns, then call `orbit_get_app_schema` and confirm the table/columns are present exactly as created.

---

### P2: Admin's LLM sets RLS mode and applies a policy template through MCP tools

**User Story**: As a dashboard admin, I want my LLM to configure a table's RLS mode and apply a named policy template, so that an AI-created table isn't left wide open or fail-closed by accident.

**Why P2**: Completes the minimum "safe app" outcome — a table created without any RLS/policy story is a real security gap, not just a missing nicety, so this can't be deferred to P3 even though it's demoable independently of the create-app story once a table already exists.

**Acceptance Criteria**:

1. WHEN an MCP client calls `orbit_set_table_rls_mode` with an app id, table name, and one of the valid RLS values (`""`, `"owner"`, `"enabled"`, `"policy"`) THEN the system SHALL apply it via the same path as the existing table-update endpoint, including its existing validation of the RLS value.
2. WHEN an MCP client calls `orbit_list_policy_templates` THEN the system SHALL return the same fixed set of named templates the Dashboard UI's Policy Templates feature exposes, with enough structure (id, description, required inputs) for an LLM to pick one without guessing free-form clause syntax.
3. WHEN an MCP client calls `orbit_create_policy_from_template` with a template id, target table, and its required inputs (e.g. role list) THEN the system SHALL create the resulting policy/policies via the same sequential `POST` calls to the existing policies endpoint the UI's template picker uses (`policy-templates` spec, P2 AC1), including its same partial-failure behavior (stop on first error, keep already-created policies).
4. IF `orbit_create_policy_from_template` is called with a template requiring inputs that are missing or invalid THEN the system SHALL return a structured error naming the missing/invalid input, without creating any policy.

**Independent Test**: On a table created via P2's app/table story, call `orbit_set_table_rls_mode` to `"policy"`, call `orbit_list_policy_templates`, then call `orbit_create_policy_from_template` for "Only the owner sees/edits" and confirm the resulting policy exists and behaves as that template's row policies do today via the UI.

---

### P3: "Create with AI" chat drawer consumes the MCP server internally

**User Story**: As a dashboard admin, I want the "Create with AI" chat drawer to actually work (not just show a "Soon" badge), driving the same MCP tools an external client would use, so that describing an app conversationally in the Dashboard itself produces a real app.

**Why P3**: Depends on P1/P2 tools already existing and working standalone; this story is "wire the existing placeholder UI to the now-real backend," not new tool surface.

**Acceptance Criteria**:

1. WHEN an admin confirms an app description in the "Create with AI" drawer THEN the backend SHALL call the same MCP tools (`orbit_create_app`, `orbit_create_table`, `orbit_set_table_rls_mode`, `orbit_create_policy_from_template`) an external MCP client would call, using a PAT minted server-side for that admin's active session.
2. The server-minted PAT used by the chat drawer SHALL be short-lived and SHALL NOT be displayed to the admin or exposed in any API response.
3. WHEN the chat drawer's underlying tool calls complete THEN the system SHALL reflect the created app in the Apps list without requiring a manual page reload (same `usePublicConfig`/shared-hook pattern used elsewhere per `AGENTS.md` §5, not an ad-hoc `useEffect` poll).
4. The "Create with AI" header button SHALL lose its "Soon" badge and disabled state once this story ships.

**Independent Test**: Use the chat drawer to describe a simple app end-to-end, confirm on the resulting app's page that the table/RLS/policy match what was described, then independently confirm (via `orbit_get_app_schema` or the Dashboard UI) that the state matches what an equivalent direct MCP tool sequence would have produced.

---

## Edge Cases

- IF a PAT's issuing admin is deactivated or deleted after the token was minted THEN the system SHALL reject that token on the next request (token validity is derived from the live admin record, not cached at mint time).
- IF an MCP tool call targets an app the PAT's admin no longer has access to (e.g. removed as a member) THEN the system SHALL reject it with the same authorization error the REST endpoint would return for that case today.
- IF two tool calls race to create a table with the same name in the same app THEN the system SHALL exhibit the same last-write-wins/conflict behavior the REST endpoint already has today — no new locking introduced for MCP specifically.
- WHEN the rate limit for a PAT is exceeded THEN the system SHALL reject further tool calls with a rate-limit error until the window resets, without disconnecting the MCP session itself.
- IF `orbit_create_policy_from_template`'s sequential policy creation fails partway (per the reused `policy-templates` behavior) THEN the tool result SHALL clearly enumerate which policies were created and which step failed, so the LLM can decide whether to retry the remainder or surface the partial state to the admin.
- IF an admin denies consent on the OAuth authorization screen THEN the system SHALL redirect back to the requesting client with a standard access-denied error, issuing no code or token.
- IF an OAuth-issued access token's underlying PAT-equivalent record is revoked (e.g. via a future "connected apps" admin action) THEN the system SHALL reject it on the next request exactly as a revoked manually-created PAT would — one revocation code path for both issuance methods.
- IF a client attempts to reuse a refresh token after it has already been exchanged once THEN the system SHALL reject the reuse and SHOULD treat it as a signal to invalidate the rest of that token family (standard refresh-token-rotation abuse detection) — exact behavior finalized in `design.md`.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| MCP-01 | P1 AC1: PAT created, shown once, stored hashed | Tasks (T1) | Verified |
| MCP-02 | P1 AC2: valid PAT resolves to admin identity, authorizes as that admin | Tasks (T1, T2) | Verified |
| MCP-03 | P1 AC3: invalid/expired/revoked PAT rejected before tool execution | Tasks (T1, T2) | Verified |
| MCP-04 | P1 AC4: revoked PAT rejected on next request | Tasks (T1) | Verified |
| MCP-05 | P1 AC5: PAT create/revoke audited | Tasks (T13) | Pending |
| MCP-19 | P1 (OAuth) AC1: authorization server metadata discoverable | Tasks (T17) | Verified |
| MCP-20 | P1 (OAuth) AC2: dynamic client registration issues a client_id with no manual setup | Tasks (T17) | Verified |
| MCP-21 | P1 (OAuth) AC3: authorize endpoint requires login then shows consent, naming the client | Tasks (T18, T19) | Implementing |
| MCP-22 | P1 (OAuth) AC4: PKCE-bound authorization code, single-use | Tasks (T18, T19, T20) | Verified |
| MCP-23 | P1 (OAuth) AC5: token exchange resolves through the same identity path as a manual PAT | Tasks (T20) | Verified |
| MCP-24 | P1 (OAuth) AC6/AC7: refresh token renewal; reused/expired/mismatched-PKCE code rejected | Tasks (T20) | Verified |
| MCP-06 | P2 (app/table) AC1: `orbit_create_app` matches `POST /dashboard/api/apps` behavior | Tasks (T4, T11) | Verified |
| MCP-07 | P2 (app/table) AC2: `orbit_create_table` matches `POST /dashboard/api/apps/{id}/tables` behavior/validation | Tasks (T5, T11) | Verified |
| MCP-08 | P2 (app/table) AC3: validation failure returns structured tool error | Tasks (T11) | Verified |
| MCP-09 | P2 (app/table) AC4: `orbit_get_app_schema` reflects current state | Tasks (T8, T10) | Verified |
| MCP-10 | P2 (app/table) AC5: tool calls produce the same audit_log entries as REST | Tasks (T4-T7, T11) | Verified |
| MCP-11 | P2 (RLS/policy) AC1: `orbit_set_table_rls_mode` matches existing validation | Tasks (T6, T11) | Verified |
| MCP-12 | P2 (RLS/policy) AC2: `orbit_list_policy_templates` matches UI's template set | Tasks (T3, T12) | Verified |
| MCP-13 | P2 (RLS/policy) AC3: `orbit_create_policy_from_template` matches UI's sequential-create + partial-failure behavior | Tasks (T7, T12) | Verified |
| MCP-14 | P2 (RLS/policy) AC4: missing/invalid template input rejected before any policy is created | Tasks (T12) | Verified |
| MCP-15 | P3 AC1: chat drawer backend calls the same tools an external client would | Design | Pending |
| MCP-16 | P3 AC2: server-minted PAT is short-lived and never exposed | Design | Pending |
| MCP-17 | P3 AC3: Apps list reflects the new app without manual reload | Design | Pending |
| MCP-18 | P3 AC4: "Create with AI" button loses its "Soon" badge once shipped | Design | Pending |

**ID format:** `MCP-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 24 total, 0 mapped to tasks yet (spec stage — `design.md`/`tasks.md` not yet written).

---

## Success Criteria

- [ ] An admin can go from "no MCP client connected" to "app created with a table, RLS mode, and a policy" using only MCP tool calls and a self-minted PAT — no Dashboard UI form involved.
- [ ] An admin can connect an OAuth-only client (Claude Desktop or equivalent) end-to-end — discovery, registration, consent, token exchange — with zero manual PAT creation.
- [ ] The same tool set works identically regardless of caller or auth method: external MCP client via PAT, external MCP client via OAuth, or the internal "Create with AI" chat drawer — one tool implementation, multiple auth front doors, all resolving to the same token store.
- [ ] Every MCP tool call is indistinguishable from its REST equivalent in the `audit_log`, and produces the same validation/authorization outcome.
- [ ] Revoking a PAT (manually created or OAuth-issued) immediately cuts off that token's access, with no in-memory state anywhere that would let one replica keep honoring a revoked token.
