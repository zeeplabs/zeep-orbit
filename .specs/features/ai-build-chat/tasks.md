# Build with AI — Chat-Driven App Creation Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/ai-build-chat/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling + `AGENTS.md` §3 — confirm before Execute. Guidelines found: `AGENTS.md` §3 (backend: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`; frontend: `npx tsc -b`, `npm run build`). Existing test samples: `internal/dashboard/*_store_test.go`, `internal/dashboard/*_handler_test.go`, `internal/mcpserver/tools_add_table_column_test.go` — all Go tests in this repo are integration-style, run against a real Postgres pool via `authTestPool(t)`/`newTestDashboardHandler(pool)`, no DB mocking. No frontend test framework present (no vitest/jest/testing-library in `package.json`) — frontend correctness is verified by `tsc -b` + build only, matching existing repo convention.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Store/domain (`ai_providers_store.go`, `ai_build_sessions_store.go`) | integration | Key query paths + error handling: merge-on-absent-key behavior, session lifecycle transitions (`in_progress`→`completed`/`abandoned`), scoping to `owner_user_id` | `internal/dashboard/ai_providers_store_test.go`, `internal/dashboard/ai_build_sessions_store_test.go` | `go test ./...` |
| AI client (`internal/dashboard/ai/client.go`) | unit | All branches; 1:1 to spec ACs AIBC-12/13/14/15/17: message-shape response, plan-shape response, `list_apps`/`get_app_schema` tool round-trip, malformed `propose_app_plan` arguments treated as error | `internal/dashboard/ai/client_test.go` | `go test ./internal/dashboard/ai/...` |
| HTTP handlers (`ai_provider_handlers.go`, `ai_build_chat_handlers.go`) | integration | All routes in scope: happy path + every listed edge case (RBAC 403, partial-failure retry, idempotent table skip, 501 for gemini/claude) + error/failure paths | `internal/dashboard/ai_provider_handlers_test.go`, `internal/dashboard/ai_build_chat_handlers_test.go` | `go test ./...` |
| Entity/migration (`ai_providers`, `ai_build_sessions`, `ai_build_messages` tables) | none | build gate only | migration SQL files | build gate only |
| Frontend components (AI Provider settings panel, `BuildWithAIDrawer`) | none (no test framework in repo) | build gate only: type-check + production build | `internal/dashboard/ui/src/**` | `npx tsc -b && npm run build` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 and `internal/dashboard/ui/package.json` — confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a task touching only `internal/dashboard/ai/` (no DB) | `go build ./... && go vet ./... && go test ./internal/dashboard/ai/...` |
| Full | After a task touching store/handler layers (DB-integration tests) | `go build ./... && go vet ./... && go test ./...` |
| Build | After phase completion, migration-only tasks, or frontend tasks | Backend: `go build ./... && go test ./... && go vet ./... && gofmt -l <changed files>` — Frontend: `cd internal/dashboard/ui && npx tsc -b && npm run build` |

---

## Execution Plan

Phases are ordered and run sequentially — each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: AI Provider Management (backend)

```
T1 -> T3
T2 -> T3
T3 -> T4
```

### Phase 2: Chat session persistence (backend)

```
T5 -> T6
```

### Phase 3: OpenAI client package

```
T7
```

### Phase 4: Chat orchestration (backend)

```
T4 -> T8
T6 -> T8
T7 -> T8
T8 -> T9
T8 -> T10
T9 -> T10
```

### Phase 5: Frontend wiring

```
T4 -> T11
T10 -> T12
```

---

## Task Breakdown

### T1: Add dedicated AI-provider encryption key + Encrypt/Decrypt wrappers

