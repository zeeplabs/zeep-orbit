# ai-build-chat Validation

## Validation: ai-build-chat - FAIL ❌

**Date**: 2026-08-23
**Spec**: `.specs/features/ai-build-chat/spec.md`
**Diff range**: `83f4cfb~1..2f8aaa4` (12 commits, `feat(ai-provider)`/`feat(ai-chat)`)
**Verifier**: independent sub-agent (author ≠ verifier) — no prior context on this feature, re-derived coverage from the diff and spec.md only.

---

## Task Completion

All 12 tasks (T1–T12) marked `[x]` in `tasks.md`, each with a real commit. Two documented `SPEC_DEVIATION`s (route prefix `/dashboard/api/...` instead of `/api/dashboard/...`; `BuildChatConfirm` never reads a plan from the request body) — both defensible and consistent with the codebase's actual routing convention and a stricter reading of AIBC-24.

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1–T12 | ✅ Done | All commits present in `83f4cfb..2f8aaa4`; build/vet/gofmt/test gates pass per-task per commit history |

---

## Spec-Anchored Acceptance Criteria

### P1: Configure the OpenAI provider

| AC | Spec-defined outcome | `file:line` + assertion | Result |
| -- | --------------------- | ------------------------ | ------ |
| AIBC-01 | PUT valid key+model → AES-256-GCM encrypt, persist in `zeep_system.ai_providers` | `internal/dashboard/ai_providers_store_test.go:49` `TestUpsertAIProvider_EncryptsKeyOnCreate` — asserts stored ciphertext ≠ plaintext and `DecryptAIProviderKey` recovers the original; `internal/dashboard/ai_provider_handlers_test.go:106` `TestUpsertAIProviderConfig_SuperadminSucceedsAndGetReflectsHasKey` — asserts `200` + `has_key: true` | ✅ PASS |
| AIBC-02 | Non-superadmin PUT → exact `403`, no store mutation | `internal/dashboard/ai_provider_handlers_test.go:82` `TestUpsertAIProviderConfig_NonSuperadminForbiddenNoMutation` — `w.Code != http.StatusForbidden` fatal, then asserts `GetAIProvider(...).HasKey == false` | ✅ PASS |
| AIBC-03 | Model-only update (no `api_key`) preserves stored key | `ai_providers_store_test.go:97` `TestUpsertAIProvider_ModelOnlyUpdatePreservesKey` — decrypts and compares the preserved key value, not just `has_key`; `ai_provider_handlers_test.go:146` HTTP-level equivalent | ✅ PASS |
| AIBC-04 | GET returns only `{has_key, model, enabled}`, never the key (cleartext or masked) | `ai_providers_store_test.go:140` `TestGetAIProvider_NeverLeaksKey`; `ai_provider_handlers_test.go:118,139` `bytesContain(body, "sk-real-key-abc")` asserted false on both PUT and GET response bytes | ✅ PASS |
| AIBC-05 | Dedicated `AI_PROVIDER_ENCRYPTION_KEY` env var, fallback `DASHBOARD_BOOTSTRAP_SECRET` | `internal/crypto/aes_test.go:100` `TestAIProviderKey_ErrorsLoudlyWithNoKeyConfigured`; `:113` `TestAIProviderKey_DedicatedKeyIndependentFromGoogleOAuthKey` — proves independent key space, not just a differently-named alias | ✅ PASS |
| AIBC-06 | Gemini/Claude: "em breve" badge in UI, endpoint rejects config (no persistence) | Backend: `ai_provider_handlers_test.go:176` `TestUpsertAIProviderConfig_GeminiClaudeReturn501NoPersistence` — exact `501`, `has_key` still false after. UI badge: not automated (Test Coverage Matrix declares frontend "build gate only, no test framework in repo"); confirmed by code read (`AppsPage.tsx:587` `t("apps.soon")`, gated by `useAIProviderStatus`) | ✅ PASS (backend); UI half verified by inspection only, consistent with the declared matrix |

### P2: Persisted per-app chat session

