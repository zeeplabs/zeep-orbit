# Build with AI — Chat-Driven App Creation Specification

## Problem Statement

Creating an app in zeep-orbit today requires a user to already know the shape of their schema — tables, columns, whether they need auth — and fill that in through dashboard forms. The "Build with AI" entry point already exists in the UI (badge "em breve") promising a chat where a user describes their app in plain language and the system sets up the backend. No provider integration, no chat backend, and no execution path exist yet. This spec covers the chat itself and the minimal AI-provider configuration needed to power it with OpenAI.

## Goals

- [ ] A user with app-create permission can describe a new app in natural language and get a structured, reviewable plan (tables, columns, auth) before anything is created.
- [ ] A superadmin can configure one global OpenAI API key + model once; every user in the instance can then use the chat with zero configuration of their own.
- [ ] Confirming a plan creates the real app/tables/auth through the exact same code path (`*ForUser` handlers) that the REST API and MCP mutation tools already use — no parallel creation logic.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Editing an existing app via chat (add table/column to an app that already exists) | MVP covers new-app creation only; existing-app editing is a distinct interaction model (which app, which table) that needs its own spec. |
| RAG / semantic memory across chat sessions | No concrete use case beyond "reference an existing app," which is already served by read-only function-calling tools reusing `List*ForUser`/`Get*ForUser`. Revisit only if a use case emerges that structured lookups can't cover. |
| Streaming (SSE/WebSocket) chat responses | No streaming precedent in this codebase; request/response with a loading state is sufficient for MVP and avoids introducing new infra. |
| Structured/editable plan form | Plan revisions happen by sending a new chat message; no separate form UI for editing plan fields directly. |
| Gemini and Claude providers — functional | Selectable in the provider UI with an "em breve" badge, disabled, no persistence or call path implemented. |
| Per-workspace/per-org AI provider configuration | Instance has one global provider config; no multi-tenant scoping. |
| New RBAC permission for *using* the chat | Chat access reuses the existing app-create permission check; only *configuring* the provider is a new superadmin-gated permission. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here — nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Confirmation before mutation | Every proposed plan requires explicit user confirmation ("Confirm & create app") before any real `CreateAppForUser`/`CreateAppTableForUser` call runs. | User-decided; no auto-execution path in MVP regardless of model confidence. | y |
| Plan revision path | Plan changes only via new chat message (LLM regenerates plan); no structured form-editing UI. | User-decided; keeps MVP surface small. | y |
| MVP creation scope | New-app creation only; no chat-driven editing of an existing app. | User-decided. | y |
| Chat persistence model | One session per app being built, persisted server-side (`ai_build_sessions` + `ai_build_messages`); reopening the drawer resumes the `in_progress` session; user can explicitly abandon and restart without finishing a build. | User-decided: losing in-progress work on drawer close is bad UX; but forcing completion before starting over is also bad UX, hence explicit restart. | y |
| Cross-session memory | No RAG/embeddings; LLM gets existing-app awareness via read-only function-calling tools (mirroring `orbit_list_apps`/`orbit_get_app_schema`), not vector search. | User-decided after cost/benefit discussion — RAG needs new infra (vector store, reindexing pipeline) with no concrete use case that structured lookups don't already cover. | y |
| Streaming | No token streaming in MVP; single request/response per chat turn. | User-decided; no SSE/WebSocket precedent in the codebase, avoids new infra for MVP. | y |
| LLM failure handling | Generic error message shown in chat ("couldn't generate a plan right now, try again"); real provider error (auth failure, quota, timeout) is logged server-side only, never surfaced to non-superadmin users. | User-decided; avoids leaking provider-specific error detail to end users. | y |
| API key scope | Single global provider configuration for the whole instance (no per-org/workspace key). | User-decided; matches the instance's current single-tenant-per-deployment model. | y |
| Who can use the chat | Same authorization as the existing "New app" action (app-create permission) — no new permission gate for *usage*. | User-decided; chat is an alternate on-ramp to an action users can already perform. | y |
| Who can configure the provider | Superadmin only (`platformPerms` gate), mirroring existing global-settings patterns. | User-decided; API key is a shared, instance-wide secret. | y |
| Encryption approach | Reuse `internal/crypto` AES-256-GCM (same primitive as `auth_providers_store.go`), with a new dedicated `AI_PROVIDER_ENCRYPTION_KEY` env var (fallback `DASHBOARD_BOOTSTRAP_SECRET`), independently rotatable — mirrors the existing `WEBHOOK_TOKEN_ENCRYPTION_KEY` pattern. | Established codebase convention; avoids introducing a new crypto primitive or a shared rotation blast radius across unrelated secrets. | y |
| Partial update of provider config | Reuse the `mergeProviderConfig` merge-on-absent-key pattern: updating the model without resending the key preserves the existing key. | Established codebase convention (`auth_providers_store.go`); per `AGENTS.md`, any mergeable config surface must follow this pattern. | y |
| Key exposure on read | `GET` on the provider config never returns the key, in cleartext or masked — only `{has_key: bool, model}`. | User-decided; minimizes secret exposure surface even to the superadmin's own browser session after the initial write. | y |
| Function-calling shape | The model is forced to call a `propose_app_plan(name, tables[], auth)` tool when it has enough information; every backend response to the frontend is exactly one of `{type: "message", content}` or `{type: "plan", plan: {...}}` — never free-form text treated as a schema. | User-decided; prevents free-text LLM output from ever reaching mutation code, closing a prompt-injection-into-schema risk. | y |
| Partial-failure recovery | If plan confirmation creates the app but fails partway through table creation, the session stays `in_progress`, `created_app_id` is already set to what succeeded, a generic error is shown, and retry must be idempotent (re-creating an existing table is a handled error, not a duplicate). | User-decided; avoids orphaned apps with no path to finish or clean up. | y |
| Audit trail origin | Mutations triggered from chat record origin `"ai_chat"` (distinct from `"mcp"` and the plain REST origin) for traceability. | Matches the existing pattern where MCP tools already tag their origin distinctly from REST. | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Configure the OpenAI provider ⭐ MVP

