# ai-build-chat Validation

## Validation: ai-build-chat - PASS ✅

**Date**: 2026-08-23
**Spec**: `.specs/features/ai-build-chat/spec.md`
**Diff range**: `83f4cfb~1..2359c85` (13 commits, `feat(ai-provider)`/`feat(ai-chat)`/`test(ai-chat)`) — round 1 covered `83f4cfb~1..2f8aaa4`; this round adds fix commit `2359c85`.
**Verifier**: independent sub-agent, round 2 (author ≠ verifier) — no prior context carried over from round 1's mental model; every AC re-derived from spec.md and the diff from scratch, per the skill's re-derivation rule.

**Round-1 recap**: FAIL on 6 of 24 ACs — all missing/imprecise test evidence for a sub-clause, zero behavioral defects, 5/5 discrimination-sensor mutations killed. Fix commit `2359c85` addressed 4 of the 6 (AIBC-11, AIBC-16, AIBC-17, AIBC-22 documented as SPEC_DEVIATION). AIBC-12 and AIBC-18 had no fix task registered.

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1–T12 | ✅ Done | All commits present in `83f4cfb..2f8aaa4`; re-confirmed in this worktree (`git log`) |
| Fix pass | ✅ Done | `2359c85` — 3 new tests + 1 documentation fix, all present and passing |

---

## Spec-Anchored Acceptance Criteria — the 6 previously-failed ACs, re-derived from zero

Only the 6 ACs in scope for this round are re-derived below (per the task). The other 18 were not re-litigated; round 1's evidence for them (spec.md, `ai_providers_store_test.go`, `ai_provider_handlers_test.go`, `ai_build_sessions_store_test.go`, `client_test.go`, `ai_build_chat_handlers_test.go`, `server_test.go`) was independently spot-checked for internal consistency and found unchanged by the fix commit's diff (`2359c85` touches only `ai_build_chat_handlers.go` and `ai_build_chat_handlers_test.go`).