| AC | Spec-defined outcome | `file:line` + assertion | Result |
| -- | --------------------- | ------------------------ | ------ |
| AIBC-07 | Existing `in_progress` session is resumed (same ID) with full history | `ai_build_sessions_store_test.go:80` `TestGetOrCreateInProgressSession_ResumesExistingWithHistory` — asserts same ID + ordered message content; `ai_build_chat_handlers_test.go:134` HTTP-level equivalent | ✅ PASS |
| AIBC-08 | No `in_progress` session → new one created, scoped to user | `ai_build_sessions_store_test.go:59`; `ai_build_chat_handlers_test.go:109` | ✅ PASS |
| AIBC-09 | Restart → old session `abandoned` (messages preserved), new `in_progress` created | `ai_build_sessions_store_test.go:113` `TestAbandonAndRestartSession_PreservesHistoryAndCreatesFresh` — checks old status, old message count, new session ID differs; `internal/server/server_test.go:234` end-to-end through the real router | ✅ PASS |
| AIBC-10 | Confirm success → session `completed`, `created_app_id` set | `ai_build_sessions_store_test.go:190` `TestCompleteSession_And_SetSessionCreatedApp_AreIndependent`; `ai_build_chat_handlers_test.go:428` `TestBuildChatConfirm_FullSuccessRecordsAiChatOrigin` — asserts `finalSession.Status == "completed"` and `CreatedAppID == got.App.ID` | ✅ PASS |
| AIBC-11 | Sessions/messages scoped to `owner_user_id`; no cross-user read | `ai_build_sessions_store_test.go:159` `TestGetOrCreateInProgressSession_ScopedToOwner` — proves the store-level resume/create path never surfaces another user's session/messages | ⚠️ Partial — the general session/message scoping is solidly covered at the store layer, but `BuildChatConfirm`'s own owner-scoped lookup (`loadOwnedBuildChatSession`, `ai_build_chat_handlers.go:233`, `WHERE id = $1 AND owner_user_id = $2`) has no dedicated test attempting to confirm another user's session ID (IDOR-shaped check). Code inspection shows the guard exists; no test exercises it directly. |

### P3: Chat-driven plan proposal via function-calling

| AC | Spec-defined outcome | `file:line` + assertion | Result |
| -- | --------------------- | ------------------------ | ------ |
| AIBC-12 | Message appended to `ai_build_messages`; model called with full history + system prompt | `ai_build_chat_handlers_test.go:166` `TestBuildChatTurn_MessageShapeTurn` — asserts the user message is persisted (`messages[0].Content`) and the model is invoked (fake returns a canned reply that flows through) | ⚠️ Partial — persistence half is solid; **no test asserts the actual payload sent to the model** (that `history` includes the system prompt + prior turns). `withFakeAIModel`'s fake ignores its `history` argument in every test. The system-prompt-prepend logic exists in code (`ai_build_chat_handlers.go:131`) but is not independently verified. |
| AIBC-13 | Model lacking info → `{type: "message"}` returned + persisted | `ai_build_chat_handlers_test.go:166`; `internal/dashboard/ai/client_test.go:46` `TestCallModel_MessageShapeWhenNoToolCall` | ✅ PASS |
| AIBC-14 | Model ready → forced `propose_app_plan` call → `{type: "plan"}` returned + plan JSON persisted on the message row | `ai_build_chat_handlers_test.go:208` `TestBuildChatTurn_PlanShapeTurn` — unmarshal-checks persisted `messages[1].Plan`; `client_test.go:75` `TestCallModel_PlanShapeOnValidProposeAppPlanCall`; `ai_build_sessions_store_test.go:240` `TestAppendMessage_PersistsPlanJSON` | ✅ PASS |
| AIBC-15 | Model referencing an existing app uses read-only tools backed by `List*ForUser`/`Get*ForUser`; never fabricates | `client_test.go:130` `TestCallModel_ReadToolRoundTripReturnsFinalResult` (client-level round-trip mechanics); `ai_build_chat_handlers_test.go:333` `TestBuildChatReadToolInvoker_ListAppsAndGetAppSchema` — invokes the real closure against a real created app, and asserts an unknown app name returns an error payload rather than a fabricated schema | ✅ PASS |
| AIBC-16 | OpenAI call fails → fixed generic message shown; real error logged server-side | `ai_build_chat_handlers_test.go:252` `TestBuildChatTurn_ModelFailureReturnsGenericMessage` — asserts exact `genericAIChatError` content and that the injected secret string never appears in the response body | ⚠️ Partial — the "never leak the real error to the client" half is solidly proven. The "**logs the real error server-side**" half is **not independently verified**: `aiBuildChatHandlerTestPool` constructs `NewHandler(pool, registry.New(), zap.NewNop())` — a no-op logger — so no test can observe whether `h.logger.Error(...)` (`ai_build_chat_handlers.go:141`) actually ran. Code inspection confirms the call exists; no assertion targets it. Matches spec.md's own traceability table, which still lists AIBC-16 as `Design | Pending` (never updated to `Verified`/`Implementing`) — the spec's own bookkeeping flags this gap. |
| AIBC-17 | No table/app mutation results from a chat message alone — only explicit confirm mutates | No direct test asserts "zero apps/tables exist after a plan-shape `BuildChatTurn`". Structurally guaranteed: `BuildChatTurn` (`ai_build_chat_handlers.go:92-166`) contains no call to `CreateAppForUser`/`CreateAppTableForUser` — only `BuildChatConfirm` does. | ❌ GAP — evidence-or-zero: no test cites this outcome directly, even though the code path makes it true by construction. A future refactor that accidentally wires a mutation into `BuildChatTurn` would not be caught by any existing test. |
| AIBC-18 | No provider / `enabled=false` → disable "Build with AI" entry point in UI rather than starting a session | Backend surface tested indirectly: `ai_build_chat_handlers_test.go:311` `TestBuildChatTurn_DisabledProviderReturnsGenericMessage` proves `BuildChatTurn` itself degrades gracefully when disabled — but that is AIBC-16's contract, not "the entry point is disabled". The actual AC (frontend gating via `GET .../ai-providers/openai`'s `enabled` field, `AppsPage.tsx:366-367` `useAIProviderStatus`) has no automated test — declared out of scope by the Test Coverage Matrix ("frontend: no test framework, build gate only"). | ⚠️ Spec-precision gap — acceptable per the matrix's own declared scope, but the AC as literally written ("disable the entry point") is verified only by code read (`useAIProviderStatus`, `AppsPage.tsx:366`), never by a test. |

