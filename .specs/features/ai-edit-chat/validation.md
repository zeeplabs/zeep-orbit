# AI Edit Chat Validation

**Date**: 2026-08-23
**Spec**: `.specs/features/ai-edit-chat/spec.md`
**Diff range**: `2a11764..2630481` (commits `8edc61a`..`2630481` on `develop`)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Overall Verdict: ❌ FAIL

Two grounded gaps block a clean PASS: AIEC-18 has zero test coverage (evidence-or-zero), and the discrimination sensor found one surviving mutant (session-status guard in `EditChatConfirm` is untested). Everything else — 15 of 18 ACs, the build/vet/fmt gate, the frontend build, and 3 of 4 injected faults — checks out. Neither gap requires a design change; both are missing-test fixes.

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1–T12 | ✅ Done | All 12 tasks marked done in `tasks.md`; commits `8edc61a`..`2630481` present on `develop` in order. |

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AIEC-01: open/reuse edit session on (user, app) | Existing `in_progress` row reused, else created with `target_app_id` set | `internal/dashboard/ai_edit_chat_handlers_test.go:863` `TestGetEditChatSession_ReloadsExistingHistory` — asserts `got.Session.ID == session.ID` and reloaded message; `:892` `TestGetEditChatSession_CreatesFreshSessionAndEnforcesWriteAccess` — asserts fresh `in_progress` session, 0 messages | ✅ PASS |
| AIEC-02: `propose_add_column` with concrete table/column/type | `EditOp.Kind=="add_column"` with populated fields | `internal/dashboard/ai/client_edit_test.go:104` `TestCallEditModel_AddColumn`; `ai_edit_chat_handlers_test.go:155` `TestEditChatTurn_EditOpShapeTurn` asserts `got.EditOp.AddColumn.Table=="users"`, `.Column.Name=="email"` | ✅ PASS |
| AIEC-03: confirm add_column calls `AddTableColumnForUser`, applies immediately, session stays `in_progress` | Column exists in table; `finalSession.Status=="in_progress"` | `ai_edit_chat_handlers_test.go:349-410` `TestEditChatConfirm_AddColumn` — asserts `foundEmail`, `finalSession.Status=="in_progress"` | ✅ PASS |
| AIEC-04: validation error surfaces specific message, session `in_progress`, app unmodified | `got["error"] == ErrColumnAlreadyExists.Error()`; column count stays 1 | `ai_edit_chat_handlers_test.go:612-673` `TestEditChatConfirm_DuplicateColumnSurfacesSpecificError` | ✅ PASS |
| AIEC-05: no-write-access denied on every edit-chat endpoint + button hidden client-side | 403 before any handler runs; UI button absent for non-owner-non-superadmin | `ai_edit_chat_handlers_test.go:255` `TestEditChatTurn_ViewerForbidden` (403, 0 messages persisted); `:678` `TestEditChatConfirm_ViewerForbidden` (403, no mutation); `:892` viewer-403 on GET; `internal/mcpserver/tools_update_app_test.go:67` `TestOrbitUpdateApp_ViewerForbidden`; `internal/dashboard/ui/src/pages/AppDetailsPage.tsx:94` `canEditWithAI` gate | ✅ PASS — see Judgment Call 1 below for a narrower UX note that does not violate this AC's literal text |
| AIEC-06: audit origin `ai_chat` on every applied mutation | `audit_log.ip_address == "ai_chat"` for the resource touched | `ai_edit_chat_handlers_test.go:401-409` — `SELECT ip_address FROM audit_log WHERE resource_id=... AND action='app.table_column.create'` asserts `"ai_chat"` | ✅ PASS |
| AIEC-07: `propose_add_index` → `AddTableIndexForUser` | Index exists, correct unique flag | `ai/client_edit_test.go:131` `TestCallEditModel_AddIndex`; `ai_edit_chat_handlers_test.go:413-452` `TestEditChatConfirm_AddIndex` asserts `foundIdx` with `idx.Unique==true` | ✅ PASS |
| AIEC-08: `propose_add_table` → `CreateAppTableForUser` | Table created with given name | `ai/client_edit_test.go:67` `TestCallEditModel_AddTable`; `ai_edit_chat_handlers_test.go:455-482` `TestEditChatConfirm_AddTable` asserts `got.Table.Name=="notes"` | ✅ PASS |
| AIEC-09: `propose_add_reference` → `AddTableColumnForUser` with `References` set, only on a new column | Column has `References.Table` set | `ai/client_edit_test.go:162` `TestCallEditModel_AddReference`; `ai_edit_chat_handlers_test.go:486-534` `TestEditChatConfirm_AddReference` asserts `c.References.Table=="users"` | ✅ PASS |
| AIEC-10: decline FK on a column that already exists | AI declines in chat before proposing | No runtime-behavior test (can't be asserted without a real model call) — covered only as prompt-content: `editChatSystemPrompt` (`ai_edit_chat_handlers.go:481`) contains the explicit decline instruction, and `TestEditChatTurn_UsesEditChatSystemPromptNotBuildPrompt` proves that exact prompt is what's sent. The stated server-side fallback ("`AddTableColumnForUser` itself already rejects existing-column names") is real (`ErrColumnAlreadyExists`) but no edit-chat test exercises `add_reference` against an existing column name to prove the fallback fires end-to-end | ⚠️ Spec-precision gap (same class as `ai-build-chat`'s accepted off-topic-guard gap — not new to this feature) |
| AIEC-11: `propose_set_rls_mode` → `UpdateTableRLSModeForUser` | `Table.RLS=="enabled"` | `ai/client_edit_test.go:192` `TestCallEditModel_SetRLSMode`; `ai_edit_chat_handlers_test.go:537-579` `TestEditChatConfirm_SetRLSMode` | ✅ PASS |
| AIEC-12: `propose_toggle_auth` → `UpdateAppForUser` | `App.AuthEmailEnabled==true` | `ai/client_edit_test.go:217` `TestCallEditModel_ToggleAuth`; `ai_edit_chat_handlers_test.go:582-607` `TestEditChatConfirm_ToggleAuth`; `internal/dashboard/apps_update_app_foruser_test.go:19` `TestUpdateAppForUser_SuccessTogglesAuthEmailEnabled` | ✅ PASS |
| AIEC-13: `orbit_update_app` MCP tool, same RBAC/audit as REST/chat | Toggles `auth_email_enabled`; viewer denied | `internal/mcpserver/tools_update_app_test.go:21` `TestOrbitUpdateApp_TogglesAuthEmailEnabled`; `:67` `TestOrbitUpdateApp_ViewerForbidden` | ✅ PASS |
| AIEC-14 (edge case): OpenAI call fails → generic error, session `in_progress` | `Content==genericAIChatError`, `session.Status=="in_progress"` | `ai_edit_chat_handlers_test.go:216-251` `TestEditChatTurn_ModelFailureReturnsGenericMessage` — asserts both | ✅ PASS — **but mislabeled**: the test's own comment cites "AIEC-18 (mirrors AIBC-16)"; per spec.md's Edge Cases ordering this is actually AIEC-14 (see Judgment Call 2) |
| AIEC-15 (edge case): off-topic guard | AI declines and steers back | Same prompt-content-only coverage as AIEC-10: `TestEditChatTurn_UsesEditChatSystemPromptNotBuildPrompt` proves the guard text is on the wire, not that the model obeys it | ⚠️ Spec-precision gap (accepted pre-existing pattern) |
| AIEC-16 (edge case): "Recomeçar" abandons current session, creates fresh one, no pending-op requirement | New session ID, old session `abandoned`, pending op not required to resolve first | `ai_edit_chat_handlers_test.go:928-976` `TestRestartEditChatSession_AbandonsAndCreatesFresh` — asserts new ID, old status `abandoned`, and seeds an *unconfirmed* pending op before restarting | ✅ PASS |
| AIEC-17 (edge case): create-mode and edit-mode sessions coexist per (user, app, mode) | Distinct session rows, each independently resumable | `ai_build_sessions_store_test.go:313-346` `TestGetOrCreateInProgressEditSession_CoexistsWithCreateSession` | ✅ PASS |
| AIEC-18 (edge case): op references a nonexistent table/column → rejected with the underlying handler's own validation error | 404/400 from the handler's real error, session unchanged, app unmodified | **No test found.** Grepped `ai_edit_chat_handlers_test.go` for "not found"/"does not exist"/`ErrNotFound` in an edit-chat-confirm context — none. The mechanism exists (`AddTableColumnForUser` returns `ErrNotFound` when the target table is absent, `respondEditChatConfirmError` maps it to 404) but no test drives a proposed op against a nonexistent table through `EditChatConfirm` | ❌ **GAP — evidence-or-zero: NOT covered** |

**Status**: ❌ Gaps present (AIEC-18 uncovered; AIEC-10/15 spec-precision gaps, accepted pattern, not new)

---

## Discrimination Sensor

Isolated scratch: `git worktree add /tmp/zeep-orbit-verifier-scratch HEAD` (never `git stash`). Pre-sensor baseline `git status --porcelain`: ` M .specs/STATE.md` (pre-existing, unrelated). Post-cleanup porcelain: identical.

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/dashboard/ai_edit_chat_handlers.go:320` (`applyEditOperation`, `add_index` case) | Dispatched `add_index` to `AddTableColumnForUser` instead of `AddTableIndexForUser` (wrong handler for one `Kind`) | ✅ Killed — `TestEditChatConfirm_AddIndex` fails: "expected the users_email_idx index in the returned table, got []" |
| 2 | `internal/dashboard/apps_store.go:386` (`UpdateApp`) | `if !role.CanWrite()` → `if false && !role.CanWrite()`, removing the RBAC check `UpdateAppForUser` relies on | ✅ Killed — `TestUpdateAppForUser_ViewerForbidden` fails: "expected ErrForbidden for a viewer, got \<nil\>" |
| 3 | `internal/dashboard/ai_edit_chat_handlers.go:253-258` (`EditChatConfirm`) | Removed the `editChatAppliedMarker` no-op branch, so a repeat confirm no longer short-circuits | ✅ Killed — `TestEditChatConfirm_DoubleConfirmIsNoOp` fails: second confirm returns 400 instead of 200 |
| 4 | `internal/dashboard/ai_edit_chat_handlers.go:237` (`EditChatConfirm`) | `if session.Status != "in_progress"` → `if false && session.Status != "in_progress"`, so confirming against an `abandoned`/`completed` session no longer gets rejected | ❌ **Survived** — full `go test ./internal/dashboard/...` suite (134s) passes unchanged with this mutation in place |

**Sensor depth**: lightweight (4 targeted mutations, standard-risk feature; not P0/critical-path)
**Result**: 3/4 killed — ❌ FAIL (a surviving mutant blocks a clean verdict per the skill's rule: "do not mark the feature done if the sensor found weak tests")

Mutation 4 points at a real, narrow gap: no test ever confirms `EditChatConfirm` rejects a session that isn't `in_progress` (e.g., already `abandoned` via restart, or a stale client retrying against an old session). The guard code is almost certainly correct — it's a one-line status comparison identical in shape to guards tested elsewhere in this codebase — but nothing in this feature's test suite would catch someone accidentally deleting or inverting it in a future change.

---

## Judgment Call 1 — T12's client-side "Edit with AI" button gating (flagged deviation)

**The code**: `internal/dashboard/ui/src/pages/AppDetailsPage.tsx:94`
```ts
const canEditWithAI = Boolean(app && me && (app.owner_id === me.id || me.role === "superadmin"));
```

**The claim under review**: a non-owner `app_members` row with `editor`/`admin` grant (server-side `CanWrite()==true`) does not see the button, even though the same user calling the REST/MCP endpoints directly would get a working response.

**Verified independently**:
- `GET /dashboard/api/apps/{id}` (`internal/dashboard/handler.go:1007-1028`) discards the caller's per-app role entirely (`app, _, err := GetApp(...)`) — the response body never carries it. There is genuinely no endpoint today that lets a non-admin caller learn their own role on an app they don't own, confirming the stated constraint.
- The claim that this "mirrors the backend's `CanWrite()` semantics" and matches "the same write-permission signal the manual edit form already checks" (design.md, T12 Done-when) does **not** hold up: the manual "Add Table" button on the same page (`AppDetailsPage.tsx:253`) has **no** client-side permission gate at all — it's rendered unconditionally and relies entirely on the backend rejecting the mutation. So the new AI button is actually *stricter* than the existing manual-form pattern, not a reuse of it.

**Spec text, AIEC-05** (spec.md line 61): *"IF the user lacks write access (`CanWrite()`) to app X THEN the system SHALL return an authorization error for every edit-chat endpoint scoped to X, and the 'Edit with AI' entry point SHALL NOT be shown for that app in the UI."*

**Decision: acceptable gap for this MVP, not an AC violation, worth a follow-up task — not a blocking fix.**

Reasoning: AIEC-05's UI clause is one-directional — it requires the button to be **absent when access is lacking**. It says nothing about guaranteeing the button's **presence when access is present**. The implementation fails closed (hides more often than strictly required) rather than failing open (showing the button to someone without write access), which is the safer direction of error for a mutating feature. This is a real product gap — a legitimate `editor` app-member cannot use "Edit with AI" today — but fixing it requires a new capability (an endpoint exposing "my role on this app") that is explicitly out of this feature's built surface and wasn't budgeted in tasks.md. Recommend a follow-up task, not a fix routed back into this feature's 3-iteration verify loop.

**What to validate if picked up**: whether adding "my role" to the existing `GET /apps/{id}` response (guarded to the caller's own role only, not other members') is acceptable, or whether it needs its own endpoint — that's a design decision, not something to default on unilaterally.

---

## Judgment Call 2 — AIEC-16 mislabeling in tasks.md and test comments

**tasks.md's T8** "Done when" cites AIEC-16 for double-confirm idempotency: *"Double-confirm on an already-applied operation is a no-op, not a duplicate mutation (AIEC-16)"* (tasks.md line 274).

**spec.md's actual edge-case ordering** (lines 103-107, mapped 1:1 to AIEC-14 through AIEC-18 in the traceability table):
1. OpenAI call fails → **AIEC-14**
2. Off-topic guard → **AIEC-15**
3. "Recomeçar" restart → **AIEC-16**
4. create/edit session coexistence → **AIEC-17**
5. Proposed op references a nonexistent table/column → **AIEC-18**

**Decision: this is a real requirement-ID mislabeling, but it is cosmetic and confined to `tasks.md` and test-file comments — idempotency is NOT missing an AC. It just isn't tagged with an ID of its own.**

Reasoning: re-reading spec.md's actual acceptance criteria (P1 AC3, line 59) — *"WHEN the user confirms a proposed `add_column` operation THEN the system SHALL call `AddTableColumnForUser`... applying the change immediately, and the session SHALL remain `in_progress` after the operation completes"* — this AC never mentions repeat-confirm behavior at all. The double-confirm-is-a-no-op guarantee is a **design decision** (design.md's Error Handling Strategy table: "Confirm called with no pending operation... re-derive from the session's last persisted operation, no-op if already applied"), not a spec-level acceptance criterion. So there is genuinely no AIEC-NN that idempotency traces to — the implementer's citation of "AIEC-16" was simply the nearest edge-case-looking number, not a real requirement ID for this behavior. This is consistent with my finding above: `TestEditChatConfirm_DoubleConfirmIsNoOp` is a real, valuable test (and its mutant was killed), but it should be understood as covering a design-level guarantee, not spec.md's AIEC-16 (which is actually the "Recomeçar" edge case, itself separately and correctly covered by `TestRestartEditChatSession_AbandonsAndCreatesFresh`).

A second instance of the same mislabeling pattern exists in test comments: `ai_edit_chat_handlers_test.go:213` labels `TestEditChatTurn_ModelFailureReturnsGenericMessage` as "AIEC-18 (mirrors AIBC-16)" — but per the ordering above, an OpenAI-call failure is AIEC-14, not AIEC-18. This left the *actual* AIEC-18 (nonexistent-table/column validation) looking covered when it silently isn't — see the AIEC-18 row above. This is worth fixing as a documentation correction alongside the AIEC-18 test gap, since the mislabeling is precisely what let the real gap go unnoticed.

**Recommendation**: (1) fix the two comment mislabels (tasks.md T8, and `ai_edit_chat_handlers_test.go:213`) to cite the design-decision rationale instead of a wrong AIEC number; (2) add the missing AIEC-18 test (see Fix Plan below) — these are independent fixes, not one fix covering both.

---

## Gate Check

- **Gate command**: `go build ./...`; `go test ./internal/dashboard/... ./internal/mcpserver/... ./internal/server/...`; `go vet ./...`; `gofmt -l $(git diff --name-only 2a11764..HEAD -- '*.go')`; frontend `npx tsc -b && npm run build`; i18n JSON validation
- **go build ./...**: ✅ PASS (clean, no output)
- **go vet ./...**: ✅ PASS (clean, no output)
- **gofmt -l <changed files>**: ✅ PASS (no files listed — all correctly formatted)
- **go test** (feature-relevant packages, run **serially** across packages — see note): `internal/dashboard`: ✅ PASS (129s, all tests including all `ai-edit-chat` tests); `internal/dashboard/ai`: ✅ PASS; `internal/mcpserver`: ❌ FAIL, but only pre-existing webhook tests (`TestOrbitCreateWebhook_*`, `TestOrbitSaveWebhookEventMapping_*`, `TestOrbitListWebhooks_*`, `TestOrbitGetWebhook_*`, `TestOrbitListWebhookDeliveries_*`) — zero `orbit_update_app` or other ai-edit-chat-tool failures; `internal/server`: ❌ FAIL, but only pre-existing webhook-delivery tests (`TestWebhookActive_*`, `TestWebhookDelivery_*`) — zero edit-chat-route failures
- **Root cause of the webhook failures**: environmental, not this feature. Running the three packages **concurrently** (Go's default) against the same `TEST_DATABASE_URL` causes cross-package `TRUNCATE ... CASCADE` races on shared `zeep_system` tables, which is what first surfaced as failures in `ai_build_chat`/`ai_edit_chat` tests too. Running with `-p 1` (serial) eliminates all of those; only the webhook tests kept failing, independent of concurrency, tracing to a missing `WEBHOOK_TOKEN_ENCRYPTION_KEY`/session setup unrelated to this diff (confirmed these test files predate this feature: `cda893f`, `4c87a73`). Verified no `ai-edit-chat` test appears in any failure list once run serially — grepped for "editchat|updateapp|orbit_update_app" across the full serial output: zero matches.
- **Test count before feature**: not measured (no isolated baseline checkout run) — the diff stat shows 13 files changed, 8 of them new `*_test.go`/test-additions files
- **Test count after feature**: `ai_edit_chat_handlers_test.go` (18 test funcs), `ai_build_sessions_store_test.go` (+3 new edit-mode tests), `ai/client_edit_test.go` (10 test funcs), `apps_update_app_foruser_test.go` (3 test funcs), `tools_update_app_test.go` (2 test funcs), `server_test.go` (+3 edit-chat route tests) — net new tests, none removed
- **Skipped tests**: none observed to be skipped for this feature (DB-backed tests use `t.Skip` only when `TEST_DATABASE_URL` is unset, which was set for this run)
- **Failures**: webhook-only, pre-existing, unrelated to this diff (see above)
- **Frontend**: `npx tsc -b`: ✅ PASS (clean); `npm run build`: ✅ PASS (493 modules, built in 1.46s; pre-existing chunk-size warning, not new)
- **i18n JSON**: `en.json`: ✅ valid; `pt-BR.json`: ✅ valid

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — new file (`ai_edit_chat_handlers.go`) isolated per design's explicit decision; no changes to `ai_build_chat_handlers.go` |
| Surgical changes | ✅ — `handler.go`/`provisioner.go`/`tools.go`/`server.go`/`client.go` each touched additively |
| No scope creep | ✅ — no rename/drop-column/FK-on-existing-column logic added, matching spec's Out of Scope table |
| Matches patterns | ✅ — mirrors `ai_build_chat_handlers.go`'s IDOR guard, `*ForUser` handler shape, audit-origin convention |
| Spec-anchored outcome check (asserted values match spec) | ⚠️ — 16/18 ACs match; AIEC-10/15 are accepted spec-precision gaps (prompt-content proxy only, same as `ai-build-chat`); AIEC-18 is an uncovered AC, not merely imprecise |
| Per-layer Coverage Expectation met | ⚠️ — store/handler/AI-client layers have thorough branch coverage; the one true gap is AIEC-18's "nonexistent table" branch inside `EditChatConfirm` |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — every test file's tests cite an AIEC ID or edge case in comments (two labels are wrong, see Judgment Call 2, but the tests themselves are legitimately grounded) |
| Documented guidelines followed | AGENTS.md §3 (backend/frontend gate commands), §4 (English-only API errors — confirmed: all new error strings in `respondEditChatConfirmError`/handlers are English), §5 (i18n both locales updated in T11's commit) |

---

## Edge Cases

- [x] OpenAI call fails (AIEC-14): handled, tested (mislabeled AIEC-18 in comment — cosmetic)
- [x] Off-topic guard (AIEC-15): handled at prompt level; behavior not independently testable (accepted gap)
- [x] "Recomeçar" restart (AIEC-16): handled, tested
- [x] create/edit session coexistence (AIEC-17): handled, tested
- [ ] Nonexistent table/column reference (AIEC-18): mechanism exists in the reused handler, but **not tested** through the edit-chat path — NOT handled per evidence-or-zero

---

## Fix Plans

### Fix 1: Add AIEC-18 coverage — nonexistent-table rejection through `EditChatConfirm`

- **Root cause**: no test in `ai_edit_chat_handlers_test.go` persists a proposed op referencing a table that doesn't exist in the app's schema and confirms `EditChatConfirm` returns the underlying handler's own error (404 `ErrNotFound`, unmodified app, session still `in_progress`)
- **Fix task**: add `TestEditChatConfirm_NonexistentTableSurfacesNotFound` — persist `{Kind: "add_column", AddColumn: {Table: "does_not_exist", ...}}` on a session, confirm, assert 404 and that `finalSession.Status == "in_progress"`. Mirrors `TestEditChatConfirm_DuplicateColumnSurfacesSpecificError`'s structure.
- **Priority**: Major (a real spec AC has zero coverage; the underlying mechanism is very likely correct, but nothing proves it for this path)

### Fix 2: Add coverage for the non-`in_progress` session guard in `EditChatConfirm`

- **Root cause**: the discrimination sensor's mutation 4 (disabling `session.Status != "in_progress"`) survived the full suite — no test exercises confirming against an `abandoned` (or otherwise non-`in_progress`) session
- **Fix task**: add `TestEditChatConfirm_AbandonedSessionRejected` — create a session, persist a pending op, call `AbandonAndRestartEditSession` (or directly `UPDATE ... SET status='abandoned'`) on it, then confirm against the now-abandoned session ID and assert 400 with no mutation applied
- **Priority**: Minor (defense-in-depth for a guard that's almost certainly correct today, but currently unproven)

### Fix 3 (non-blocking, documentation only): Correct AIEC mislabeling

- **Root cause**: `tasks.md` T8's Done-when cites AIEC-16 for double-confirm idempotency (should reference design.md's Error Handling Strategy, not a spec AC — see Judgment Call 2); `ai_edit_chat_handlers_test.go:213`'s comment cites AIEC-18 for a model-failure test that is actually AIEC-14
- **Fix task**: correct both comments; no code or test-assertion change needed
- **Priority**: Cosmetic

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| AIEC-01 | Implementing | ✅ Verified |
| AIEC-02 | Implementing | ✅ Verified |
| AIEC-03 | Implementing | ✅ Verified |
| AIEC-04 | Implementing | ✅ Verified |
| AIEC-05 | Implementing | ✅ Verified |
| AIEC-06 | Implementing | ✅ Verified |
| AIEC-07 | Implementing | ✅ Verified |
| AIEC-08 | Implementing | ✅ Verified |
| AIEC-09 | Implementing | ✅ Verified |
| AIEC-10 | Implementing | ⚠️ Verified (spec-precision gap, accepted) |
| AIEC-11 | Implementing | ✅ Verified |
| AIEC-12 | Implementing | ✅ Verified |
| AIEC-13 | Implementing | ✅ Verified |
| AIEC-14 | Implementing | ✅ Verified |
| AIEC-15 | Implementing | ⚠️ Verified (spec-precision gap, accepted) |
| AIEC-16 | Implementing | ✅ Verified |
| AIEC-17 | Implementing | ✅ Verified |
| AIEC-18 | Implementing | ❌ Needs Fix |

---

## Summary

**Overall**: ❌ Not Ready (2 grounded gaps, both narrow and low-risk to close)

**Spec-anchored check**: 15/18 ACs matched spec outcome exactly; 2 accepted spec-precision gaps (AIEC-10, AIEC-15 — same class already accepted for `ai-build-chat`'s off-topic guard); 1 real uncovered AC (AIEC-18)
**Sensor**: 3/4 mutations killed; 1 survived (non-`in_progress` session guard in `EditChatConfirm`, mutation 4)
**Gate**: `go build`/`go vet`/`gofmt` clean; `go test` clean for every `ai-edit-chat`-relevant package (serial run required — see Gate Check note); frontend `tsc`/`vite build` clean; i18n JSON valid. Pre-existing, unrelated webhook test failures in `internal/mcpserver`/`internal/server` — zero overlap with this feature's diff surface.

**What works**: all 12 tasks implemented and gated; RBAC, audit-origin, and IDOR-guard parity with the existing REST/MCP handlers confirmed by tests and by the mutation sensor (2 of the 3 killed mutants were exactly these properties); every one of the 6 `propose_*` operations round-trips end to end (AI client → handler → confirm → real DB mutation) with a passing test.

**Issues found**:
1. AIEC-18 (nonexistent table/column reference) has no test — Fix 1
2. `EditChatConfirm`'s non-`in_progress`-session guard is untested and its mutant survived — Fix 2
3. Cosmetic AIEC mislabeling in `tasks.md` and one test comment — Fix 3

**Next steps**: route Fix 1 and Fix 2 to an implementer as fix tasks (both are additive test-only changes, no production code implicated); apply Fix 3's comment corrections in the same pass. Re-run this Verifier after both land — expect a clean PASS at that point, since no design or handler-level change is required.