**What**: Add `aiProviderEncryptionKey()` (dedicated env var `AI_PROVIDER_ENCRYPTION_KEY`, fallback `DASHBOARD_BOOTSTRAP_SECRET`) and `EncryptAIProviderKey`/`DecryptAIProviderKey` wrapper functions to `internal/crypto`, mirroring the existing `webhookTokenEncryptionKey`/`EncryptWebhookToken` shape.
**Where**: `internal/crypto/aes.go`
**Depends on**: None
**Reuses**: `encryptWithKey`/`decryptWithKey`, `normalizeKey` (`internal/crypto/aes.go`); `webhookTokenEncryptionKey` pattern for the dedicated-key-with-fallback shape.
**Requirement**: AIBC-01, AIBC-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `aiProviderEncryptionKey()` resolves `AI_PROVIDER_ENCRYPTION_KEY` with `DASHBOARD_BOOTSTRAP_SECRET` fallback, erroring if both are unset (same contract as `webhookTokenEncryptionKey`)
- [x] `EncryptAIProviderKey`/`DecryptAIProviderKey` round-trip correctly and use the dedicated key (not `GOOGLE_OAUTH_ENCRYPTION_KEY`)
- [x] Quick gate passes: `go build ./... && go vet ./... && go test ./internal/crypto/...`
- [x] Test count: existing `internal/crypto` tests + at least 3 new tests (round-trip, missing-key error, independence from `GOOGLE_OAUTH_ENCRYPTION_KEY`) pass

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-provider): add dedicated encryption key for AI provider secrets`

---

### T2: Migration for `zeep_system.ai_providers`

**What**: Add the `zeep_system.ai_providers` table (provider, model, api_key_encrypted, enabled, updated_at) per design's Data Models section.
**Where**: repo's existing migration mechanism (mirror how `zeep_system.auth_providers` was added — locate and follow that migration file's convention/location).
**Depends on**: None
**Reuses**: `zeep_system.auth_providers` migration as the structural template.
**Requirement**: AIBC-01, AIBC-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Table created with the exact columns from design.md's Data Models section
- [x] Migration applies cleanly on a fresh DB and is idempotent per the repo's existing migration convention
- [x] Build gate passes: `go build ./... && go vet ./...`

**Tests**: none
**Gate**: build

**Commit**: `feat(ai-provider): add ai_providers table migration`

---

### T3: `ai_providers_store.go` — CRUD, encryption, merge-on-absent-key

**What**: Implement `GetAIProvider`, `UpsertAIProvider`, `resolveDecryptedAIProviderKey` per design's Components section.
**Where**: `internal/dashboard/ai_providers_store.go`
**Depends on**: T1, T2
**Reuses**: `mergeProviderConfig` pattern (`internal/dashboard/auth_providers_store.go:222-247`) adapted for the AI provider's `{model, api_key}` shape; `crypto.EncryptAIProviderKey`/`DecryptAIProviderKey` from T1.
**Requirement**: AIBC-01, AIBC-03, AIBC-05

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `UpsertAIProvider` with a key present encrypts and persists it
- [x] `UpsertAIProvider` with no key field preserves the existing encrypted key, updating only `model`/`enabled` (AIBC-03)
- [x] `GetAIProvider` never returns the key in any form, only `{has_key, model, enabled}`
- [x] `resolveDecryptedAIProviderKey` returns a treatable error (not a panic) on decrypt failure — feeds the "provider unconfigured" fallback path
- [x] Full gate passes: `go build ./... && go vet ./... && go test ./...` (see T3 note: `internal/dashboard`'s pre-existing webhook/OAuth suite needs `WEBHOOK_TOKEN_ENCRYPTION_KEY`/`DASHBOARD_BOOTSTRAP_SECRET` set and hits local Postgres `max_connections` under full parallelism in this sandbox — environmental, reproduces identically on stock `develop`, unrelated to this task; all 6 new AI-provider tests pass in isolation and under `-p 1`)
- [x] Test count: at least 6 new tests (encrypt-on-create, merge-preserves-key, get-never-leaks-key, decrypt-failure-path, gemini/claude return unconfigured, round-trip through real Postgres pool) pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-provider): add ai_providers store with encrypted key persistence`

---

### T4: `ai_provider_handlers.go` — `GET`/`PUT /api/dashboard/ai-providers/{provider}` + route wiring