### P4: Confirm plan → real app creation

| AC | Spec-defined outcome | `file:line` + assertion | Result |
| -- | --------------------- | ------------------------ | ------ |
| AIBC-19 | Confirm validates plan, calls `CreateAppForUser` → `CreateAppTableForUser`×N → auth config, as the authenticated user | `ai_build_chat_handlers_test.go:428` `TestBuildChatConfirm_FullSuccessRecordsAiChatOrigin` — asserts `App.Name`, `App.AuthEmailEnabled == true`, `len(App.Tables) == 2` | ✅ PASS |
| AIBC-20 | User without `CanWrite()` rejected before any mutation, exact `403` | `ai_build_chat_handlers_test.go:772` `TestBuildChatConfirm_RevokedWritePermissionForbidden` — `w.Code != http.StatusForbidden` fatal, session confirmed still `in_progress` afterward | ✅ PASS |
| AIBC-21 | Full success → audit origin `"ai_chat"`, session `completed`, `created_app_id` set | `ai_build_chat_handlers_test.go:475-483` — queries `zeep_system.audit_log` directly and asserts `ip_address == "ai_chat"` (the field the origin is threaded through) | ✅ PASS |
| AIBC-22 | Partial failure (app created, table N fails) → session stays `in_progress`, `created_app_id` already set, generic error shown, no rollback | `ai_build_chat_handlers_test.go:554` `TestBuildChatConfirm_PartialFailureLeavesSessionInProgress` — asserts `Status == "in_progress"`, `CreatedAppID` set, table 1 remains (no rollback) | ⚠️ Spec-precision gap — the "generic error shown" half does not match literally: `respondBuildChatConfirmError` (`ai_build_chat_handlers.go:384-399`) returns the **specific** `ValidationError` message for a validation-class failure (the test itself asserts `400` with the literal validation error, not `genericAIChatError`). Only default/internal errors get the fixed generic string. This is a deliberate, more informative choice consistent with design.md's Error Handling table ("Plan table name collides → chat shows a validation error"), but it is a real divergence from spec.md's literal "surface a generic error in the chat" wording for the partial-failure case, and it is undocumented as a `SPEC_DEVIATION`. |
| AIBC-23 | Retry after partial failure skips existing tables idempotently, including the provisioned-but-metadata-missing case from design.md's Risks | `ai_build_chat_handlers_test.go:606` `TestBuildChatConfirm_RetryAfterPartialFailureSkipsExistingTable`; `:668` `TestBuildChatConfirm_RetryAfterMetadataWriteFailureSelfHeals` (exactly the design-flagged edge case); `:727` `TestBuildChatConfirm_AllTablesAlreadyExistStillCompletes` | ✅ PASS — exceeds the stated minimum, covers the specific risk design.md called out |
| AIBC-24 | Confirm never accepts a free-form/client-supplied plan payload — only the structured `propose_app_plan` shape | `ai_build_chat_handlers_test.go:491` `TestBuildChatConfirm_IgnoresRequestBodyNoStoredPlanRejected` — sends a garbage body with a fabricated app name, asserts `400` and that no app named `"totally-not-proposed"` was ever created | ✅ PASS — satisfied via the documented `SPEC_DEVIATION` (server never reads a plan from the request body at all, a strictly stronger guarantee than schema-validating a client payload) |