**User Story**: As a superadmin, I want to set an OpenAI API key and model once for the whole instance, so that every user can use "Build with AI" without configuring anything themselves.

**Why P1**: The chat cannot function without a configured provider; this is the enabling capability.

**Acceptance Criteria**:

1. WHEN a superadmin submits a valid OpenAI API key and model via `PUT /api/dashboard/ai-providers/openai` THEN the system SHALL encrypt the key with AES-256-GCM and persist it in `zeep_system.ai_providers`.
2. IF a non-superadmin user calls `PUT /api/dashboard/ai-providers/openai` THEN the system SHALL respond with `403 Forbidden` and SHALL NOT modify any stored provider config.
3. WHEN a superadmin submits a model update without an API key field THEN the system SHALL preserve the previously stored encrypted key (merge-on-absent-key), updating only the model.
4. WHEN any authenticated user calls `GET /api/dashboard/ai-providers/openai` THEN the system SHALL respond with only `{has_key: bool, model: string, enabled: bool}` and SHALL NOT include the API key in any form (cleartext or masked).
5. The system SHALL store the OpenAI encryption key material under a dedicated `AI_PROVIDER_ENCRYPTION_KEY` environment variable, falling back to `DASHBOARD_BOOTSTRAP_SECRET` when unset.
6. WHERE the Gemini or Claude provider option is selected in the UI THEN the system SHALL display an "em breve" badge and SHALL NOT accept a configuration submission for that provider (endpoint returns `501 Not Implemented` or equivalent, no persistence).

**Independent Test**: As superadmin, PUT a key+model, confirm 200 and `has_key: true` on GET; PUT a model-only update, confirm the app can still call OpenAI successfully (key retained); attempt the same PUT as a non-superadmin and confirm 403.

---

### P2: Persisted per-app chat session ⭐ MVP

**User Story**: As a user building a new app, I want my in-progress "Build with AI" conversation to survive closing the drawer or reloading the page, so I don't lose my place.

**Why P2**: Named P2 for ordering only — this is required for the P1-adjacent chat experience to be usable at all, but depends on no provider-specific logic, so it's built and tested as its own vertical slice before wiring in the LLM call.