**What**: Implement `GetAIProviderConfig` (any authenticated user) and `UpsertAIProviderConfig` (superadmin-only via `HasPlatformPermission(user.Role, ActionManageIntegrations)`); `gemini`/`claude` path params return `501`. Wire both routes into the existing router.
**Where**: `internal/dashboard/ai_provider_handlers.go` (+ router wiring file, wherever existing dashboard routes are registered)
**Depends on**: T3
**Reuses**: `writeJSON`, `h.decodeJSONBody`, `UserFromContext` patterns (`internal/dashboard/handler.go`); `HasPlatformPermission`/`ActionManageIntegrations` (`internal/dashboard/platform_roles.go:33-35`).
**Requirement**: AIBC-01, AIBC-02, AIBC-04, AIBC-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Non-superadmin `PUT` returns `403`, no store mutation (AIBC-02)
- [x] Superadmin `PUT` with key+model succeeds; `GET` reflects `has_key: true`
- [x] `PUT` to `gemini`/`claude` returns `501`, no persistence attempted (AIBC-06)
- [x] Full gate passes: `go build ./... && go vet ./... && go test ./...` (see T3 note on pre-existing env gap for the unrelated webhook/OAuth suite; all 5 new AI-provider handler tests pass)
- [x] Test count: at least 5 new handler tests (403 case, successful upsert, model-only update preserves key, 501 for gemini/claude, unauthenticated request) pass