**Status**: ❌ Gaps present (1 hard gap: AIBC-17; 3 partial/spec-precision gaps: AIBC-11, AIBC-12, AIBC-16, AIBC-18, AIBC-22 — six annotated rows total, none of them a functional defect, all of them missing or imprecise test evidence for a specific sub-clause)

---

## Discrimination Sensor

Isolated scratch: `git worktree add /tmp/ai-build-chat-sensor-scratch HEAD` (never `git stash`). Baseline `git status --porcelain` on the real worktree was empty before the sensor ran and confirmed empty again after `git worktree remove --force` — the real tree was never mutated.

| # | File:line | Description | Killed? |
| - | --------- | ------------ | ------- |
| 1 | `internal/dashboard/ai_build_chat_handlers.go:343` | Inverted idempotent-retry check: `if appTableRowExists(...)` → `if !appTableRowExists(...)` (retry skip logic reversed, per design.md's flagged risk) | ✅ Killed — `TestBuildChatConfirm_FullSuccessRecordsAiChatOrigin` and `TestBuildChatConfirm_RetryAfterPartialFailureSkipsExistingTable` both failed |
| 2 | `internal/dashboard/ai_build_chat_handlers.go:317` | Flipped `AuthEmailEnabled: plan.Auth` → `AuthEmailEnabled: !plan.Auth` | ✅ Killed — `TestBuildChatConfirm_FullSuccessRecordsAiChatOrigin` failed on the `AuthEmailEnabled` assertion |
| 3 | `internal/dashboard/ai_build_chat_handlers.go:323` | Removed the `SetSessionCreatedApp` call before the per-table loop (AIBC-22's core guarantee) | ✅ Killed — `TestBuildChatConfirm_PartialFailureLeavesSessionInProgress` failed on the `CreatedAppID` assertion |
| 4 | `internal/dashboard/ai_provider_handlers.go:75` | Removed the `HasPlatformPermission` check in `UpsertAIProviderConfig` (`if !HasPlatformPermission(...)` → `if false`) | ✅ Killed — `TestUpsertAIProviderConfig_NonSuperadminForbiddenNoMutation` failed (got `200`/mutation instead of `403`) |
| 5 | `internal/dashboard/ai_provider_handlers.go` (`GetAIProviderConfig`) | Made the handler return the real decrypted key alongside `has_key` | ✅ Killed — `TestUpsertAIProviderConfig_SuperadminSucceedsAndGetReflectsHasKey` failed (`bytesContain(getW.Body.Bytes(), "sk-real-key-abc")` tripped) |

**Sensor depth**: lightweight (5 targeted mutations, exceeding the default 1–3 minimum given this feature touches auth-gating and secret-handling paths)
**Sensor outcome**: 5 of 5 mutations killed (discrimination sensor itself is clean — the FAIL verdict below comes from the spec-anchored coverage gaps, not from the sensor)

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| No features beyond what was asked | ✅ |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for the tasks | ✅ |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ (mirrors `auth_providers_store.go`, `webhookTokenEncryptionKey`, `mcpserver/tools.go` call shapes throughout) |
| Tests map to acceptance criteria, non-shallow | ✅ (spot-checked P4 in full — every assertion targets state/value, not just call-occurred) |
| Spec-anchored outcome check | ⚠️ 6 of 24 rows flagged (see table above) — mostly precision/evidence gaps on secondary clauses, not incorrect behavior |
| Per-layer coverage (domain 1:1 AC; routes happy+edge+error) | ✅ — P4 in particular exceeds the stated minimum (idempotent-retry has 3 dedicated tests including the design-flagged metadata-write-failure case) |
| Every test maps to a spec AC / Done-when — no unclaimed tests | ✅ |
| Documented guidelines followed | `AGENTS.md` §3 (build/test/vet/gofmt gates), §4 (English-only API errors, no raw `err.Error()` in 500s — confirmed in `respondBuildChatConfirmError`'s default branch), §5 (i18n in both locale files, `sonner` toast wiring, confirmed by grep) |

---

## Edge Cases (spec.md)

- [x] Two tabs, same session: last-write-wins is the natural consequence of the store's read-then-append pattern; not separately tested but not a distinct code path either.
- [x] Permission revoked mid-flow → `TestBuildChatConfirm_RevokedWritePermissionForbidden`.
- [x] Reserved table name (`_auth_users`) → `TestBuildChatConfirm_ReservedTableNameRejectedBeforeAnyMutation`.
- [x] Resuming a `completed` session never happens → `findInProgressSession`'s `WHERE status = 'in_progress'` filter structurally excludes `completed`/`abandoned` rows; covered indirectly by every resume test (none ever resume a completed session).
- [x] Decrypt failure treated as unconfigured → `TestResolveDecryptedAIProviderKey_DecryptFailureReturnsError`.

---

## Gate Check

- **Gate command**: `go build ./... && go test ./... && go vet ./... && gofmt -l <changed files>` (backend); `cd internal/dashboard/ui && npx tsc -b && npm run build` (frontend)
- **Build**: clean (`go build ./...` — no output, exit 0)
- **Vet**: clean (`go vet ./...` — no output, exit 0)
- **gofmt**: clean on all 15 changed/new Go files (no output)
- **Frontend**: `npx tsc -b` clean; `npm run build` succeeded (492 modules, no errors); `en.json`/`pt-BR.json` both valid JSON
- **Full `go test ./... -p 1`**: environmental failure reproduced — `internal/dashboard`'s full suite hits Postgres `max_connections` ("sorry, too many clients already", SQLSTATE 53300) under the package's full test volume, confirmed **unrelated to this feature** by re-running with `-parallel 1` (same cascade of connection-refused failures across pre-existing, unrelated tests: webhooks, OAuth, RBAC, etc.) and by running every ai-build-chat-scoped test file in isolation
- **Feature-scoped isolation run** (all 34 new/modified tests across the 5 test files in scope): **34/34 pass**
  - `internal/crypto` (AIBC-01/05 subset): 3/3 pass
  - `internal/dashboard` — `ai_providers_store_test.go`: 6/6 pass
  - `internal/dashboard` — `ai_provider_handlers_test.go`: 5/5 pass
  - `internal/dashboard` — `ai_build_sessions_store_test.go`: 6/6 pass
  - `internal/dashboard` — `ai_build_chat_handlers_test.go`: 16/16 pass
  - `internal/dashboard/ai` — `client_test.go`: 6/6 pass (9 sub-tests)
  - `internal/server` — `server_test.go` (BuildChat-scoped): 3/3 pass
- **Test count before feature**: N/A (all files are new to this feature; no baseline to regress against)
- **Test count after feature**: 34 new test functions across 6 files (per tasks.md's own per-task counts, verified present and passing)
- **Skipped tests**: none in scope
- **Failures**: none in scope; the `internal/dashboard` full-package failures are the pre-existing environmental ceiling documented repeatedly in `tasks.md` (T3/T4/T6/T8/T9/T10 notes) — reproduced and confirmed identical in nature (connection-refused, not assertion failures) on this exact worktree

---

## Fix Plans

### Fix 1: AIBC-17 has no test evidence that a chat message alone never mutates

- **Root cause**: every `BuildChatTurn` test asserts response shape and persistence, but none assert `ListApps`/table-count invariants before/after a plan-shape turn.
- **Fix task**: add an assertion to `TestBuildChatTurn_PlanShapeTurn` (or a new test) that `ListApps(ctx, pool, user)` returns no app named after the plan immediately after a plan-shape `BuildChatTurn` response, proving no mutation occurred.
- **Priority**: Minor (structurally guaranteed today by code-path separation; this closes a regression-detection blind spot, not a live bug).

### Fix 2: AIBC-16's "log the real error server-side" clause is unverified

- **Root cause**: test handler pool wires `zap.NewNop()`, which discards everything — no observation point exists.
- **Fix task**: swap in a `zaptest`/observed-logger in `TestBuildChatTurn_ModelFailureReturnsGenericMessage` and assert an `Error` (or `Warn`, for the unconfigured-provider path) entry was recorded containing the session ID, without asserting on the secret content itself.
- **Priority**: Minor (the leak-prevention half is solid; only the diagnosability half is unverified).

### Fix 3: AIBC-22's "generic error" wording vs. the specific validation message returned

- **Root cause**: `respondBuildChatConfirmError` intentionally surfaces the precise `ValidationError` message for validation-class partial failures, which is more useful to the user but diverges from spec.md's literal "generic error" phrasing for this AC.
- **Fix task**: either (a) add a `SPEC_DEVIATION` note next to `respondBuildChatConfirmError` documenting that validation-class errors are deliberately more specific than the "generic error" wording implies (matching design.md's own Error Handling table), or (b) if the literal spec wording is load-bearing, route validation-class partial failures through `genericAIChatError` too. Given design.md already reconciles this nuance, (a) — documenting the deviation — is the lower-risk fix.
- **Priority**: Minor (documentation gap, not a behavior bug — design.md already establishes the intended behavior).

### Fix 4: AIBC-11's IDOR-shaped gap at the confirm-handler level

- **Root cause**: no test attempts to confirm a session ID that belongs to a different user.
- **Fix task**: add `TestBuildChatConfirm_AnotherUsersSessionReturnsNotFound` — create session as user A, attempt confirm as user B, assert `404` (matching `loadOwnedBuildChatSession`'s `ErrNotFound` mapping) and that no mutation occurred.
- **Priority**: Minor (the guard clause exists and is structurally sound; only test evidence is missing).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | ---------------- | ----------- |
| AIBC-01 through AIBC-05, AIBC-06 | Implementing | ✅ Verified |
| AIBC-07 through AIBC-10 | Implementing | ✅ Verified |
| AIBC-11 | Implementing | ⚠️ Verified with gap (Fix 4) |
| AIBC-12 | Implementing | ⚠️ Verified with gap (payload-content assertion missing) |
| AIBC-13, AIBC-14, AIBC-15 | Implementing | ✅ Verified |
| AIBC-16 | Design/Pending (never updated) | ⚠️ Verified with gap (Fix 2) |
| AIBC-17 | Implementing | ❌ Needs Fix (Fix 1) |
| AIBC-18 | Design/Pending (never updated) | ⚠️ Verified with gap (frontend-only per matrix) |
| AIBC-19, AIBC-20, AIBC-21 | Design/Pending (never updated) | ✅ Verified |
| AIBC-22 | Design/Pending (never updated) | ⚠️ Verified with gap (Fix 3) |
| AIBC-23, AIBC-24 | Design/Pending (never updated) | ✅ Verified |

Note: spec.md's own traceability table was never updated past "Implementing"/"Pending" during implementation (a documentation-sync gap independent of test coverage) — flagging so it gets corrected alongside the fixes above.

---

## Summary

**Overall**: ⚠️ Issues — no functional defects found, gate is clean, discrimination sensor kills 5/5 targeted mutations on the highest-risk logic (idempotent retry, auth mapping, partial-failure bookkeeping, permission gate, key-leak surface). The gaps are all missing-or-imprecise **test evidence** for specific sub-clauses of otherwise-implemented behavior, not incorrect behavior. Given the evidence-or-zero rule, this nets to **FAIL** on strict spec-anchored coverage (1 hard gap: AIBC-17; 5 partial/precision gaps), while the implementation itself is sound.

**Spec-anchored check**: 18/24 ACs fully matched; 6/24 flagged (1 hard gap, 5 partial/spec-precision)
**Sensor**: 5/5 mutations killed
**Gate**: 34/34 feature-scoped tests pass; full-suite run confirms a pre-existing, documented, unrelated environmental ceiling (Postgres `max_connections`), not a regression

**What works**: Provider config CRUD with encryption/merge-on-absent-key/never-leak-key; session lifecycle (resume/create/restart/complete) with owner scoping; OpenAI client with tool-calling round-trip and malformed-plan rejection; full confirm flow including the design-flagged provisioned-but-metadata-missing retry case; RBAC gating on both provider config and confirm; audit-origin tagging.

**Issues found**: See Fix Plans 1–4 above — all Minor priority, all additive test-coverage work, none requiring a behavior change except the documentation/wording reconciliation in Fix 3.

**Next steps**: Route Fixes 1–4 as fix tasks to an implementer; re-verify. None block shipping the feature as-is, but should be closed before calling AIBC-16/17/22's traceability status "Verified" without qualification.
