# AI Edit Chat Tasks

## Execution Protocol (MANDATORY — do not skip)

Implement these tasks with the `tlc-spec-driven` skill: activate it by name and follow its Execute flow and Critical Rules. Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user — do not proceed without it.**

---

**Design**: `.specs/features/ai-edit-chat/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate: `go build`, `go test`, `go vet`, `gofmt -l`; frontend gate: `npx tsc -b`, `npm run build`; i18n JSON validated via `python3 -c "import json; json.load(...)"`). No frontend test framework exists in this repo (confirmed: no `*.test.*` files under `internal/dashboard/ui/src`) — same accepted gap already logged for `ai-build-chat` (AIBC-18).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Provisioner DDL (entity/config) | none | build gate only | `internal/dashboard/provisioner.go` | `go build ./...` |
| Store layer (`ai_build_sessions_store.go`, `handler.go` `UpdateAppForUser`) | unit | key paths (create/reuse edit session, RBAC deny, idempotent confirm) + error handling | `internal/dashboard/*_test.go` | `go test ./internal/dashboard/...` |
| AI client (`ai/client.go` edit tool defs, `EditOperation` parsing) | unit | all branches; 1:1 to spec ACs AIEC-02, AIEC-07/08/09, AIEC-11/12 | `internal/dashboard/ai/*_test.go` | `go test ./internal/dashboard/ai/...` |
| Chat handlers (`ai_edit_chat_handlers.go`) | unit | all branches; 1:1 to spec ACs AIEC-01..06, AIEC-14..18; every listed edge case | `internal/dashboard/ai_edit_chat_handlers_test.go` | `go test ./internal/dashboard/...` |
| MCP tool (`orbit_update_app`) | unit | happy path + RBAC deny + validation error, matching sibling tools' test depth | `internal/mcpserver/tools_update_app_test.go` | `go test ./internal/mcpserver/...` |
| REST route wiring (`server.go`) | integration | routes reachable, RBAC enforced before handler runs | `internal/server/server_test.go` | `go test ./internal/server/...` |
| Frontend component (`EditWithAIDrawer.tsx`, `AppDetailsPage.tsx` button) | none | build gate only (no test framework in this repo, matches accepted AIBC-18 gap) | `internal/dashboard/ui/src/**` | `npx tsc -b && npm run build` |
| i18n JSON (`en.json`, `pt-BR.json`) | none | valid JSON only | `internal/dashboard/ui/src/locales/*.json` | `python3 -c "import json; json.load(open('en.json'))"` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a backend task with unit tests only | `go build ./... && go test ./internal/dashboard/... ./internal/mcpserver/... && go vet ./... && gofmt -l <changed files>` |
| Full | After the REST route-wiring task (integration) | Quick, plus `go test ./internal/server/...` |
| Build | After phase completion, or frontend/config-only tasks | Full, plus `npx tsc -b && npm run build` (from `internal/dashboard/ui`) and the i18n JSON validation for any touched locale file |

---

## Execution Plan

### Phase 1: Data model + core backend wiring

```
T1 -> T2
T1 -> T3 -> T4
```

### Phase 2: AI client — edit tool schemas

```
T5
```

### Phase 3: Edit chat handlers (core loop)

```
T2 -> T6 -> T7
T5 -> T7
T7 -> T8
T2 -> T9
```

### Phase 4: Routes

```
T3 -> T10
T4 -> T10
T8 -> T10
T9 -> T10
```

### Phase 5: Frontend

```
T10 -> T11 -> T12
```

---

## Task Breakdown

### T1: Add `mode`/`target_app_id` columns and edit-session unique index to provisioner DDL

**What**: Append the idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` statements for `mode`/`target_app_id` on `zeep_system.ai_build_sessions`, plus the partial unique index enforcing one `in_progress` edit session per `(owner_user_id, target_app_id)`, to the `stmts` slice in `provisioner.go`.
**Where**: `internal/dashboard/provisioner.go`
**Depends on**: None
**Reuses**: The existing `family_id`/`dashboard_pats` idempotent-ALTER pattern (`provisioner.go:567-568`) and the original `ai_build_sessions` DDL block (`provisioner.go:587-596`)
**Requirement**: AIEC-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Statements appended, each idempotent (`IF NOT EXISTS`)
- [ ] Comment explains the new columns' purpose, matching the file's existing comment style
- [ ] `go build ./...` passes

**Tests**: none
**Gate**: build

---

### T2: Extend `ai_build_sessions_store.go` for edit-mode session lifecycle

**What**: Add `GetOrCreateInProgressEditSession(ctx, ownerUserID, appID) (*Session, error)` (scoped by `mode='edit'`, `target_app_id=appID`, creates with `target_app_id` populated immediately if none `in_progress`), and confirm `AppendMessage`/`AbandonAndRestartSession` work unchanged for edit-mode rows (add a `mode` param only where the existing signature needs to distinguish create vs edit scoping).
**Where**: `internal/dashboard/ai_build_sessions_store.go`
**Depends on**: T1
**Reuses**: `GetOrCreateInProgressSession`, `AppendMessage`, `AbandonAndRestartSession` (existing implementations, extended not replaced)
**Requirement**: AIEC-01, AIEC-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GetOrCreateInProgressEditSession` returns the existing `in_progress` row for `(ownerUserID, appID)` if present, else creates one with `target_app_id` set at creation (AIEC-01)
- [ ] A create-mode session for the same user and an edit-mode session for a different/same app coexist without collision (AIEC-17)
- [ ] Unit tests: create-new, reuse-existing, and coexistence-with-create-session cases
- [ ] Gate passes: `go build ./... && go test ./internal/dashboard/... && go vet ./... && gofmt -l internal/dashboard/ai_build_sessions_store.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add edit-mode session lifecycle to ai_build_sessions_store`

---

### T3: Add `UpdateAppForUser` handler

**What**: New handler `UpdateAppForUser(ctx, user, appID, authEmailEnabled, origin, ip) (*App, error)` — checks `CanWrite()` for the app, calls the existing store `UpdateApp`, writes an audit log entry with the given `origin`, following the exact shape of `AddTableColumnForUser`/`AddTableIndexForUser`.
**Where**: `internal/dashboard/handler.go`
**Depends on**: T1
**Reuses**: Store `UpdateApp` (`apps_store.go`), the RBAC/audit pattern from `AddTableColumnForUser` (`handler.go:1455`)
**Requirement**: AIEC-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `CanWrite()` denial returns an authorization error before the store is touched
- [ ] Success path updates `auth_email_enabled` and writes an audit entry with the passed `origin`
- [ ] Unit tests: success, RBAC-denied, store-error paths
- [ ] Gate passes: `go build ./... && go test ./internal/dashboard/... && go vet ./... && gofmt -l internal/dashboard/handler.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add UpdateAppForUser handler`

---

### T4: Add `orbit_update_app` MCP tool

**What**: Register a new MCP tool `orbit_update_app` calling `UpdateAppForUser`, following the registration pattern of `orbit_add_table_column`/`orbit_add_table_index`.
**Where**: `internal/mcpserver/tools.go`
**Depends on**: T3
**Reuses**: `orbit_add_table_column`/`orbit_add_table_index` registration pattern (`tools.go:312-340`)
**Requirement**: AIEC-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Tool registered with input schema for `app_id`/`auth_email_enabled`
- [ ] Calls `UpdateAppForUser` with MCP origin, matching the audit-parity pattern of sibling tools
- [ ] Unit tests in `internal/mcpserver/tools_update_app_test.go`: happy path, RBAC-denied
- [ ] Gate passes: `go build ./... && go test ./internal/mcpserver/... && go vet ./... && gofmt -l internal/mcpserver/tools.go internal/mcpserver/tools_update_app_test.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add orbit_update_app MCP tool`

---

### T5: Add edit-mode tool schemas and `EditOperation` type to `ai/client.go`

**What**: Add `editToolDefs()` (6 `propose_*` JSON schemas: `propose_add_table`, `propose_add_column`, `propose_add_index`, `propose_add_reference`, `propose_set_rls_mode`, `propose_toggle_auth`), the `EditOperation`/`PlanColumnOp`/`PlanIndexOp`/`PlanReferenceOp`/`PlanRLSOp`/`PlanAuthOp` types, and response parsing that populates `ChatTurnResult.EditOp` when the model calls one of these tools.
**Where**: `internal/dashboard/ai/client.go`
**Depends on**: None
**Reuses**: Existing `toolDefs()`/`AppPlan`/`PlanTable`/`PlanColumn` shapes and the existing `CallModel` request/response plumbing
**Requirement**: AIEC-02, AIEC-07, AIEC-08, AIEC-09, AIEC-11, AIEC-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All 6 tool schemas defined with their required fields per design's `EditOperation` shape
- [ ] `CallModel` (or a parallel edit-mode entry point) returns a populated `EditOp` with exactly one non-nil sub-field matching `Kind`, for each of the 6 tool calls
- [ ] Unit tests: one per `Kind`, asserting the parsed `EditOperation` shape from a mocked tool-call response
- [ ] Gate passes: `go build ./... && go test ./internal/dashboard/ai/... && go vet ./... && gofmt -l internal/dashboard/ai/client.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add edit-mode tool schemas and EditOperation type`

---

### T6: Add `editChatSystemPrompt`

**What**: New constant `editChatSystemPrompt` — same structural constraints (column types, naming rules, off-topic guard) as `buildChatSystemPrompt`, adapted: the app already exists, the model must read its current schema via `get_app_schema` rather than proposing from scratch, and must decline any FK request targeting a column that already exists (per spec AIEC-10).
**Where**: `internal/dashboard/ai_edit_chat_handlers.go` (new file)
**Depends on**: T2
**Reuses**: `buildChatSystemPrompt`'s structural-constraints text (`ai_build_chat_handlers.go`)
**Requirement**: AIEC-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Prompt text includes the same type/naming rules and off-topic guard as the creation prompt
- [ ] Prompt explicitly instructs reading `get_app_schema` before proposing any operation on an existing table
- [ ] Prompt explicitly instructs declining FK-on-existing-column requests
- [ ] `go build ./...` passes (no behavior yet to unit-test beyond compiling — covered by T7's turn tests)

**Tests**: none (content-only constant; exercised by T7's tests)
**Gate**: build

---

### T7: `EditChatTurn` handler

**What**: `EditChatTurn(ctx, user, appID, message) (*ChatTurnResponse, error)` — loads/creates the edit session via T2's store method, injects `editChatSystemPrompt` + current app schema (via the existing read-only schema lookup, same as `ai-build-chat`'s `list_apps`/`get_app_schema` tool invoker) + message history, calls the model with `editToolDefs()`, persists the resulting message (with `EditOp` in `plan_json` when present).
**Where**: `internal/dashboard/ai_edit_chat_handlers.go`
**Depends on**: T5, T6
**Reuses**: `buildChatReadToolInvoker` pattern, `AppendMessage`
**Requirement**: AIEC-01, AIEC-02, AIEC-07, AIEC-08, AIEC-09, AIEC-11, AIEC-12, AIEC-15, AIEC-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Clarifying-question turn persists a plain message with no `plan_json`
- [ ] Each of the 6 tool-call shapes results in a message with the correct `EditOp` persisted
- [ ] Off-topic input is declined per the ported guard, matching the creation flow's existing off-topic test pattern
- [ ] Model/network failure surfaces the same generic error used by `ai-build-chat`, session stays `in_progress`
- [ ] At least one test asserts the actual `tools` array sent to the mocked model call includes the edit-mode schemas (closes lesson `L-026` from `ai-build-chat`, applied proactively here)
- [ ] Gate passes: `go build ./... && go test ./internal/dashboard/... && go vet ./... && gofmt -l internal/dashboard/ai_edit_chat_handlers.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add EditChatTurn handler`

---

### T8: `EditChatConfirm` handler

**What**: `EditChatConfirm(ctx, user, sessionID) (*EditChatConfirmResponse, error)` — loads the session's last persisted `EditOperation`, switches on `Kind` to call exactly one of `AddTableColumnForUser`/`AddTableIndexForUser`/`CreateAppTableForUser`/`UpdateTableRLSModeForUser`/`UpdateAppForUser` with audit origin `ai_chat`, appends a result message, session stays `in_progress`; re-derives from the last persisted operation on retry so a duplicate confirm call is a no-op rather than a duplicate mutation.
**Where**: `internal/dashboard/ai_edit_chat_handlers.go`
**Depends on**: T7
**Reuses**: The owner-scoped IDOR guard pattern from `loadOwnedBuildChatSession`, all 5 target handlers unchanged
**Requirement**: AIEC-03, AIEC-04, AIEC-05, AIEC-06, AIEC-07, AIEC-08, AIEC-09, AIEC-11, AIEC-12, AIEC-14, AIEC-16, AIEC-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Each of the 6 `Kind` values dispatches to the correct handler with correctly-mapped fields
- [ ] Handler validation errors (duplicate column, bad identifier, disallowed type, invalid reference) surface verbatim in the chat response, session stays `in_progress`, app unmodified (AIEC-04)
- [ ] RBAC denial (`CanWrite()` false) returns an authorization error before any handler runs, for every `Kind` (AIEC-05)
- [ ] Every applied mutation is audit-logged with origin `ai_chat` (AIEC-06)
- [ ] Session belonging to another user returns not-found with no mutation (IDOR guard, mirrors `TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation`)
- [ ] Double-confirm on an already-applied operation is a no-op, not a duplicate mutation (AIEC-16)
- [ ] Gate passes: `go build ./... && go test ./internal/dashboard/... && go vet ./... && gofmt -l internal/dashboard/ai_edit_chat_handlers.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add EditChatConfirm handler`

---

### T9: `GetEditChatSession` / `RestartEditChatSession` handlers

**What**: `GetEditChatSession(ctx, user, appID)` (returns the current `in_progress` edit session and its messages, or a fresh empty one) and `RestartEditChatSession(ctx, user, appID)` (marks the current edit session `abandoned`, creates a new one) — same semantics as the creation flow's restart, scoped by `(user, appID, mode=edit)`.
**Where**: `internal/dashboard/ai_edit_chat_handlers.go`
**Depends on**: T2
**Reuses**: `AbandonAndRestartSession`, `GetBuildChatSession`'s response-shaping pattern
**Requirement**: AIEC-01, AIEC-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Reopening the drawer for an app with an `in_progress` edit session reloads its messages (AIEC-01)
- [ ] Restart marks the current session `abandoned` and creates a new one without requiring a pending operation to be resolved first
- [ ] Unit tests for both paths
- [ ] Gate passes: `go build ./... && go test ./internal/dashboard/... && go vet ./... && gofmt -l internal/dashboard/ai_edit_chat_handlers.go`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ai-edit-chat): add GetEditChatSession and RestartEditChatSession handlers`

---

### T10: Wire edit-chat REST routes

**What**: Register `POST /dashboard/api/apps/{appId}/ai/edit-chat`, `POST /dashboard/api/apps/{appId}/ai/edit-chat/{sessionId}/confirm`, `GET /dashboard/api/apps/{appId}/ai/edit-chat`, `POST /dashboard/api/apps/{appId}/ai/edit-chat/restart` in the router, each requiring the same auth middleware as the rest of the app-scoped API surface.
**Where**: `internal/server/server.go`
**Depends on**: T3, T4, T8, T9
**Reuses**: Existing app-scoped route registration pattern (same middleware chain as `AddTableColumnForUser`'s REST/MCP-parity routes)
**Requirement**: AIEC-01, AIEC-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All 4 routes reachable and return the expected handler's response shape
- [ ] Unauthenticated request rejected before any handler runs
- [ ] Integration tests in `internal/server/server_test.go` covering route reachability + auth rejection
- [ ] Gate passes: `go build ./... && go test ./internal/server/... && go vet ./... && gofmt -l internal/server/server.go`

**Tests**: integration
**Gate**: full

**Commit**: `feat(ai-edit-chat): wire edit-chat REST routes`

---

### T11: `EditWithAIDrawer.tsx` component + API client + i18n

**What**: New drawer component showing one-operation-at-a-time proposals (not the "Proposed setup" batch card), calling the 4 new endpoints via new functions in `src/lib/api.ts`; add all new user-facing strings to `en.json` and `pt-BR.json` in the same change.
**Where**: `internal/dashboard/ui/src/components/patterns/EditWithAIDrawer.tsx`, `internal/dashboard/ui/src/lib/api.ts`, `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json`
**Depends on**: T10
**Reuses**: `BuildWithAIDrawer`'s `Textarea`/drawer primitives and `sonner` toast-on-error pattern (kept as a separate component per design)

**Requirement**: AIEC-01, AIEC-03, AIEC-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Drawer shows chat history, a single pending-operation confirmation affordance, and a "Recomeçar" action
- [ ] Every new string present in both `en.json` and `pt-BR.json`
- [ ] Mutation errors toast via `onError` (`sonner`)
- [ ] `npx tsc -b && npm run build` pass
- [ ] i18n JSON validated: `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json'))"` and same for `pt-BR.json`

**Tests**: none (no frontend test framework in this repo)
**Gate**: build

**Commit**: `feat(ai-edit-chat): add EditWithAIDrawer component`

---

### T12: Wire "Edit with AI" button into `AppDetailsPage.tsx`

**What**: Add the entry-point button, gated on the same write-permission signal the manual edit form already checks, opening `EditWithAIDrawer` scoped to the current app.
**Where**: `internal/dashboard/ui/src/pages/AppDetailsPage.tsx`
**Depends on**: T11
**Reuses**: Existing permission-check pattern already gating the manual edit form on this page

**Requirement**: AIEC-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Button hidden when the current user lacks write access to the app (AIEC-05)
- [ ] Button opens `EditWithAIDrawer` scoped to the current `app_id`
- [ ] `npx tsc -b && npm run build` pass

**Tests**: none (no frontend test framework in this repo)
**Gate**: build

**Commit**: `feat(ai-edit-chat): add Edit with AI entry point to AppDetailsPage`

---

## Phase Execution Map

```
Phase 1: T1 -> T2
         T1 -> T3 -> T4

Phase 2: T5

Phase 3: T2 -> T6 -> T7
         T5 -> T7
         T7 -> T8
         T2 -> T9

Phase 4: T3 -> T10
         T4 -> T10
         T8 -> T10
         T9 -> T10

Phase 5: T10 -> T11 -> T12
```

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: DDL columns + index | 1 file, additive statements | ✅ Granular |
| T2: Store edit-session lifecycle | 1 file, 1 new method + minor param additions | ✅ Granular |
| T3: `UpdateAppForUser` | 1 function, 1 file | ✅ Granular |
| T4: `orbit_update_app` MCP tool | 1 tool registration, 1 file | ✅ Granular |
| T5: Edit tool schemas + `EditOperation` | 1 file, cohesive type+schema set (6 tightly related schemas, same shape) | ✅ Granular |
| T6: `editChatSystemPrompt` | 1 constant, 1 file | ✅ Granular |
| T7: `EditChatTurn` | 1 function, 1 file | ✅ Granular |
| T8: `EditChatConfirm` | 1 function, 1 file | ✅ Granular |
| T9: `GetEditChatSession`/`RestartEditChatSession` | 2 tightly related functions, same file, same session-fetch pattern | ✅ Granular |
| T10: Route wiring | 4 routes, 1 file, 1 concept (registration) | ✅ Granular |
| T11: Drawer + API client + i18n | 1 component + its direct API/i18n dependencies (same change, per AGENTS.md §5) | ✅ Granular |
| T12: Entry-point button | 1 file, 1 button | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1 -> T2 | ✅ Match |
| T3 | T1 | T1 -> T3 | ✅ Match |
| T4 | T3 | T3 -> T4 | ✅ Match |
| T5 | None | None | ✅ Match |
| T6 | T2 | T2 -> T6 | ✅ Match |
| T7 | T5, T6 | T5 -> T7, T6 -> T7 | ✅ Match |
| T8 | T7 | T7 -> T8 | ✅ Match |
| T9 | T2 | T2 -> T9 | ✅ Match |
| T10 | T3, T4, T8, T9 | T3 -> T10, T4 -> T10, T8 -> T10, T9 -> T10 | ✅ Match |
| T11 | T10 | T10 -> T11 | ✅ Match |
| T12 | T11 | T11 -> T12 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Provisioner DDL | none | none | ✅ OK |
| T2 | Store layer | unit | unit | ✅ OK |
| T3 | Store/handler layer | unit | unit | ✅ OK |
| T4 | MCP tool | unit | unit | ✅ OK |
| T5 | AI client | unit | unit | ✅ OK |
| T6 | Chat handlers (constant only) | unit (exercised by T7) | none, deferred to T7 by design (content-only constant, no branching logic of its own) | ✅ OK |
| T7 | Chat handlers | unit | unit | ✅ OK |
| T8 | Chat handlers | unit | unit | ✅ OK |
| T9 | Chat handlers | unit | unit | ✅ OK |
| T10 | REST route wiring | integration | integration | ✅ OK |
| T11 | Frontend component | none | none | ✅ OK |
| T12 | Frontend component | none | none | ✅ OK |