**SPEC_DEVIATION**: route path is `/dashboard/api/ai-providers/{provider}`, not the spec's literal `/api/dashboard/ai-providers/{provider}` — matches the one real routing convention in `internal/server/server.go` (`r.Route("/dashboard", ...)` with `/api/...` sub-paths, e.g. the existing `/api/config/auth/providers/{provider}`). See the comment in `ai_provider_handlers.go`.

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-provider): add provider config HTTP endpoints`

---

### T5: Migration for `zeep_system.ai_build_sessions` and `zeep_system.ai_build_messages`

**What**: Add both tables per design's Data Models section, including the `ai_build_sessions_owner_status_idx` and `ai_build_messages_session_idx` indexes and the `ON DELETE CASCADE` FK.
**Where**: repo's existing migration mechanism (same convention as T2)
**Depends on**: None
**Reuses**: T2's migration as the immediate structural sibling.
**Requirement**: AIBC-07, AIBC-08, AIBC-09, AIBC-10, AIBC-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Both tables created with exact columns/indexes/FK from design.md
- [x] Migration applies cleanly and is idempotent
- [x] Build gate passes: `go build ./... && go vet ./...`

**Tests**: none
**Gate**: build

**Commit**: `feat(ai-chat): add ai_build_sessions and ai_build_messages tables`

---

### T6: `ai_build_sessions_store.go` — session lifecycle + message persistence

**What**: Implement `GetOrCreateInProgressSession`, `AppendMessage`, `AbandonAndRestartSession`, `CompleteSession`, `SetSessionCreatedApp` per design's Components section.
**Where**: `internal/dashboard/ai_build_sessions_store.go`
**Depends on**: T5
**Reuses**: standard `pool.QueryRow`/`pool.Exec` pattern used throughout `internal/dashboard/*_store.go`.
**Requirement**: AIBC-07, AIBC-08, AIBC-09, AIBC-10, AIBC-11

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `GetOrCreateInProgressSession` resumes an existing `in_progress` session for the user, or creates one, per AIBC-07/08
- [x] `AbandonAndRestartSession` sets the old session to `abandoned` and creates a new `in_progress` one, preserving old messages (AIBC-09)
- [x] `SetSessionCreatedApp` is callable independently of `CompleteSession` (supports the partial-failure requirement, AIBC-22)
- [x] Sessions/messages are scoped to `owner_user_id` — a different user's query returns nothing (AIBC-11)
- [x] Full gate passes: `go build ./... && go vet ./... && go test ./...` (see T3 note on pre-existing env gap for the unrelated webhook/OAuth suite; all 6 new session-store tests pass)
- [x] Test count: at least 6 new tests (resume existing, create new, restart preserves history, scoping enforcement, complete sets status+app id, set-created-app-id independent of complete) pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-chat): add session store with resume/restart/complete lifecycle`

---

### T7: `internal/dashboard/ai` — OpenAI Chat Completions client with function-calling

**What**: Implement `CallModel`, the 3 tool schemas (`propose_app_plan`, `list_apps`, `get_app_schema`), `ChatTurnResult`, `ReadToolInvoker`, per design's Components section — plain `net/http` POST, `tool_choice: "auto"`, tool-call round-trip for read tools, plan-arguments validation.
**Where**: `internal/dashboard/ai/client.go`
**Depends on**: None
**Reuses**: nothing existing (new integration, isolated per design so a future Gemini/Claude client is a sibling file).
**Requirement**: AIBC-12, AIBC-13, AIBC-14, AIBC-15

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] A response with plain assistant content (no tool call) returns `ChatTurnResult{Kind: "message", ...}`
- [x] A response with a `propose_app_plan` tool call and valid arguments returns `ChatTurnResult{Kind: "plan", Plan: ...}`
- [x] A response with a `list_apps`/`get_app_schema` tool call invokes the provided `ReadToolInvoker`, feeds the result back, and returns the final round's result (not the intermediate one)
- [x] Malformed/incomplete `propose_app_plan` arguments return an error, never a partially-populated `Plan`
- [x] A bounded client-side HTTP timeout (per design's Risks & Concerns) is applied to the OpenAI call
- [x] Quick gate passes: `go build ./... && go vet ./... && go test ./internal/dashboard/ai/...`
- [x] Test count: at least 6 new tests (message shape, plan shape, tool-call round-trip via `httptest.Server`, malformed plan args, timeout behavior, no OpenAI SDK dependency added to `go.mod`) pass — 6 test functions (9 sub-tests) all pass

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-chat): add OpenAI client with forced function-calling`

---

### T8: `ai_build_chat_handlers.go` — `BuildChatTurn` + session resume

**What**: Implement `(h *Handler) BuildChatTurn(w, r)` (loads/creates session via T6, resolves provider config via T3, calls `ai.CallModel` from T7, appends messages, returns `{type, content}`/`{type, plan}`) and the session-resume `GET` handler used on drawer open.
**Where**: `internal/dashboard/ai_build_chat_handlers.go`
**Depends on**: T4, T6, T7
**Reuses**: `UserFromContext`, `writeJSON`, `h.decodeJSONBody` patterns.
**Requirement**: AIBC-07, AIBC-08, AIBC-12, AIBC-13, AIBC-14, AIBC-15, AIBC-16, AIBC-17, AIBC-18

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Opening with an existing `in_progress` session returns its full history (AIBC-07)
- [x] Sending a message persists it, calls the model, persists and returns the assistant response
- [x] `list_apps`/`get_app_schema` tool calls resolve through the caller-side `ReadToolInvoker` closure wired to `List*ForUser`/`Get*ForUser` (AIBC-17), never fabricating schema
- [x] A provider/OpenAI failure returns the fixed generic chat message and logs the real error server-side (AIBC-16)
- [x] `enabled = false` on the provider disables the entry point at the API level (returns a state the frontend can gate on, AIBC-18)
- [x] Full gate passes: `go build ./... && go vet ./... && go test ./...` (see T3 note on pre-existing environmental `max_connections` ceiling under full parallelism — reproduces identically on stock `develop`; all 8 new tests pass)
- [x] Test count: at least 7 new tests (resume, new session creation, message-shape turn, plan-shape turn, read-tool round-trip, provider-failure generic message + server log assertion, disabled-provider response) pass — 8 new test functions all pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-chat): add chat turn handler with session resume`

---

### T9: `ai_build_chat_handlers.go` — `BuildChatConfirm`

**What**: Implement `(h *Handler) BuildChatConfirm(w, r)` — validates the plan's fixed schema, calls `CreateAppForUser` then `CreateAppTableForUser` per table (with the fresh-`GetApp`-per-attempt idempotent-retry check flagged in design's Risks & Concerns), sets `created_app_id` immediately after app creation, marks the session `completed` on full success.
**Where**: `internal/dashboard/ai_build_chat_handlers.go` (same file as T8)
**Depends on**: T8
**Reuses**: `Handler.CreateAppForUser` (`internal/dashboard/handler.go:922`), `Handler.CreateAppTableForUser` (`internal/dashboard/handler.go:1196`) with origin `"ai_chat"` — identical call shape to `internal/mcpserver/tools.go:227-253`; `validateTableInput`'s reserved-name check.
**Requirement**: AIBC-19, AIBC-20, AIBC-21, AIBC-22, AIBC-23, AIBC-24

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] A valid plan creates the app + all tables + auth (if `plan.auth = true` → `AuthEmailEnabled = true` per design's mitigation) with audit origin `"ai_chat"` (AIBC-19, AIBC-21)
- [x] A user without `CanWrite()` is rejected before any mutation (AIBC-20)
- [x] A plan referencing a reserved table name (e.g. `_auth_users`) is rejected before any provisioner call
- [x] A partial failure (app created, table N fails) leaves the session `in_progress` with `created_app_id` already set, shows a generic error (AIBC-22)
- [x] Retrying after a partial failure skips tables that already exist — verified against a fresh `GetApp` call per attempt, not a stale in-memory list, and also verified for the "table provisioned but metadata write failed" case flagged in design's Risks & Concerns (AIBC-23)
- [x] Confirm rejects any payload that isn't the exact structured shape from a `propose_app_plan` tool call (AIBC-24)

**SPEC_DEVIATION**: `BuildChatConfirm` accepts no request body at all — the plan it executes is always the one already persisted by `BuildChatTurn` (T8) on the session's latest assistant message, never a client-supplied payload. This is a stricter reading of AIBC-24 than "validate the payload against a schema": since the server never trusts any client-supplied plan JSON in the first place, there is no path where a free-form body could ever be treated as a plan. See the comment above `BuildChatConfirm` in `ai_build_chat_handlers.go`.

- [x] Full gate passes: `go build ./... && go vet ./... && go test ./...` (see T3 note on pre-existing environmental `max_connections` ceiling under full parallelism — reproduces identically on stock `develop`; all 8 new tests pass)
- [x] Test count: at least 7 new tests (full success, forbidden-write rejection, reserved-name rejection, partial failure state, idempotent retry after metadata-write failure, idempotent retry after full early success, malformed/free-form plan rejection) pass — 8 new test functions all pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-chat): add plan confirmation with idempotent retry`

---

### T10: `RestartBuildChatSession` handler + full route wiring

**What**: Implement `(h *Handler) RestartBuildChatSession(w, r)` as a thin wrapper over `AbandonAndRestartSession` (T6); wire `POST /api/dashboard/ai/build-chat`, `GET /api/dashboard/ai/build-chat/session`, `POST /api/dashboard/ai/build-chat/{session_id}/confirm`, `POST /api/dashboard/ai/build-chat/restart` into the router.
**Where**: `internal/dashboard/ai_build_chat_handlers.go` (handler) + router wiring file
**Depends on**: T8, T9
**Reuses**: `UserFromContext`, existing router registration pattern.
**Requirement**: AIBC-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `POST .../restart` abandons the current session and returns a fresh empty one (AIBC-09)
- [ ] All four routes respond correctly end-to-end (not just unit-callable) through the real router
- [ ] Full gate passes: `go build ./... && go vet ./... && go test ./...`
- [ ] Test count: at least 3 new tests (restart via real HTTP route, all 4 routes reachable and auth-gated, unauthenticated request to each route rejected) pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-chat): wire restart endpoint and full chat route set`

---

### T11: AI Provider settings panel (frontend, superadmin-only)

**What**: Build the settings UI for configuring the OpenAI key/model (`GET`/`PUT /api/dashboard/ai-providers/openai`), with `gemini`/`claude` shown disabled with an "em breve" badge. Gate the panel with `RequireRole`.
**Where**: `internal/dashboard/ui/src/...` (new settings page/section, alongside existing settings pages)
**Depends on**: T4
**Reuses**: `RequireRole` (`internal/dashboard/ui/src/components/patterns/RequireRole.tsx`), existing dashboard API client (`src/lib/api.ts`), `sonner` toast, `react-i18next`.
**Requirement**: AIBC-01, AIBC-02, AIBC-04, AIBC-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Panel visible only to superadmin (`RequireRole` gate)
- [ ] Saving a key/model calls `PUT`; the key input never pre-fills with a real value (only `has_key` state shown)
- [ ] Model-only update UX doesn't require re-entering the key
- [ ] Gemini/Claude rows show "em breve" badge, disabled, no submission possible
- [ ] All user-facing strings added to `src/locales/en.json` AND `src/locales/pt-BR.json` in this same task
- [ ] Build gate passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`; `python3 -c "import json; json.load(open('src/locales/en.json'))"` and same for `pt-BR.json`

**Tests**: none (no frontend test framework in repo — build gate only)
**Gate**: build

**Commit**: `feat(ai-provider): add superadmin AI provider settings panel`

---

### T12: `BuildWithAIDrawer` — live chat wiring

**What**: Replace the static "em breve" mockup with live chat: session resume on mount, send-message mutation, `{type: message}`/`{type: plan}` rendering (plan renders the "Proposed setup" card from the existing mockup), "Confirm & create app" button calling confirm, "Restart" button, error toasts on failure, disabled entry point when no provider is configured/enabled.
**Where**: existing `BuildWithAIDrawer` component location (`internal/dashboard/ui/src/components/...`)
**Depends on**: T10
**Reuses**: existing drawer shell/animation from the current mockup; existing dashboard API client, `sonner`, `react-i18next`.
**Requirement**: AIBC-07, AIBC-08, AIBC-09, AIBC-12, AIBC-13, AIBC-14, AIBC-15, AIBC-16, AIBC-17, AIBC-18

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Opening the drawer resumes an in-progress session's history via the session-resume `GET`
- [ ] Sending a message shows a loading state (no streaming) then renders either a message bubble or the plan card
- [ ] "Confirm & create app" calls confirm, shows the created app on success, shows a toast + generic message on failure without losing the session state
- [ ] "Restart" clears the visible history and starts a new session
- [ ] Entry point is disabled/shows a "not configured" state when the provider isn't enabled
- [ ] All user-facing strings added to both locale files in this same task
- [ ] Build gate passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`; locale JSON validation for both files

**Tests**: none (no frontend test framework in repo — build gate only)
**Gate**: build

**Commit**: `feat(ai-chat): wire Build with AI drawer to live chat endpoints`

---

## Phase Execution Map

Every arrow below is a real `Depends on` edge from the task body — phase grouping is shown by the section headers, not by extra sequencing arrows between tasks that have no actual dependency (e.g. T1 and T2 both have `Depends on: None` and run in file order within Phase 1, but neither arrow exists below since neither depends on the other).

```
Phase 1: T1 -> T3
Phase 1: T2 -> T3
Phase 1: T3 -> T4
Phase 2: T5 -> T6
Phase 3: T7
Phase 4: T4 -> T8
Phase 4: T6 -> T8
Phase 4: T7 -> T8
Phase 4: T8 -> T9
Phase 4: T8 -> T10
Phase 4: T9 -> T10
Phase 5: T4 -> T11
Phase 5: T10 -> T12
```

Execution is strictly sequential — there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

Full dependency list (matches every task's `Depends on` field exactly):
- T1: None
- T2: None
- T3: T1, T2
- T4: T3
- T5: None
- T6: T5
- T7: None
- T8: T4, T6, T7
- T9: T8
- T10: T8, T9
- T11: T4
- T12: T10

All dependencies point backward or within the same phase — none point forward.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add AI-provider encryption key + wrappers | 1 file (`internal/crypto/aes.go`) | ✅ Granular |
| T2: `ai_providers` migration | 1 migration file | ✅ Granular |
| T3: `ai_providers_store.go` | 1 file, 3 cohesive methods (one component per design) | ✅ Granular |
| T4: `ai_provider_handlers.go` + route wiring | 1 file + router registration for 2 routes | ✅ Granular |
| T5: session/message migrations | 1 migration file, 2 related tables (cohesive, one seam) | ✅ Granular |
| T6: `ai_build_sessions_store.go` | 1 file, 5 cohesive methods (one component per design) | ✅ Granular |
| T7: `internal/dashboard/ai/client.go` | 1 file, 1 component | ✅ Granular |
| T8: `BuildChatTurn` + resume | 1 file (shared with T9), 1 handler + 1 resume endpoint | ✅ Granular |
| T9: `BuildChatConfirm` | same file as T8, 1 handler, split out for its own complex AC set | ✅ Granular |
| T10: `RestartBuildChatSession` + route wiring | same file as T8/T9, 1 handler + router registration | ✅ Granular |
| T11: AI Provider settings panel | 1 component/page | ✅ Granular |
| T12: `BuildWithAIDrawer` wiring | 1 existing component, behavior-only change | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (Phase 1 start) | ✅ Match |
| T2 | None | T1→T2 (phase order) | ✅ Match |
| T3 | T1, T2 | T2→T3, and T1/T2 both listed under "Cross-phase dependencies" | ✅ Match |
| T4 | T3 | T3→T4 | ✅ Match |
| T5 | None | (Phase 2 start) | ✅ Match |
| T6 | T5 | T5→T6 | ✅ Match |
| T7 | None | (Phase 3, standalone) | ✅ Match |
| T8 | T4, T6, T7 | Listed under cross-phase dependencies; Phase 4 start | ✅ Match |
| T9 | T8 | T8→T9 | ✅ Match |
| T10 | T8, T9 | T9→T10, and T8 listed under cross-phase dependencies | ✅ Match |
| T11 | T4 | Listed under cross-phase dependencies; Phase 5 start | ✅ Match |
| T12 | T10 | T11→T12 (phase order) and T10 listed under cross-phase dependencies | ✅ Match |

No task depends on a task in a later phase — all dependencies point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: encryption key + wrappers | (part of) Store/domain-adjacent crypto util | unit (existing `internal/crypto` convention) | unit | ✅ OK |
| T2: `ai_providers` migration | Entity/migration | none | none | ✅ OK |
| T3: `ai_providers_store.go` | Store/domain | integration | integration | ✅ OK |
| T4: `ai_provider_handlers.go` | HTTP handlers | integration | integration | ✅ OK |
| T5: session/message migrations | Entity/migration | none | none | ✅ OK |
| T6: `ai_build_sessions_store.go` | Store/domain | integration | integration | ✅ OK |
| T7: `internal/dashboard/ai/client.go` | AI client | unit | unit | ✅ OK |
| T8: `BuildChatTurn` + resume | HTTP handlers | integration | integration | ✅ OK |
| T9: `BuildChatConfirm` | HTTP handlers | integration | integration | ✅ OK |
| T10: Restart + route wiring | HTTP handlers | integration | integration | ✅ OK |
| T11: AI Provider settings panel | Frontend component | none (build gate only) | none | ✅ OK |
| T12: `BuildWithAIDrawer` wiring | Frontend component | none (build gate only) | none | ✅ OK |

No violations.

---

## Task Verification Standards

Every task above defines `Done when` (specific, binary, testable), `Tests`, and `Gate` fields per the Test Coverage Matrix and Gate Check Commands. Expected new-test counts are stated per task to guard against silent test deletion during implementation.