**Acceptance Criteria**:

1. WHEN a user opens the "Build with AI" drawer and has an existing session with `status = in_progress` THEN the system SHALL reload that session's message history instead of starting a new one.
2. WHEN a user opens the "Build with AI" drawer and has no `in_progress` session THEN the system SHALL create a new `ai_build_sessions` row scoped to that user.
3. WHEN a user clicks "Restart" on an `in_progress` session THEN the system SHALL set that session's status to `abandoned` and SHALL create a new `in_progress` session, discarding no data (the abandoned session's messages remain in storage).
4. WHEN a plan is confirmed and the resulting app is created successfully THEN the system SHALL set the session's `status` to `completed` and SHALL set `created_app_id` to the new app's ID.
5. The system SHALL scope every session and its messages to the `owner_user_id` that created it; a user SHALL NOT be able to read or resume another user's session.

**Independent Test**: Open drawer, send a message, close drawer, reopen — same history shown. Click Restart — history clears to a fresh greeting, old session still exists in storage with `status = abandoned`.

---

### P3: Chat-driven plan proposal via function-calling ⭐ MVP

**User Story**: As a user, I want to describe my app in plain language and have the assistant ask clarifying questions and then propose a concrete plan (tables, columns, auth) that I can review before anything is created.

**Why P3**: This is the core value proposition of the feature; depends on P1 (provider configured) and P2 (session to persist into).

**Acceptance Criteria**:

1. WHEN a user sends a chat message THEN the system SHALL append it to the session's `ai_build_messages` and SHALL call the configured OpenAI model with the full session history plus a system prompt.
2. WHILE the model determines it lacks enough information to propose a plan THEN the system SHALL return `{type: "message", content: "..."}` to the frontend and SHALL persist that assistant message.
3. WHEN the model has enough information THEN the system SHALL force a call to the `propose_app_plan(name, tables[], auth)` tool and SHALL return `{type: "plan", plan: {...}}` to the frontend, persisting the plan JSON on the corresponding message row.
4. IF the model wants to reference an existing app (e.g., "similar to my ticket app") THEN the system SHALL make available read-only function-calling tools equivalent to `orbit_list_apps`/`orbit_get_app_schema`, backed by the same `List*ForUser`/`Get*ForUser` handlers used elsewhere, and SHALL NOT fabricate schema details for apps it has not looked up.
5. IF the OpenAI call fails (auth error, quota, timeout, network) THEN the system SHALL return a generic chat-visible error message and SHALL log the real error server-side with enough detail for a superadmin to diagnose it.
6. The system SHALL NOT execute any table/app mutation as a result of a chat message alone — only an explicit confirm action (P4) executes mutations.
7. WHILE no AI provider is configured or `enabled = false` THEN the system SHALL disable the "Build with AI" entry point in the UI (or show a "not configured" state) rather than allowing a chat session to start.

**Independent Test**: Send "I want a ticketing app," receive a clarifying question about auth; answer; receive a `{type: "plan", ...}` response matching the described intent. Disconnect the configured key (invalid), retry, confirm a generic error is shown and the real error appears in server logs.

---

### P4: Confirm plan → real app creation ⭐ MVP

**User Story**: As a user, once I approve the proposed plan, I want the app, tables, and auth to actually be created — through the same trusted path as the dashboard's manual create flow.

**Why P4**: This is the payoff step; depends on P3 producing a valid plan.

**Acceptance Criteria**:

1. WHEN a user confirms a plan via `POST /api/dashboard/ai/build-chat/{session_id}/confirm` THEN the system SHALL validate the plan against its fixed schema and SHALL call `CreateAppForUser`, then `CreateAppTableForUser` per table, then configure auth if `plan.auth = true`, using the same authenticated `user` as the dashboard session.
2. IF the authenticated user lacks write permission (`CanWrite()` false) THEN the system SHALL reject the confirm request before any mutation runs, identically to the existing REST/MCP behavior.
3. WHEN all steps in a confirm succeed THEN the system SHALL record the mutation's audit-log origin as `"ai_chat"` and SHALL mark the session `completed` with `created_app_id` set.
4. IF app creation succeeds but a subsequent table creation fails THEN the system SHALL leave the session `in_progress` with `created_app_id` already set to the created app, SHALL surface a generic error in the chat, and SHALL NOT roll back the already-created app.
5. WHEN a user retries confirmation after a partial failure THEN the system SHALL skip creation for any table that already exists on that app (idempotent retry) rather than erroring as a duplicate-creation failure or creating it twice.
6. The system SHALL NOT accept a free-form/unstructured plan payload in the confirm request — only the structured shape produced by the `propose_app_plan` tool call in P3.

**Independent Test**: Confirm a 2-table plan, verify both tables exist and audit log shows `origin = ai_chat`. Simulate a failure on the second table (e.g., duplicate name), confirm session stays `in_progress`, retry, confirm success without a duplicate-table error and without a duplicate app being created.

---

## Edge Cases

- IF a user has two browser tabs open with the same in-progress session THEN the system SHALL treat both as reading/writing the same session state (no per-tab session forking); last write wins on message order.
- IF a user's app-create permission is revoked between opening the chat and confirming a plan THEN the system SHALL reject the confirm request per P4-AC2, leaving the session `in_progress`.
- IF the proposed plan contains a table name that collides with a system-reserved name (e.g., `_auth_users`) THEN the system SHALL reject the confirm request with a validation error surfaced in the chat, without calling `CreateAppTableForUser`.
- WHEN a user opens the drawer and their most recent session is `completed` THEN the system SHALL start a fresh session (per P2-AC2), never resuming a `completed` one.
- IF the encrypted API key fails to decrypt (e.g., encryption key rotated without migrating stored ciphertext) THEN the system SHALL treat the provider as unconfigured for chat purposes and SHALL log the decryption failure for a superadmin to investigate.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AIBC-01 | P1: Configure the OpenAI provider | Tasks | Implementing |
| AIBC-02 | P1: Configure the OpenAI provider | Design | Pending |
| AIBC-03 | P1: Configure the OpenAI provider | Tasks | Implementing |
| AIBC-04 | P1: Configure the OpenAI provider | Tasks | Implementing |
| AIBC-05 | P1: Configure the OpenAI provider | Tasks | Implementing |
| AIBC-06 | P1: Configure the OpenAI provider | Design | Pending |
| AIBC-07 | P2: Persisted per-app chat session | Design | Pending |
| AIBC-08 | P2: Persisted per-app chat session | Design | Pending |
| AIBC-09 | P2: Persisted per-app chat session | Design | Pending |
| AIBC-10 | P2: Persisted per-app chat session | Design | Pending |
| AIBC-11 | P2: Persisted per-app chat session | Design | Pending |
| AIBC-12 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-13 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-14 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-15 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-16 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-17 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-18 | P3: Chat-driven plan proposal via function-calling | Design | Pending |
| AIBC-19 | P4: Confirm plan → real app creation | Design | Pending |
| AIBC-20 | P4: Confirm plan → real app creation | Design | Pending |
| AIBC-21 | P4: Confirm plan → real app creation | Design | Pending |
| AIBC-22 | P4: Confirm plan → real app creation | Design | Pending |
| AIBC-23 | P4: Confirm plan → real app creation | Design | Pending |
| AIBC-24 | P4: Confirm plan → real app creation | Design | Pending |

**ID format:** `AIBC-[NUMBER]` (AI Build Chat)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 24 total, 0 mapped to tasks, 24 unmapped ⚠️ (Design phase not yet run)

---

## Success Criteria

- [ ] A superadmin can configure OpenAI once and every other user can complete a full describe → plan → confirm → app-created flow with zero personal configuration.
- [ ] Zero mutation occurs from a chat message that was not explicitly confirmed by the user.
- [ ] Confirmed plans produce apps/tables indistinguishable (schema-wise, audit-wise except origin tag) from ones created via the existing dashboard form or MCP tools.
- [ ] A partial-failure confirm can always be retried to a successful completion without manual cleanup or duplicate resources.