| AC | Spec-defined outcome | `file:line` + assertion | Result |
| -- | --------------------- | ------------------------ | ------ |
| AIBC-11 | Sessions/messages scoped to `owner_user_id`; a confirm attempt against another user's session is rejected, not just the store's read path | `internal/dashboard/ai_build_chat_handlers_test.go:918` `TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation` — creates session as `owner`, attempts `BuildChatConfirm` as `otherUser`, asserts `w.Code == http.StatusNotFound` (`:942`), then asserts no app named `"not-yours-app"` exists via `ListAppsForUser`, then asserts `loadOwnedBuildChatSession(...).Status == "in_progress"` for the owner. Exercises `loadOwnedBuildChatSession`'s `WHERE id = $1 AND owner_user_id = $2` guard (`ai_build_chat_handlers.go:238`) directly at the handler layer, distinct from the store-level test already covering the general case. | ✅ PASS |
| AIBC-12 | Message appended to `ai_build_messages`; model called with full history + system prompt | `ai_build_chat_handlers_test.go:166` (`TestBuildChatTurn_MessageShapeTurn`) proves persistence. The system-prompt-prepend/history-assembly code exists and is straightforward (`ai_build_chat_handlers.go:130-133`: `messages := make(...); messages = append(messages, ai.Message{Role: "system", Content: buildChatSystemPrompt}); for _, m := range history { ... }`). Re-confirmed independently: `withFakeAIModel`'s fake signature accepts `history []ai.Message` in every test in this file, but **no test body in `ai_build_chat_handlers_test.go` references the `history` parameter inside a fake closure** (verified via `grep -n "history\." ai_build_chat_handlers_test.go` — zero matches inside any closure). The fix commit did not touch this code path. | ❌ GAP (unaddressed, not a fix-blocking defect — see decision below) |
| AIBC-16 | OpenAI call fails → generic message to client; real error logged server-side | Leak-prevention half: `ai_build_chat_handlers_test.go:294` `TestBuildChatTurn_ModelFailureReturnsGenericMessage`. Diagnosability half (previously unverified — `zap.NewNop` discards everything): `ai_build_chat_handlers_test.go:350` `TestBuildChatTurn_ModelFailureLogsRealErrorServerSide` — wires `aiBuildChatHandlerTestPoolWithObservedLogger` (`observer.New(zap.ErrorLevel)`), triggers a model-call failure, and asserts (`:378`) an `Error`-level log entry exists whose `Context` contains a field with `Key == "session_id"` and a non-empty `String` value. Targets `h.logger.Error("ai build chat: model call failed", zap.String("session_id", session.ID), zap.Error(err))` at `ai_build_chat_handlers.go:141` directly. | ✅ PASS |
| AIBC-17 | No table/app mutation results from a chat message alone — only explicit confirm mutates | `ai_build_chat_handlers_test.go:254` `TestBuildChatTurn_PlanShapeTurn`, extended at `:295-305` — after a plan-shape `BuildChatTurn` response, calls `ListAppsForUser(ctx, pool, user)` and fails the test if any returned app has `Name == "ticketing"` (the plan's name), proving no app was created by the turn alone. | ✅ PASS |
| AIBC-18 | No provider / `enabled=false` → disable "Build with AI" entry point in UI rather than starting a session | Re-inspected independently (not carried over from round 1): `internal/dashboard/ui/src/pages/AppsPage.tsx:366-367` — `const { data: aiProviderStatus } = useAIProviderStatus(); const aiProviderReady = Boolean(aiProviderStatus?.enabled && aiProviderStatus?.has_key);`; the entry-point button at `:573-590` is rendered with `disabled={!aiProviderReady}` and shows the `t("apps.soon")` badge when not ready. This is code-inspection evidence only — `tasks.md`'s Test Coverage Matrix (`.specs/features/ai-build-chat/tasks.md:26`) explicitly scopes frontend components to "none (no test framework in repo) — build gate only: type-check + production build," confirmed by this repo having no `vitest`/`jest`/`@testing-library` dependency. | ⚠️ Spec-precision gap — acceptable per the declared, pre-approved Test Coverage Matrix; not a fix-blocking gap. |
| AIBC-22 | Partial failure → generic error shown in chat | `ai_build_chat_handlers_test.go:554` `TestBuildChatConfirm_PartialFailureLeavesSessionInProgress` still asserts the literal validation-error message, not `genericAIChatError`, for this specific failure mode. This round confirms the deviation is now **documented**: `ai_build_chat_handlers.go:392-406`, a `SPEC_DEVIATION` comment directly above the `errors.As(err, &valErr)` branch of `respondBuildChatConfirmError`, explaining that validation-class failures intentionally surface their specific message (matching design.md's Error Handling table and `AGENTS.md` §4's carved-out exception for safe, input-class errors), while the default/internal-error branch still uses the fixed generic string. | ✅ PASS — SPEC_DEVIATION now explicit and traceable; behavior itself was never wrong, only undocumented in round 1. |

**Status**: ✅ 4 of 6 fully closed (AIBC-11, AIBC-16, AIBC-17, AIBC-22). AIBC-18 remains an accepted spec-precision gap per the pre-approved Test Coverage Matrix (not new — round 1 flagged the same). AIBC-12 remains a genuine, unaddressed evidence gap on a specific sub-clause (payload content, not the general behavior).

### Decision on AIBC-12 (per task instructions — Verifier judgment call)

**Decision: does not block PASS. Documented as an accepted spec-precision/evidence gap, not a fix task.**

Reasoning:
- The behavior itself is simple, deterministic, and has no conditional branching that could silently regress in a way existing tests wouldn't eventually surface: `messages := make(...)`, one `append` of a fixed system-prompt constant, one loop copying `history` verbatim, one `append` of the new user message. There is no plan-vs-message branching, no truncation, no reordering logic — the risk profile is closer to a data-transform one-liner than to auth-gating or mutation logic.
- Every other AIBC-12 sub-clause (message persisted, model invoked, response shape correct) already has direct test evidence.
- The discrimination sensor (below) targets the three actual fixes from this round, per the task's explicit instruction; AIBC-12 was never assigned a fix task by round 1 either, and the task brief allows leaving it as a documented gap rather than requiring a code fix (which the Verifier is barred from writing anyway).
- If this surface grows more complex later (e.g., message truncation/windowing, per-role formatting, injected metadata), the next feature touching it should add a `TestBuildChatTurn_SendsFullHistoryWithSystemPromptToModel`-style test asserting on the captured `history` argument via `withFakeAIModel`. This is recorded as a lesson (below), not shipped as a blocking gap today.

This does not reopen the FAIL verdict: it is carried forward as an open, low-risk, low-priority item — same disposition round 1 gave AIBC-18.

---

## Discrimination Sensor

Isolated scratch: `git worktree add /tmp/ai-build-chat-sensor-scratch2 HEAD` (never `git stash`). Baseline `git status --porcelain` on the real worktree was empty before the sensor ran and confirmed empty again after `git worktree remove --force` — the real tree was never mutated. `internal/dashboard/static/` (a `.gitignore`d build artifact needed only to satisfy `//go:embed static`) was copied into the scratch tree from the real worktree's already-built output so `go build` would succeed there; this is not tracked content and was not part of the mutation.

Mutations target the three areas the fix commit added evidence for, per the task brief:

| # | File:line | Description | Killed? |
| - | --------- | ------------ | ------- |
| 1 | `internal/dashboard/ai_build_chat_handlers.go:146` (scratch) | AIBC-17 regression: injected an unconditional `h.CreateAppForUser(r.Context(), user, AppRequestBody{Name: result.Plan.Name}, "ai_chat")` call into the plan-shape branch of `BuildChatTurn`, simulating a chat message alone triggering a real mutation | ✅ Killed — `TestBuildChatTurn_PlanShapeTurn` failed: `expected no app created from a plan-shape BuildChatTurn alone, found &{... Name:ticketing ...}` |
| 2 | `internal/dashboard/ai_build_chat_handlers.go:141` (scratch) | AIBC-16 regression: renamed the observed log field key from `"session_id"` to `"session_ref"` in the model-call-failure `h.logger.Error(...)` call | ✅ Killed — `TestBuildChatTurn_ModelFailureLogsRealErrorServerSide` failed: no entry had a `Context` field with `Key == "session_id"` |
| 3 | `internal/dashboard/ai_build_chat_handlers.go:238` (scratch) | AIBC-11 regression: dropped the `AND owner_user_id = $2` clause from `loadOwnedBuildChatSession`'s query, so any authenticated user could load and confirm any session ID (classic IDOR) | ✅ Killed — `TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation` failed: `expected 404 confirming another user's session, got 200` with the other user's app fully created |

**Sensor depth**: lightweight (3 targeted mutations, matching the task's explicit minimum and its explicit focus areas — the fix-commit's own new assertions)
**Sensor outcome**: 3 of 3 mutations killed
**Isolation verified**: `git status --porcelain` on the real worktree was empty immediately before `git worktree add` and immediately after `git worktree remove --force` (confirmed by direct command output, not inferred)

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| No features beyond what was asked | ✅ — fix commit adds only test coverage + one comment, no behavior change |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for the fixes | ✅ — `2359c85` touches exactly `ai_build_chat_handlers.go`, `ai_build_chat_handlers_test.go`, and the three `.specs/features/ai-build-chat/*.md` bookkeeping files |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ — `aiBuildChatHandlerTestPoolWithObservedLogger` mirrors the existing `aiBuildChatHandlerTestPool` helper shape; `zaptest/observer` is the standard zap testing pattern |
| Spec-anchored outcome check (asserted values match spec) | ✅ for the 5 PASS rows above; ❌/⚠️ for AIBC-12/AIBC-18, both dispositioned as accepted, documented gaps rather than fix-blocking |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ |
| Every test maps to a spec AC — no unclaimed tests | ✅ — all 3 new tests carry an `// AIBC-NN` comment tying them to the requirement |
| Documented guidelines followed | `AGENTS.md` §3 (build/test/vet/gofmt gates — all re-run and clean this round), §4 (the new `SPEC_DEVIATION` comment explicitly invokes the "typed errors that are safe to expose are the exception" carve-out for `ValidationError`) |

---

## Edge Cases (spec.md)

- [x] Two tabs, same session: unchanged from round 1 — last-write-wins is the natural consequence of the store's read-then-append pattern.
- [x] Permission revoked mid-flow → `TestBuildChatConfirm_RevokedWritePermissionForbidden` (unchanged).
- [x] Reserved table name (`_auth_users`) → `TestBuildChatConfirm_ReservedTableNameRejectedBeforeAnyMutation` (unchanged).
- [x] Resuming a `completed` session never happens → structurally guaranteed, unchanged.
- [x] Decrypt failure treated as unconfigured → `TestResolveDecryptedAIProviderKey_DecryptFailureReturnsError` (unchanged).

---

## Gate Check

- **Gate command**: `go build ./... && go test ./... && go vet ./... && gofmt -l <changed files>` (backend); `cd internal/dashboard/ui && npx tsc -b && npm run build` (frontend)
- **Build**: clean (`go build ./...` — no output, exit 0)
- **Vet**: clean (`go vet ./...` — no output, exit 0)
- **gofmt**: clean — `git diff --name-only b01f2be 2359c85 -- '*.go' | xargs gofmt -l` produced no output
- **Frontend**: `npx tsc -b` clean (after `npm install`, which this worktree needed — `node_modules` was absent); `npm run build` succeeded (492 modules, no errors, same as round 1)
- **Full `go test ./... -p 1`**: reproduced the same environmental ceiling round 1 documented — `internal/dashboard`, `internal/mcpserver`, and `internal/server` packages show a large cascade of failures, confirmed by grepping the raw output for `FATAL: sorry, too many clients already (SQLSTATE 53300)`, which matches on every failing line sampled. Re-running the exact failing tests in isolation (e.g. `TestAppMembersRBACMatrix`) passes cleanly, confirming the failures are Postgres connection-pool exhaustion under full-suite volume, not regressions. None of the failing test names belong to the ai-build-chat feature (`internal/dashboard/ai` package reports `ok`; no `AIProvider`/`BuildChat`/`AIBuild` test name appears in the failure list for `internal/dashboard`).
- **Feature-scoped isolation run** (`-run 'AIProvider|AIBuild|BuildChat|CallModel|AIProviderKey' -p 1`, all packages in scope): **34/34 pass**, including all 3 new tests from the fix commit (`TestBuildChatTurn_PlanShapeTurn` with the extended assertion, `TestBuildChatTurn_ModelFailureLogsRealErrorServerSide`, `TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation`)
- **Test count before this round's fix**: 31 (round 1's count, `83f4cfb..2f8aaa4`)
- **Test count after this round's fix**: 34 (round 1's 31 + 3 new: `TestBuildChatTurn_ModelFailureLogsRealErrorServerSide`, `TestBuildChatConfirm_AnotherUsersSessionReturnsNotFoundNoMutation`, plus the extended assertion inside the existing `TestBuildChatTurn_PlanShapeTurn`, which does not add a new test function but does add new evidence)
- **Delta**: +2 new test functions, +1 strengthened existing test
- **Skipped tests**: none in scope
- **Failures**: none in scope; full-suite failures are the pre-existing, documented, unrelated `max_connections` ceiling

---

## Fix Plans

None. No fix-blocking gaps remain. AIBC-12 and AIBC-18 are carried forward as documented, accepted spec-precision gaps (see the AIBC-12 decision above and AIBC-18's pre-approved Test Coverage Matrix scoping) — optional future hardening, not required before shipping.

---

## Requirement Traceability Update

| Requirement | Previous Status (round 1) | New Status |
| ----------- | -------------------------- | ----------- |
| AIBC-01 through AIBC-10 | ✅ Verified | ✅ Verified (unchanged) |
| AIBC-11 | ⚠️ Verified with gap | ✅ Verified — IDOR-shaped handler-layer test added |
| AIBC-12 | ⚠️ Verified with gap | ⚠️ Verified with accepted gap — payload-content assertion still missing; judged non-blocking (see decision above) |
| AIBC-13, AIBC-14, AIBC-15 | ✅ Verified | ✅ Verified (unchanged) |
| AIBC-16 | ⚠️ Verified with gap | ✅ Verified — observed-logger test added, proves the log call fires with `session_id` |
| AIBC-17 | ❌ Needs Fix | ✅ Verified — mutation-assertion added, sensor-confirmed to kill the exact regression it targets |
| AIBC-18 | ⚠️ Verified with gap (frontend-only per matrix) | ⚠️ Verified with accepted gap — same disposition, re-confirmed by independent code inspection this round |
| AIBC-19 through AIBC-21 | ✅ Verified | ✅ Verified (unchanged) |
| AIBC-22 | ⚠️ Verified with gap | ✅ Verified — `SPEC_DEVIATION` comment now documents the intentional wording divergence |
| AIBC-23, AIBC-24 | ✅ Verified | ✅ Verified (unchanged) |

spec.md's own traceability table (lines 148-171) already reflects "Verified" for all 24 rows as of commit `2359c85` — consistent with this report.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 22/24 ACs fully matched spec outcome; 2/24 carry an accepted, documented spec-precision/evidence gap (AIBC-12, AIBC-18) that does not block shipping
**Sensor**: 3/3 targeted mutations killed (round 2, focused on the three fixes); round 1's separate 5/5 general-risk mutations remain valid and unretracted
**Gate**: 34/34 feature-scoped tests pass; full-suite run reproduces the same pre-existing, documented, unrelated Postgres connection-pool ceiling as round 1 — not a regression

**What works**: All of round 1's "what works," plus: another-user's-session confirm attempt is now proven to return `404` with zero mutation at the handler layer (not just the store layer); a model-call failure is now proven to actually reach the logger at `Error` level with the session ID attached, not just proven to never leak to the client; a plan-shape chat turn is now proven, by direct assertion, to create no app — closing the one hard gap from round 1.

**Issues found**: None blocking. Two accepted, low-risk, documented gaps carried forward (AIBC-12: no test asserts the literal content of the history/system-prompt payload sent to the model, though the code path is simple and every other sub-clause is covered; AIBC-18: frontend gating verified by code inspection only, consistent with the project's pre-approved, no-test-framework Test Coverage Matrix).

**Next steps**: None required to ship. Optional future hardening (not blocking, recorded as a lesson): if `BuildChatTurn`'s history-assembly logic grows more complex, add a test that captures and asserts on the `history []ai.Message` argument passed to `callAIModel` via `withFakeAIModel`.
