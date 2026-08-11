# Inbound Webhooks Validation

**Date**: 2026-08-10
**Spec**: `.specs/features/inbound-webhooks/spec.md`
**Diff range**: `573f07f..cf58718` (18 commits, `main`, local/unpushed)
**Verifier**: independent sub-agent (author ≠ verifier) — did not participate in implementation

> [^token-storage]: Token storage was superseded after this Verifier run — see [Addendum](#addendum-cf58718head---post-pass-changes-and-opus-review) below. References to `TokenHash`/`hashWebhookToken` throughout this report describe the state *at the time this PASS was issued*; the column is now `TokenSecret`, encrypted (AES-256-GCM) rather than hashed.

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1-T17 | ✅ Done | All 17 tasks marked complete in `tasks.md`; independently re-run (see Gate Check below) rather than trusted from the checkmarks alone. |

---

## Spec-Anchored Acceptance Criteria

### P1: App owner creates a webhook and captures a real sample

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: create webhook → unique URL+token | non-empty token, distinct from stored secret | `internal/dashboard/webhooks_store_test.go` — `TestCreateWebhook_HappyPath`: `token == ""` fails, round-trips through `DecryptWebhookToken`/`VerifyWebhookToken` [^token-storage]; also `internal/dashboard/ui/e2e/webhooks.spec.ts:106-111` (webhookId/token length > 0) | ✅ PASS |
| AC2: unmapped webhook is capture-only, no write | no table write; capture-only | Code: `internal/server/webhook_handler.go:81-90` returns before reaching `handleActiveDelivery`, which is the only path that ever calls `query.Build*`; `webhooks.spec.ts:114-119` (`rowCount == 0` after two capture POSTs) | ✅ PASS |
| AC3: 2nd capture overwrites sample | sample after 2nd call has only 2nd call's fields | `internal/dashboard/webhooks_store_test.go:151-187` — `TestStoreCapturedSample_OverwritesOnSecondCall`: asserts key `"a"` absent, `"b"==2` present; `internal/server/webhook_handler_test.go:196-235` (same assertion at HTTP layer); `webhooks.spec.ts:150-153` (`employeeName` visible, `foo` from decoy absent) | ✅ PASS |
| AC4: method mismatch → 404 | exact status 404 | `internal/server/webhook_handler_test.go:68-89` — `TestWebhookDelivery_MethodMismatchReturns404NoDeliveryLogged`: `rec.Code != http.StatusNotFound` fails, and asserts 0 deliveries logged; `webhooks.spec.ts:132-133` (`expect(...).toBe(404)`) | ✅ PASS |
| AC5: missing/invalid token → 401, attempt logged | exact status 401 + 1 delivery `outcome=invalid_token` | `internal/server/webhook_handler_test.go:91-139` — `TestWebhookDelivery_MissingTokenReturns401AndLogsInvalidToken` / `_WrongTokenReturns401...`: `rec.Code != http.StatusUnauthorized`, `list[0].Outcome != "invalid_token"`; `webhooks.spec.ts:137-141` | ✅ PASS |
| AC6: every call recorded regardless of outcome | delivery row per call (except 404 method-mismatch, per design) | Cumulative: `webhook_handler_test.go` (malformed/capture/overwrite), `webhook_active_insert_test.go` (unmapped/duplicate/inserted/write_error), `webhook_active_update_delete_test.go` (updated/deleted/row_not_found); `webhooks.spec.ts:218-232` asserts all 7 expected outcomes visible in the dashboard delivery log after one full run | ✅ PASS |

### P2 (map + activate): App owner maps captured sample and activates for inserts

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: save insert mapping | mapping persisted with `action=insert` | `internal/dashboard/webhook_event_mappings_store_test.go:36-67` — `TestSaveEventMapping_HappyPathInsert`: `row.Action != "insert"`; `internal/dashboard/webhooks_handler_test.go:329-356` (audit `webhook.mapping.save` count==1); `webhooks.spec.ts:157-166` | ✅ PASS |
| AC2: activate with ≥1 mapping flips capture→active | `status == "active"` | `internal/dashboard/webhooks_store_test.go:214-244` — `TestActivateWebhook_WithMappingSucceeds`: `got.Status != "active"`; `webhooks_handler_test.go:415-451` (HTTP layer + audit); `webhooks.spec.ts:168-172` | ✅ PASS |
| AC3: matching event creates a row | exactly 1 row with mapped values | `internal/server/webhook_active_insert_test.go:215-246` — `TestWebhookActive_InsertHappyPathCreatesRow`: `externalID != "u-42"`, `fullName != "Ana Souza"`; `webhooks.spec.ts:194-201` | ✅ PASS |
| AC4: duplicate event id skipped | 200, `duplicate_skipped`, row count unchanged | `webhook_active_insert_test.go:175-213` — `TestWebhookActive_DuplicateEventIDSkipsSecondWrite`: `count != 1` after 2 calls, `list[0].Outcome != "duplicate_skipped"`; `webhooks.spec.ts:203-210` | ✅ PASS |
| AC5: unmapped event-type → 200, logged unmapped | exact status 200, `outcome=unmapped`, no write | `webhook_active_insert_test.go:147-173` — `TestWebhookActive_UnmappedEventTypeReturns200NoWrite`; `webhooks.spec.ts:174-181` | ✅ PASS |
| AC6: mapping-apply failure → 500, no partial write | exact status 500, `write_error`, row count 0 | `webhook_active_insert_test.go:248-312` — RLS-denied and missing-field variants; `webhooks.spec.ts:183-190` | ✅ PASS |

### P2 (update/delete): App owner maps update and delete with a match key

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: match key required for update/delete | `ErrMatchKeyRequired` when absent | `internal/dashboard/webhook_event_mappings_store_test.go:69-92` — `TestSaveEventMapping_UpdateAndDeleteRequireMatchKey`: `err != ErrMatchKeyRequired` fails | ✅ PASS |
| AC2: update locates row by match key, overwrites | exactly 1 row, linked columns overwritten, `outcome=updated` | `internal/server/webhook_active_update_delete_test.go:60-98` — `TestWebhookActive_UpdateHappyPathOverwritesLinkedColumns`: `count != 1`, `fullName != "Ana Souza Silva"`, `list[0].Outcome != "updated"` | ✅ PASS |
| AC3: delete locates row by match key, removes per soft-delete config | 0 rows, `outcome=deleted` | `webhook_active_update_delete_test.go:100-131` — `TestWebhookActive_DeleteHappyPathRemovesRow` | ✅ PASS |
| AC4: no match → 200, `row_not_found` | exact status 200, `outcome=row_not_found`, no write | `webhook_active_update_delete_test.go:133-171` — Update/DeleteNoMatchingRow tests | ✅ PASS |
| AC5: one webhook, multiple event-type mappings/actions | insert+update+delete resolve correctly in sequence on one webhook | `webhook_active_update_delete_test.go:173-228` — `TestWebhookActive_FullLifecycleCreateUpdateDelete`: asserts row created, then updated field, then 0 rows, then `[deleted, updated, inserted]` newest-first | ✅ PASS |

Note: this story is **not** exercised by the Playwright e2e (`webhooks.spec.ts` explicitly documents this scope boundary at lines 9-13); coverage here is entirely backend-integration, verified directly above — the traceability table's claim in `spec.md` is accurate, not merely asserted.

### P2 (delivery log): App owner reviews webhook deliveries in the dashboard

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: deliveries listed reverse-chronological | newest-first, timestamp/outcome/event-type shown | `internal/dashboard/webhook_deliveries_store_test.go:50-82` — `TestListDeliveries_NewestFirst`; `internal/dashboard/webhooks_handler_test.go:494-545` (HTTP layer, same ordering); `webhooks.spec.ts:215-232` | ✅ PASS |
| AC2: opening an entry shows raw payload / error detail | raw payload + error detail visible for a failed entry | Backend: `webhooks_handler_test.go:533-541` — asserts `ErrorDetail == "boom"`, `RawPayload["b"] == 2` on the wire DTO. **UI expand-and-display behavior itself (`Webhooks.tsx:329-338`, `WebhookDeliveryLog`) has no test** — neither the e2e spec nor a component test clicks an entry and asserts the expanded raw payload/error text renders. | ⚠️ Partial — API contract proven; UI rendering of the expanded detail is unverified (see Gaps) |
| AC3: 30-day purge | rows older than cutoff removed, others untouched | `webhook_deliveries_store_test.go:119-169` — `TestPurgeExpiredDeliveries_RemovesOnlyOldRows` / `_NoOpWhenNothingExpired`; killed by discrimination sensor mutation #3 (see below) | ✅ PASS |

### P3: App owner manages webhook lifecycle

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: rotate token invalidates old, generates new, mappings untouched | old token no longer verifies, new token verifies, distinct values | `internal/dashboard/webhooks_store_test.go` — `TestRotateToken_InvalidatesOldToken`: old token fails `VerifyWebhookToken`, new token passes it [^token-storage]; HTTP layer: `webhooks_handler_test.go:224-266` — `TestRotateWebhookTokenHandler_InvalidatesOldTokenAndAudits` (also asserts old token no longer verifies via `VerifyWebhookToken`) | ✅ PASS |
| AC2: delete webhook stops URL, retains delivery log until 30-day purge | webhook 404s, row physically exists (soft-delete) | `internal/dashboard/webhooks_store_test.go:278-308` — `TestSoftDeleteWebhook_NeverHardDeletes`: explicitly queries `deleted_at IS NOT NULL` to prove the row is **not** hard-deleted; `webhooks_handler_test.go:272-310` (HTTP layer, `GetWebhookByID` → `ErrWebhookNotFound` post-delete) | ✅ PASS |
| AC3: every create/edit/delete/rotate action recorded in `audit_log` | one `audit_log` row per action, correct `action` value | create: `webhooks_handler_test.go:151-159` (`webhook.create` count==1); rotate: `:257-265` (`webhook.rotate_token`); delete: `:301-309` (`webhook.delete`); mapping save/activate also audited (`:347-355`, `:442-450`) — **but there is no "edit webhook" capability anywhere in the codebase** (no `UpdateWebhook` store function, no PUT/PATCH handler, no route) to audit in the first place. | ❌ GAP — "edit" clause of this AC has no implementation, hence no test; not the same as the deliberately-scoped-out P2-update/delete-story exclusion, which the codebase explicitly documents. This gap is undocumented anywhere (`design.md`, `tasks.md`, code comments). |

**Status**: ⚠️ 22/23 traceability rows (WEBHOOK-01 through -22) fully verified with matching spec outcomes. WEBHOOK-23 (audit on lifecycle actions) is verified for create/rotate/delete/activate/mapping-save/mapping-delete, but the "edit webhook" action referenced by spec P3 AC3 does not exist in the implementation — see Gaps.

---

## Edge Cases

- [x] Malformed JSON body (POST/PUT) → 400, logged `malformed`: `internal/server/webhook_handler_test.go:141-162`
- [⚠️] GET-configured webhook with no query params → captured/processed as empty payload, not an error: **verified by code reading only** (`internal/server/webhook_handler.go:356-365`, `parseWebhookPayload`'s GET branch builds an empty map when `r.URL.Query()` is empty — never errors). No automated test exercises a GET-method webhook end-to-end (the only GET usage in the test suite, `webhook_handler_test.go:74`, is a method-*mismatch* case against a POST-configured webhook, which 404s before `parseWebhookPayload` is ever called). Manually verified correct; not test-verified.
- [x] Purge runs regardless of webhook active/inactive/deleted state: `PurgeExpiredDeliveries` (`internal/dashboard/webhook_deliveries_store.go:140-152`) has no join/filter on `webhook_subscriptions` status at all — purges by `received_at` only, structurally satisfying the edge case; also covered by the discrimination sensor (mutation #3 below) proving the cutoff logic is real, not a no-op.
- [x] Activate with zero mappings rejected, not silent no-op: `internal/dashboard/webhooks_store_test.go:189-212` (`ErrNoMappings`, no state change) + `webhooks_handler_test.go:383-411` (HTTP 400)

---

## Discrimination Sensor

Isolated via `git worktree add /tmp/wh-sensor-worktree HEAD` (never `git stash`). Baseline `git status --porcelain` captured empty before any sensor work; confirmed identical after cleanup (see below). Each mutation applied one at a time, relevant test run, mutant confirmed killed, mutation reverted before the next.

| # | File:line | Description | Test run | Killed? |
| --- | --- | --- | --- | --- |
| 1 | `internal/server/webhook_handler.go:153` | Flipped dedup check `if seen {` → `if !seen {` (inverts duplicate-skip logic) | `TestWebhookActive_DuplicateEventIDSkipsSecondWrite` | ✅ Killed — `got "inserted"`, want newest outcome `duplicate_skipped` |
| 2 | `internal/dashboard/webhooks_store.go:288` | Removed `deleted_at = now()` from `SoftDeleteWebhook`'s UPDATE (kept only `updated_at`) | `TestSoftDeleteWebhook_NeverHardDeletes` | ✅ Killed — `expected ErrWebhookNotFound ..., got <nil>` |
| 3 | `internal/dashboard/webhook_deliveries_store.go:145` | Flipped purge cutoff `received_at < now() - $1` → `received_at > now() - $1` (purges newest instead of oldest) | `TestPurgeExpiredDeliveries_RemovesOnlyOldRows` | ✅ Killed — `expected 0 remaining deliveries for the old webhook, got 1` |
| 4 | `internal/server/webhook_handler.go:54` | Flipped method-match check `r.Method != wh.Method` → `r.Method == wh.Method` | `TestWebhookDelivery_MethodMismatchReturns404NoDeliveryLogged` | ✅ Killed — `expected 404 on method mismatch, got 200: {"status":"captured"}` |

**Sensor depth**: lightweight (4 targeted behavior-level mutations; this is a standard feature, not P0/critical-path).
**Result**: 4/4 killed — ✅ PASS

**Isolation verification**: `git status --porcelain` on the real worktree before the sensor run was empty; `git worktree remove --force` + `rm -rf` executed after all 4 mutations were reverted; `git status --porcelain` afterward is again empty and `git worktree list` shows no leftover worktrees — real tree confirmed untouched.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ |
| No scope creep | ✅ |
| Matches patterns | ✅ (table_policies_store/handler shape reused throughout, confirmed by direct comparison) |
| Spec-anchored outcome check (asserted values match spec) | ✅ (see AC table above; all but WEBHOOK-19/AC2's UI layer and WEBHOOK-23's "edit" clause) |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ for backend; ⚠️ frontend UI expand behavior untested (see Gaps) |
| Every test maps to a spec requirement — no unclaimed tests | ✅ (spot-checked; every test file's tests trace to a specific AC/Done-when criterion, several with explicit doc-comment citations) |
| Documented guidelines followed | ✅ `AGENTS.md` §3 gate commands, §4 error-string/no-raw-error rules (confirmed: `internal/server/webhook_handler.go`'s 500 paths never leak `err.Error()` into the HTTP response body — verified directly in `TestWebhookActive_InsertRLSDeniedReturns500WriteErrorNoRawErrorLeaked`) |

One documented `SPEC_DEVIATION` found in `internal/dashboard/webhooks_store.go:365-368` (`SaveEventMapping` takes `reg`/`appName` params beyond design.md's listed signature) — correctly marked and justified; not a gap.

---

## Gate Check

- **Gate command**: `go build ./... && go test -p 1 ./internal/dashboard/... ./internal/server/... ./internal/webhookengine/... && go vet ./internal/dashboard/... ./internal/server/... ./internal/webhookengine/...`, plus `cd internal/dashboard/ui && npx tsc -b && npm run build`, plus `cd internal/dashboard/ui && npx playwright test webhooks`
- **Result**: 237 backend tests passed, 0 failed (verified with `-p 1`; **note**: `go test ./...` without `-p 1` flakes on `internal/server` with `relation "zeep_system.dashboard_users" does not exist` because `internal/dashboard`'s tests `DROP SCHEMA zeep_system CASCADE` and race against `internal/server`'s tests sharing the same `TEST_DATABASE_URL` when run in parallel — this is a pre-existing repo-wide test-infra characteristic, already called out in `reusable-ci.yml:60-66`, not introduced by this feature). Frontend `tsc -b` and `npm run build` both clean. Playwright `webhooks` spec: 1/1 passed against a real server + fresh Postgres DB, run end-to-end by this Verifier (not merely read from a prior report).
- **gofmt**: clean on all files changed in the diff range
- **Skipped tests**: none observed
- **Failures**: none

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| WEBHOOK-01 through WEBHOOK-22 | Tasks (claimed Verified) | ✅ Verified (independently re-derived, evidence above) |
| WEBHOOK-19 (AC2 UI-layer expand behavior) | Tasks (claimed Verified via backend test) | ⚠️ Verified at API layer only — UI rendering unverified |
| WEBHOOK-23 | Tasks (claimed Verified) | ⚠️ Verified for create/rotate/delete/activate/mapping actions only — "edit webhook" has no implementation to verify |

---

## Summary

**Overall**: ⚠️ Issues (two real, low-severity gaps; every other AC and edge case independently confirmed with `file:line` evidence, a fully-passing gate, and a 4/4 discrimination-sensor kill rate)

**Spec-anchored check**: 27/29 criteria (23 traceability rows' underlying ACs, plus edge cases) matched the spec-defined outcome exactly; 1 partial (WEBHOOK-19 AC2, UI layer untested) + 1 gap (WEBHOOK-23's "edit" clause, unimplemented)
**Sensor**: 4/4 mutations killed
**Gate**: all green (backend 237 tests, frontend build, 1 e2e spec)

**What works**: The full P1→P3 feature set is implemented and independently re-verified end-to-end, including running the actual Playwright spec against a live server (not just trusting the prior "all pass" claim), and a from-scratch discrimination sensor in an isolated worktree that confirmed the test suite genuinely detects 4 distinct classes of regression (dedup logic, soft-delete, purge cutoff direction, method-match routing).

**Issues found**:

1. **WEBHOOK-23 / P3 AC3 — "edit webhook" has no implementation.** Spec text: "Every webhook create, edit, delete, and token-rotation action SHALL be recorded in the existing dashboard `audit_log`." There is no `UpdateWebhook` store function, no dashboard handler, and no route for editing an existing webhook's own config (name, method, `event_type_path`, `event_id_path`) anywhere in the codebase — grep-confirmed. This was silently dropped starting at `design.md` (the `WebhookConfigStore` interface list never included an update method) and flowed unflagged through `tasks.md` (T9's Done-when never mentions edit) into the implementation and its own Requirement Traceability table, which claims WEBHOOK-23 is "covered by the create/rotate/delete/activate handler tests' audit assertions" without noting "edit" is simply absent. Distinct from the P2-update/delete e2e-scope exclusion, which *is* explicitly documented as a deliberate boundary — this one is an undocumented capability gap.
   - **Fix task**: Either (a) implement `UpdateWebhook` (name/method/paths) with its own dashboard endpoint, RBAC gate, and `webhook.edit`-style audit entry plus a test mirroring `TestRotateWebhookTokenHandler_...`'s shape, or (b) if "edit" was never actually intended as a distinct capability (e.g., the owner is expected to delete+recreate, consistent with `context.md`'s stated workaround for a leaked token pre-rotation), amend `spec.md` P3 AC3 to drop "edit" and document that decision — do not leave the spec claiming coverage that doesn't exist.
   - **Priority**: Minor (the feature is fully usable without it — app owners can already delete+recreate — but the spec/implementation mismatch itself should not ship silently).

2. **WEBHOOK-19 / P2 delivery-log AC2 — UI expand-and-display behavior is untested.** The backend correctly returns `raw_payload`/`error_detail` per delivery (proven by `webhooks_handler_test.go:494-545`), and the frontend component (`Webhooks.tsx:298-338`, `WebhookDeliveryLog`) implements the click-to-expand UI. But neither the Playwright e2e spec nor any component test clicks a delivery row and asserts the expanded raw payload / error text actually renders — `webhooks.spec.ts` only checks that outcome badges are visible, never expands an entry. `tasks.md`'s own Test Co-location Validation table acknowledged this row as "merged forward" into T17, but T17 doesn't actually exercise the expand interaction.
   - **Fix task**: Add one Playwright step to `webhooks.spec.ts` — click a `write_error` delivery row, assert its raw payload JSON and `error_detail` text ("boom"-equivalent for the real write_error case already produced in the test) are visible.
   - **Priority**: Minor.

3. **Edge case — GET-configured webhook with empty query params is not test-covered** (manually verified correct by code reading, `internal/server/webhook_handler.go:356-365`). Not a defect, but a coverage gap: no test creates a GET-method webhook and posts to it at all.
   - **Fix task**: Add one integration test creating a `GET`-method webhook and confirming a call with no/some query params captures correctly (mirrors the existing POST capture tests).
   - **Priority**: Minor.

**Next steps**: Route the 3 gaps above back as fix tasks if the team wants full closure before calling this feature done; none are blocking for demoing or shipping the P1-P3 functionality itself, since the core AC set (23 of 23 requirement rows' primary behavior) is genuinely implemented and independently verified.

---

## Addendum: `cf58718..HEAD` — post-PASS changes and Opus review

**Added**: 2026-08-11. **Author**: the implementing agent for the changes below, *not* an independent Verifier — this addendum documents what changed and its evidence, but does not carry the same author≠verifier guarantee as the PASS above. Recorded here (M4 finding, Opus review) instead of leaving `cf58718..HEAD` with no paper trail at all; an independent Verifier pass over this same range is still recommended before the next release cut.

Everything below landed after the PASS, driven either by real Slack integration testing, a direct user request, or a follow-up Opus-model review (`d662b5e..HEAD`, agent `aad6b36fb75db39af`) that found 3 blockers + 6 important + 9 minor issues (this addendum covers the fixes; the review's own findings are cited inline).

**User-driven / bug-fix changes** (not independently re-verified, but each shipped with its own new/updated test, listed):
- Provider verification-challenge echo (WEBHOOK-24, added to spec.md this session) — `internal/server/webhook_handler.go` `verificationChallenge`/`HandleWebhookDelivery`; test `TestWebhookDelivery_VerificationChallengeEchoedBeforeCapture`.
- Token storage switched hash→encryption (design.md Tech Decisions updated this session) — `internal/dashboard/webhooks_store.go`, `internal/crypto/aes.go`; tests in `webhooks_store_test.go`/`webhooks_handler_test.go`/`internal/crypto/aes_test.go`.
- Event-mapping list/save response was missing `json` tags (same bug class as the delivery-log fix the original PASS covered) — `internal/dashboard/webhooks_handler.go` `mappingResponse`/`toMappingResponse`; test `TestListEventMappingsHandler_ReturnsSnakeCaseFields`. Distilled as lesson L-013.
- Saved-mapping UI now shows every `field_mappings` entry, not just the event-type/action/table summary — `WebhookMappingEditor.tsx`; e2e assertion added to `webhooks.spec.ts`.

**Opus-review fixes (blockers, all fixed same session)**:
- **B1**: webhook token was logged verbatim in `/hooks/{webhookId}/{token}` request logs (zap + dashboard `RingBuffer`), readable by the `auditor` role — fixed via `redactWebhookToken`/`isWebhookPath` in `internal/server/server.go`; body capture skipped entirely for this route.
- **B2**: public delivery route had no rate limit or body-size cap — fixed with a 120 req/min per-IP limiter and `http.MaxBytesReader` (1 MiB) in `internal/server/webhook_handler.go`.
- **B3**: dedup (`HasProcessedEventID`) counted `write_error`/`row_not_found` as "processed," permanently blocking a provider's expected retry — fixed by filtering on a `processedOutcomes` allowlist; test `TestWebhookActive_RetryAfterWriteErrorIsNotDeduped`.

**Opus-review fixes (important, 5 of 6 fixed same session — I2-I6; I1 also fixed)**:
- **I1**: match-key lookup had no uniqueness check — fixed with `LIMIT 2` + new outcome `ambiguous_match` (409); test `TestWebhookActive_UpdateWithAmbiguousMatchKeyIsRejected`. Documented in design.md's Error Handling Strategy this session.
- **I2**: design.md's promised RLS-policy warning was never implemented — fixed in `WebhookMappingEditor.tsx` via `useTablePolicies`.
- **I3**: token encryption reused `GOOGLE_OAUTH_ENCRYPTION_KEY`, fell back to a zero-byte key if unset — fixed with a dedicated `WEBHOOK_TOKEN_ENCRYPTION_KEY` that fails loudly instead; documented in README (all 4 languages) + `.env.example`.
- **I4**: no polling while a webhook awaited its first sample — fixed with a conditional `refetchInterval` in `useWebhooks`.
- **I5**: `SaveEventMapping` accepted an empty `event_type_value`/`field_mappings` — fixed with `ErrEventTypeValueRequired`/`ErrFieldMappingsRequired`; tests `TestSaveEventMapping_EmptyEventTypeValueRejected`/`TestSaveEventMapping_EmptyFieldMappingsRejected`.
- **I6**: README (4 languages) still listed webhooks as "planned" — fixed, roadmap synced.

**Gate at time of this addendum**: `go build`/`go vet`/`gofmt` clean, full `go test ./...` green against a disposable Postgres 16 container, `tsc -b`/`npm run build` clean, i18n key parity clean (en/pt-BR). Not re-run: the discrimination-sensor mutation test and a fresh Playwright e2e pass against a live server — both are what a real Verifier pass would add over this addendum.

**M1-M9 (minor findings) — all fixed in this same addendum session**, none independently re-verified (same caveat as above — self-authored, not a fresh Verifier pass):
- **M1**: `design.md` updated — token storage description (Data Models, Components, Risks & Concerns, Tech Decisions) now says AES-256-GCM encryption, with the original SHA-256-hash text struck through and kept visible for history.
- **M2**: WEBHOOK-24 added to `spec.md`'s traceability table and to `design.md`'s Error Handling Strategy for the verification-challenge echo; covered by the existing `TestWebhookDelivery_VerificationChallengeEchoedBeforeCapture`.
- **M3**: `spec.md` P3 AC3 amended with a strikethrough on "edit" plus an explicit gap note (option (b) from this file's own fix-task suggestion above) — traceability no longer claims coverage that doesn't exist.
- **M4**: this addendum itself, plus the stale `TokenHash`/`hashWebhookToken` references above replaced with a footnote pointing here.
- **M5**: delivery outcome badges and mapping action labels now go through `t()` (`webhooks.outcome.*`, `webhookMapping.action.*`, both locales) instead of rendering the raw enum value; `webhooks.spec.ts` updated to assert the translated (English) labels.
- **M6**: added a raw-bytes assertion for `"field_mappings"`/`"source_path"`/`"column"` in `TestListEventMappingsHandler_ReturnsSnakeCaseFields`, matching the existing `event_type_value` check.
- **M7**: the verification-challenge delivery now persists only `{"challenge": "..."}`, not the full inbound payload — a Slack-style legacy verification `token` field in the same request no longer sits in `webhook_deliveries` for 30 days. New assertion in `TestWebhookDelivery_VerificationChallengeEchoedBeforeCapture` confirms the token is absent and the challenge value is still recorded.
- **M8**: `WebhookMappingEditor`'s sample-fields/table-columns grid is now `grid-cols-1 sm:grid-cols-2` instead of a fixed 2-column grid.
- **M9**: `internal/query`'s cast helper exported as `query.PgCast`; `matchColumnCast` in `internal/server/webhook_handler.go` now calls it instead of duplicating the switch.

Gate re-run after M1-M9: `go build`/`go vet`/`gofmt` clean, full `go test ./...` green (disposable Postgres), `tsc -b`/`npm run build` clean, i18n key parity clean.

---

# Addendum 2 — Independent Verifier pass over `cf58718..HEAD` + uncommitted `UpdateWebhook`

**Date**: 2026-08-11
**Diff range**: `cf58718..e2224be` (13 commits on `develop`) **plus** the uncommitted working-tree diff implementing `UpdateWebhook` (PATCH), 11 modified files
**Verifier**: independent sub-agent — did not author any of the code under review (the range above was previously *self*-verified by its implementer; Addendum 1's "not re-run" caveat is what this pass closes)
**Method**: diff read → spec anchoring → real mutation testing in an isolated `git worktree` (`/tmp/verifier-scratch-webhooks`, branch `verifier-scratch-webhooks`, with the uncommitted diff applied via `git apply`) against a disposable `postgres:16-alpine` container. Worktree, branch and container were removed afterward; the real working tree's uncommitted `UpdateWebhook` diff was never touched (`git status --porcelain` before == after: the same 11 `M` entries, nothing added or lost).

## Verdict by area

| Area | Verdict | Basis |
| --- | --- | --- |
| B3 (dedup outcome allowlist) | ✅ PASS | Mutant killed; test asserts the spec outcome (retry after `write_error` writes exactly 1 row, `[inserted, write_error]` newest-first) |
| I1 (ambiguous match key → 409 `ambiguous_match`) | ✅ PASS | Mutant killed; test asserts 409 + neither row mutated + `outcome=ambiguous_match` |
| I3 (dedicated `WEBHOOK_TOKEN_ENCRYPTION_KEY`) | ✅ PASS | 4 tests incl. key-independence and loud failure when both vars unset (`internal/crypto/aes_test.go:42,69`); documented in `README.md:469` + all 3 translations + `.env.example:18` |
| I5 (empty `event_type_value`/`field_mappings`) | ✅ PASS | Both mutants killed |
| M7 (minimal challenge persistence) | ✅ PASS | Mutant killed (persisting `rawPayload` fails the "provider token not stored" assertion) |
| M5 (i18n for delivery outcomes) | ✅ PASS | All 12 `webhooks.outcome.*` keys exist in both locales and match the DB `webhook_deliveries_outcome_check` list exactly; en/pt-BR key parity 901/901 |
| M9 (`query.PgCast` dedup) | ✅ PASS | Behaviour-preserving; covered indirectly by existing uuid-match-key update/delete tests |
| `UpdateWebhook` — store + handler behaviour | ✅ PASS | 4 of 5 mutants killed (method validation, token preservation, audit entry); see gaps F5-F8 for what isn't covered |
| **B1 (token must not reach logs)** | ❌ **FAIL** | Regression on the dashboard side (F1) **and** zero test coverage — both mutants survived (F2) |
| **B2 (rate limit + body cap on public route)** | ❌ **FAIL (coverage)** | Implementation is present and correct by inspection, but has no test at all — both mutants survived (F3) |
| I2 (mapping-editor RLS-policy warning) | ❌ **FAIL (correctness)** | Warns on the wrong policy action under soft-delete, and ignores the `select` the match-key lookup needs (F4); no test |
| I4 (capture-sample polling) | ⚠️ PASS with note | Works as designed; polls indefinitely (F10) |
| I6 / M1-M4, M6, M8 (docs, spec sync, responsive grid) | ✅ PASS | READMEs synced in all 4 languages; `spec.md` P3 AC3 + WEBHOOK-23 traceability now match the shipped `UpdateWebhook`; `CHANGELOG.md` entry present under `[Unreleased]` |

## Discrimination sensor (mutation testing) — results

Baseline before mutating: `go build ./...` clean, `go vet ./internal/...` clean, `gofmt -l` clean on all changed Go files, `go test -p 1 ./...` fully green (webhook tests confirmed *running*, not skipping — 20 `--- PASS` lines under `-run TestWebhook` in `internal/server`), `npx tsc -b` + `npm run build` clean.

| # | Mutant (behaviour-level fault injected) | Target | Result |
| --- | --- | --- | --- |
| 1 | B1a: `logPath := r.URL.Path` (revert token redaction) | `internal/server/server.go:358` | ❌ **SURVIVED** |
| 2 | B1b: drop `&& !isWebhookPath(...)` (log req/res bodies for `/hooks/`) | `internal/server/server.go:380` | ❌ **SURVIVED** |
| 3 | B2a: unregister the per-IP rate limiter on `/hooks/{webhookId}/{token}` | `internal/server/server.go:160-161` | ❌ **SURVIVED** |
| 4 | B2b: remove `http.MaxBytesReader(..., maxWebhookBodyBytes)` | `internal/server/webhook_handler.go:45` | ❌ **SURVIVED** |
| 5 | B3: add `write_error`,`row_not_found` back into `processedOutcomes` | `internal/dashboard/webhook_deliveries_store.go:119` | ✅ KILLED — `TestWebhookActive_RetryAfterWriteErrorIsNotDeduped` |
| 6 | I1: delete the `len(matchedIDs) > 1 → errWebhookAmbiguousMatch` branch | `internal/server/webhook_handler.go:316-318` | ✅ KILLED — `TestWebhookActive_UpdateWithAmbiguousMatchKeyIsRejected` |
| 7 | I1b: remove `LIMIT 2` only (keep the Go-side check) | `internal/server/webhook_handler.go:296` | ⚪ SURVIVED **correctly** — `LIMIT 2` is a query-cost bound, not the safety mechanism; ambiguity is detected in Go via `pgx.CollectRows`. Not a finding. |
| 8 | M7: log `rawPayload` instead of `minimal` on the challenge path | `internal/server/webhook_handler.go:~100` | ✅ KILLED — `TestWebhookDelivery_VerificationChallengeEchoedBeforeCapture` |
| 9 | I5: remove `len(def.FieldMappings) == 0` rejection | `internal/dashboard/webhooks_store.go:466` | ✅ KILLED — `TestSaveEventMapping_EmptyFieldMappingsRejected` |
| 10 | I5b: neuter the blank-`event_type_value` rejection | `internal/dashboard/webhooks_store.go:455` | ✅ KILLED — `TestSaveEventMapping_EmptyEventTypeValueRejected` |
| 11 | UW-a: remove `isValidWebhookMethod` from `UpdateWebhook` | `internal/dashboard/webhooks_store.go` (`UpdateWebhook`) | ✅ KILLED — `TestUpdateWebhook_InvalidMethodRejected` + `TestUpdateWebhookHandler_InvalidMethodReturns400` |
| 12 | UW-b: edit also overwrites `token_secret` | `internal/dashboard/webhooks_store.go` (UPDATE statement) | ✅ KILLED — `TestUpdateWebhook_ChangesNameMethodAndPaths` + `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` |
| 13 | UW-c: `RowsAffected() == 0` guard never fires | `internal/dashboard/webhooks_store.go` | ⚪ SURVIVED — behaviourally equivalent (the follow-up `GetWebhookByID` also returns `ErrWebhookNotFound`), so the guard is redundant rather than untested. See F8. |
| 14 | UW-d: drop the `webhook.update` audit call | `internal/dashboard/webhooks_handler.go` | ✅ KILLED — `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` |
| 15 | UW-e: neuter the `name`/`event_type_path` required check (line 183, `UpdateWebhook`) | `internal/dashboard/webhooks_handler.go:183` | ❌ **SURVIVED** |
| 16 | UW-e2: same mutant on `CreateWebhook`'s identical guard (line 133) | `internal/dashboard/webhooks_handler.go:133` | ❌ **SURVIVED** (pre-existing gap, same bug class) |
| 17 | UW-f: PATCH route unregistered / wrong verb | `internal/server/server.go:189` | ❌ **SURVIVED** |

**Score: 8 killed / 6 survived-as-findings / 3 survived-benignly.**

## Findings (ranked)

### F1 — ❌ Blocker-class regression: B1's token leak is reopened through the dashboard API response body

`internal/dashboard/webhooks_handler.go:43` now puts the decrypted plaintext `token` on **every** webhook response (list/get/create/rotate/update) — an intentional design change (commit `399b006`), but `logMiddleware` captures dashboard-API response bodies into the same `RingBuffer` that B1 was about:

- `internal/server/server.go:380` excludes body capture only for paths starting with `/hooks/` (`isWebhookPath`), so `GET /dashboard/api/apps/{id}/webhooks` **is** captured (`isTextContent("")` returns `true` for a GET with no `Content-Type` — `internal/server/server.go:419-421`).
- `internal/dashboard/logs.go:139-146` — `ExtractApp("/dashboard/api/...")` yields `"dashboard"`, and `internal/dashboard/apps_store.go:417-419` returns a `nil` (= unfiltered) allow-list for `superadmin` **and any `CanReadAnyApp` role, which includes global `auditor`**. So `GET /dashboard/api/logs` serves those entries verbatim.
- Meanwhile `webhookRBACGate` (`internal/dashboard/webhooks_handler.go`, `CanManage`) denies `auditor` any direct read of webhooks. Net effect: a role explicitly forbidden from reading webhooks can read live webhook tokens out of the request log — the exact scenario B1 was filed for.

**Suggested fix**: widen the body-capture exclusion to the webhook dashboard endpoints (e.g. skip `ResBody` when the path matches `/dashboard/api/apps/*/webhooks*`), or redact a `"token":"…"` value in captured bodies, or stop returning the token on list/get and add an explicit reveal endpoint. Whichever is chosen, add the test F2 asks for.

### F2 — ❌ B1 has no test at all (mutants 1 & 2 survived)

`redactWebhookToken` / `isWebhookPath` (`internal/server/server.go:398-411`) are referenced only by production code — no test file mentions either. Both reverts pass the whole suite.
**Suggested fix**: a table test for `redactWebhookToken` (`/hooks/abc/tok` → `/hooks/abc/***`, non-webhook paths unchanged, short paths unchanged) plus a `logMiddleware` test asserting the pushed `LogEntry` for a `/hooks/…` request has a redacted `Path`, empty `ReqBody` and empty `ResBody`.

### F3 — ❌ B2 has no test at all (mutants 3 & 4 survived)

Neither the 120 req/min per-IP limiter (`internal/server/server.go:160-161`) nor the 1 MiB cap (`internal/server/webhook_handler.go:41-46`) is exercised. `TestServerHooksRouteRegistered` only checks the route resolves.
**Suggested fix**: a `newRouter`-level test firing 121 requests at `/hooks/{id}/{token}` from one `RemoteAddr` and asserting the last one is `429`; and a delivery test posting >1 MiB and asserting a `4xx` (plus that no oversized payload lands in `webhook_deliveries`).

### F4 — ❌ I2's RLS-policy warning targets the wrong policy action

`WebhookMappingEditor.tsx` warns when no `table_policies` row has `p.action === action && p.roles.includes("webhook")`. Two inaccuracies:

1. With soft delete enabled, a `delete` mapping issues an **UPDATE** (`internal/query/builder.go:336-346` via `query.BuildDelete`), so the policy actually required is `update`. The editor therefore shows "OK" for a webhook that will still `500` at delivery time (false negative), or nags for a `delete` policy that Postgres never consults (false positive).
2. Both `update` and `delete` mappings first run a `SELECT` for the match-key lookup under `RLSClaims{Role: "webhook"}` (`internal/server/webhook_handler.go:292-305`), which needs a `select` policy for the `webhook` role; the warning never mentions it. Note the same RLS scoping means an invisible duplicate row degrades `ambiguous_match` to `row_not_found` — worth documenting in `design.md`.

There is no test (unit or e2e) for this warning at all.
**Suggested fix**: derive the required action from the system's soft-delete setting (`delete` → `update` when soft delete is on) and also require a `select` policy for non-insert mappings; add an e2e or component test.

### F5 — ❌ The new `PATCH` route registration is untested (mutant 17 survived)

`internal/server/server.go:189` is the only thing wiring `UpdateWebhook` into the app; renaming the pattern or switching the verb passes every test. The repo already has this pattern — `internal/server/server_test.go:79` `TestServerUpdateTablePolicyRouteRegistered`.
**Suggested fix**: mirror that test for `PATCH /dashboard/api/apps/{id}/webhooks/{webhookId}`.

### F6 — ❌ Required-field validation is untested on both `UpdateWebhook` and `CreateWebhook` (mutants 15 & 16 survived)

`internal/dashboard/webhooks_handler.go:133` and `:183` (`body.Name == "" || body.EventTypePath == ""` → 400) can both be neutered with a green suite. The frontend validates client-side, so the server-side guard is the only defence for direct API callers.
**Suggested fix**: two handler tests asserting `400` for `{"method":"POST","event_type_path":"eventType"}` (no name) and for a blank `event_type_path`.

### F7 — ⚠️ Spec-precision gap on P3 AC3: `status` and `captured_sample` preservation isn't asserted

`spec.md:122` states an edit touches "name, method, and event-shape paths only — token, status, and captured_sample are untouched." `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` asserts the token (mutant 12 confirms that assertion bites) but nothing asserts `status`/`captured_sample`. The UPDATE statement doesn't name those columns, so risk is low today; a future `SET status = …` slip would go unnoticed.
**Suggested fix**: activate the webhook (or store a sample) before the PATCH and assert both survive.

### F8 — ℹ️ `TestUpdateWebhook_UnknownIDReturnsNotFound` doesn't discriminate the `RowsAffected` guard

Mutant 13 shows the test passes with the guard disabled, because `GetWebhookByID(ctx, pool, "", webhookID)` returns `ErrWebhookNotFound` anyway. Behaviour is equivalent, so this is not a defect — recorded so the guard isn't mistaken for verified logic.

### F9 — ℹ️ `WEBHOOK_TOKEN_ENCRYPTION_KEY` is documented but unreachable via the Helm chart

`charts/zeep-orbit/templates/secret.yaml` builds `stringData` from a fixed key list (`DATABASE_URL`, `DASHBOARD_BOOTSTRAP_SECRET`, `GOOGLE_*`) and `deployment.yaml:56-58` mounts it with `envFrom.secretRef`, so a Helm-deployed install cannot set the new var (it silently falls back to `DASHBOARD_BOOTSTRAP_SECRET`). Same pre-existing gap as `GOOGLE_OAUTH_ENCRYPTION_KEY`, which I3 also documented in the same table.
**Suggested fix**: add both keys to `secret.yaml` + `values.yaml` (guarded by `{{- if }}`), or note in the README table that they're only settable outside the chart.

### F10 — ℹ️ `useWebhooks` polling has no stop condition

`refetchInterval` returns `3000` for as long as *any* webhook is `status === 'capture' && !captured_sample` (`internal/dashboard/ui/src/lib/api.ts`), so one abandoned capture-mode webhook makes the Webhooks tab poll every 3s indefinitely. Consider a bounded window or backoff.

## Gate check (run by this Verifier, in the scratch worktree, with the uncommitted diff applied)

| Gate | Result |
| --- | --- |
| `go build ./...` | ✅ clean |
| `go vet ./internal/...` | ✅ clean |
| `gofmt -l` on all changed Go files | ✅ clean |
| `go test -p 1 -count=1 ./...` (disposable Postgres 16, `TEST_DATABASE_URL` + `DASHBOARD_BOOTSTRAP_SECRET` set) | ✅ fully green |
| `npx tsc -b` | ✅ clean |
| `npm run build` | ✅ clean |
| `en.json`/`pt-BR.json` JSON-valid + key parity | ✅ 901/901, no orphans; every `t()` key used in `Webhooks.tsx`/`WebhookMappingEditor.tsx` resolves |
| Real working tree untouched by the scratch work | ✅ `git status --porcelain` identical before/after (11 `M` entries, the `UpdateWebhook` diff intact) |
| Playwright e2e against a live server | ⚪ not run (no live stack in this environment) — `webhooks.spec.ts` changes reviewed statically only |

**Overall verdict: ❌ FAIL** — one blocker-class regression (F1), one correctness bug in a shipped fix (F4), and four real coverage gaps where a reintroduced bug goes undetected (F2, F3, F5, F6). Everything else in `cf58718..HEAD` plus the `UpdateWebhook` work verifies clean, with tests that demonstrably bite.

## Addendum 3 (2026-08-11, same day, self-fixed — not re-verified by an independent pass)

All findings F1-F7 from Addendum 2 above were fixed on top of the same uncommitted `UpdateWebhook` diff, by the agent that had implemented `UpdateWebhook` (i.e. self-fixed, not by a fresh Verifier — F1-F7's own fixes have the same author-equals-verifier limitation this feature has been trying to close):

- **F1 (blocker, fixed)**: added `isDashboardWebhookTokenPath` (`internal/server/server.go`) matching the dashboard's webhook list/create/get/update/delete/rotate-token endpoints (never mappings/deliveries, which carry no token), and excluded it from `logMiddleware`'s body-capture condition alongside the existing `isWebhookPath` check. Covered by `TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture` (table test, 6 cases) and `TestIsDashboardWebhookTokenPath` (`internal/server/server_test.go`).
- **F2/F3 (fixed)**: added direct tests — `TestIsWebhookPath`, `TestRedactWebhookToken` (`server_test.go`); `TestRateLimiter_AllowsUpToMaxThenBlocks`/`TestRateLimiter_SeparateIPsHaveIndependentBudgets` (`internal/dashboard/middleware_test.go`, new file); `TestWebhookDelivery_RateLimitedAfter120RequestsPerMinute` (hits the real router via `New()`, 121 sequential requests, asserts 429 only on the 121st) and `TestWebhookDelivery_OversizedBodyRejectedAsMalformed` (`internal/server/webhook_handler_test.go`).
- **F4 (fixed)**: `WebhookMappingEditor.tsx`'s policy-warning check now derives the actual Postgres commands the delivery path issues (`select` for update/delete's lookup; `insert`/`update`/`delete` for the write, with delete mapping to `update` when `useSystemConfig().soft_delete_enabled`, `delete` otherwise) instead of comparing against the mapping's action label directly. Not covered by an automated test in this pass — no existing test harness renders this component with mocked `useTablePolicies`/`useSystemConfig`; flagging as a residual gap rather than claiming coverage that doesn't exist.
- **F5 (fixed)**: `TestServerUpdateWebhookRouteRegistered` (`server_test.go`), same 401-proves-registered pattern as the sibling table-policy test.
- **F6 (fixed)**: `TestCreateWebhookHandler_MissingRequiredFieldsReturns400` and `TestUpdateWebhookHandler_MissingRequiredFieldsReturns400` (`internal/dashboard/webhooks_handler_test.go`).
- **F7 (fixed)**: `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` now seeds a captured sample before the PATCH and asserts both `Status` and `CapturedSample` survive unchanged.
- **F8, F9, F10**: left open (F8 is informational/no-defect; F9 and F10 are pre-existing/out of scope for this fix pass).

Gates re-run after these fixes (real working tree, disposable Postgres 16, `TEST_DATABASE_URL` + `DASHBOARD_BOOTSTRAP_SECRET` set): `go build ./...`, `go vet ./...`, `gofmt -l` on all changed files, `go test -p 1 -count=1 ./...` (all packages, including every new test above), `npx tsc -b`, `npm run build` — all clean.

**This addendum does not upgrade the overall verdict to PASS.** These fixes were not adversarially verified by an independent agent (no fresh mutation-testing pass confirming the new tests actually kill the F1-F7 mutants, and F4 has zero automated coverage). A genuine independent Verifier pass over the full `cf58718..HEAD` + uncommitted diff, post-fix, is still the recommended gate before this ships — carrying forward the same open item Addendum 1 already flagged for the B1-M9 range.

---

# Addendum 4 — Independent re-verification of F1-F7 (post-self-fix)

**Date**: 2026-08-11
**Scope**: the fixes Addendum 3 claims for F1-F7, checked against the *actual current code*, not Addendum 3's prose.
**Verifier**: independent sub-agent — did not author `UpdateWebhook`, the F1-F7 fixes, or any code in this feature. This closes the author≠verifier gap Addendum 3 explicitly left open.
**Diff state**: still uncommitted (16 working-tree entries incl. the new untracked `internal/dashboard/middleware_test.go`). Nothing was committed, staged, or modified in the real tree by this pass.
**Method**: code read of the real runtime paths → mutation testing in an isolated `git worktree` (`/tmp/verifier-scratch-webhooks-2`, branch `verifier-scratch-webhooks-2`, working-tree diff applied via `git apply`, `internal/dashboard/static` copied in to satisfy the `embed` directive) against a disposable `postgres:16-alpine` on port 55443. 14 mutants injected. Worktree, branch and container removed afterward; `git status --porcelain` byte-identical before and after.

## Per-fix verdict

| Fix | Verdict | Evidence |
| --- | --- | --- |
| **F1** — dashboard webhook-token paths excluded from log body capture | ✅ **PASS** | `isDashboardWebhookTokenPath` (`internal/server/server.go:410-425`) covers exactly the 5 `toWebhookResponse` call sites (`webhooks_handler.go:151` create, `:206` update, `:225` list, `:241` get, `:272` rotate) — verified by enumerating call sites directly, not from the addendum. `ActivateWebhook` (`:483`) returns `{"status":"active"}` only, `ListWebhookDeliveries`/`ListEventMappings` carry no token, and all three are correctly *not* suppressed (`len(parts)==8` with `parts[7] != "rotate-token"` → false; `mappings/{mappingId}` len 9 → false). Wired at `server.go:380`. 4 mutants killed (M1, M13, M14 incl. an over-broad mutant that would have suppressed mappings/deliveries). |
| **F2** — `redactWebhookToken`/`isWebhookPath` tested | ⚠️ **PARTIAL PASS** | The pure functions are genuinely covered (M3 `redactWebhookToken` no-op → KILLED; `TestIsWebhookPath`/`TestIsDashboardWebhookTokenPath` present). But the **wiring** is not: see F11 below — reverting `logPath := redactWebhookToken(r.URL.Path)` to `logPath := r.URL.Path` (Addendum 2's original mutant #1, the actual B1 leak) still passes the whole suite. |
| **F3** — rate limit + body cap tested | ⚠️ **PARTIAL PASS** | Rate limiter: real coverage. M4 (unwrap `webhookLimiter.Middleware` from `server.go:161`) → KILLED; M11 (`e.count <= rl.max+1`) → KILLED; M12 (shared bucket across IPs) → KILLED. Body cap: **theater** — see F12. |
| **F4** — RLS policy-action derivation | ❌ **FAIL (still incorrect, and still untested)** | The soft-delete branch is now right, but two new defects remain — see F13 and F14. Confirmed by reading `dispatchInsert`/`dispatchUpdateOrDelete` (`internal/server/webhook_handler.go:380-410`, `:278-353`), `query.BuildInsert/BuildUpdate/BuildDelete` (`internal/query/builder.go:257,310,335-358`) and by an empirical Postgres 16 check. |
| **F5** — PATCH route registration tested | ✅ **PASS** | M6 (rename path to `.../{webhookId}/edit`) → KILLED; M7 (`Patch` → `Put`) → KILLED. `TestServerUpdateWebhookRouteRegistered` bites on both path and verb. |
| **F6** — required-field validation tested | ✅ **PASS** | M8 (delete the `body.Name == "" \|\| body.EventTypePath == ""` guard in `UpdateWebhook`) → KILLED; M9 (same guard in `CreateWebhook`) → KILLED. Both tests assert 400 for blank name *and* blank `event_type_path`. |
| **F7** — `Status`/`CapturedSample` preservation asserted | ✅ **PASS** | Real precondition: `StoreCapturedSample` is called before the PATCH (`webhooks_handler_test.go:310`), then `resp.Status == "capture"` and `resp.CapturedSample["a"] == 1` are both asserted. M10 (`SET ... status = 'capture', captured_sample = NULL` added to the UPDATE) → KILLED. |

## Mutation sensor — 14 mutants

Baseline in the scratch worktree: `go build ./...` clean, `go test -p 1 -count=1 ./internal/server/... ./internal/dashboard/...` green, all 12 target tests confirmed **running** (not skipping) via `-v`. `tsc -b` clean using the repo-pinned compiler (`npx` in the bare worktree resolves a newer TypeScript that rejects `baseUrl` — toolchain artifact, not a code defect).

| # | Mutant | Target | Result |
| --- | --- | --- | --- |
| M1 | Drop `!isDashboardWebhookTokenPath(...)` from the capture condition | `server.go:380` | ✅ KILLED — `TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture` |
| M2 | `logPath := r.URL.Path` (revert token redaction wiring) | `server.go:358` | ❌ **SURVIVED** → F11 |
| M3 | `redactWebhookToken` returns the path unredacted | `server.go:428` | ✅ KILLED — `TestRedactWebhookToken` |
| M4 | Unwrap `webhookLimiter.Middleware` from the `/hooks/` route | `server.go:161` | ✅ KILLED — `TestWebhookDelivery_RateLimitedAfter120RequestsPerMinute` |
| M5 | Remove `http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)` | `webhook_handler.go:45` | ❌ **SURVIVED** → F12 |
| M6 | Rename the PATCH route pattern | `server.go:191` | ✅ KILLED |
| M7 | `Patch` → `Put` on the update route | `server.go:191` | ✅ KILLED |
| M8 | Delete `UpdateWebhook`'s required-field guard | `webhooks_handler.go:183` | ✅ KILLED |
| M9 | Delete `CreateWebhook`'s required-field guard | `webhooks_handler.go:133` | ✅ KILLED |
| M10 | UPDATE also sets `status = 'capture', captured_sample = NULL` | `webhooks_store.go:342` | ✅ KILLED |
| M11 | Rate limiter off-by-one (`<= rl.max+1`) | `middleware.go:96` | ✅ KILLED — `TestRateLimiter_AllowsUpToMaxThenBlocks` |
| M12 | Rate limiter collapses all IPs into one bucket | `middleware.go:86` | ✅ KILLED — `TestRateLimiter_SeparateIPsHaveIndependentBudgets` |
| M13 | `isDashboardWebhookTokenPath` stops excluding `rotate-token` | `server.go:421` | ✅ KILLED |
| M14 | `isDashboardWebhookTokenPath` returns `true` for *everything* under `/webhooks` (over-broad: would also suppress mappings/deliveries) | `server.go:416` | ✅ KILLED |

**Score: 12 killed / 2 survived-as-findings.** Addendum 2's F1/F5/F6/F7 mutants (its #1-part, #15, #16, #17, #12-adjacent) are now all killed; two of its F2/F3 mutants are still alive.

## New findings

### F11 — ❌ The B1 path-redaction wiring is still untested (Addendum 2's mutant #1 survives)

`internal/server/server.go:358` — `logPath := redactWebhookToken(r.URL.Path)` can be reverted to `logPath := r.URL.Path` and the entire suite still passes. `TestRedactWebhookToken` covers the helper in isolation; `TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture` only asserts `ResBody`, never `LogEntry.Path`. The plaintext token in the URL reaching both zap and the `RingBuffer` *is* B1's actual leak, and it remains the one unguarded link. Addendum 2's F2 suggested fix asked for exactly this assertion; it wasn't added.
**Fix**: in the existing table test, add `wantPath` per case and assert `last.Path` — `/hooks/wh1/plaintext-token-in-url` → `/hooks/wh1/***`, dashboard paths unchanged. ~5 lines, kills M2.

### F12 — ❌ The 1 MiB body-cap test passes for the wrong reason (mutant survives)

`internal/server/webhook_handler_test.go:319` builds the oversized body as `make([]byte, maxWebhookBodyBytes+1)` — all zero bytes, which is **invalid JSON**, so the request is rejected `400 malformed` whether or not the cap exists. Proven both ways in the scratch worktree with a throwaway test using a >1 MiB *valid* JSON body:
- cap present → `400 {"error":"malformed request body"}`
- cap removed → **`200 {"status":"captured"}`** — the oversized payload lands in `captured_sample`

So B2's cap is functionally correct (good news), but the test protecting it is inert.
**Fix**: build the payload as valid JSON, e.g. `` `{"eventType":"x","pad":"` + strings.Repeat("a", maxWebhookBodyBytes) + `"}` ``. One-line change; the existing 400 + `outcome=malformed` assertions then bite.

### F13 — ❌ F4 still under-reports: `insert` mappings also need a `select` policy

`WebhookMappingEditor.tsx:103-105` returns `["insert"]` for an insert mapping. But `dispatchInsert` runs `query.BuildInsert` (`internal/query/builder.go:257`), which emits `INSERT … RETURNING *`, and Postgres applies **SELECT** policies to an `INSERT`/`UPDATE` `RETURNING` clause. Verified empirically on `postgres:16-alpine`, table with RLS enabled and an INSERT-only policy for the acting role:

```
INSERT INTO pub (v) VALUES ('a');            -- INSERT 0 1
INSERT INTO pub (v) VALUES ('b') RETURNING *; -- ERROR: new row violates row-level security policy
```

Net effect: the editor shows "OK" for an insert mapping on a table that has policies but no `select` policy for the `webhook` role, and every real delivery then logs `write_error`/500. Same false-negative class as the original F4, on the one branch the fix left alone. (`update` is unaffected — it already requires `select`.)
**Fix**: `case "insert": return ["insert", "select"]`.

### F14 — ❌ F4 false-negative when a table's policies were all deleted

`WebhookMappingEditor.tsx:120` short-circuits to "no warning" whenever `targetTablePolicies.length === 0`, on the stated assumption that "RLS only turns on once a table has at least one policy." That's true on *create* (`table_policies_store.go:142` `ALTER TABLE … ENABLE ROW LEVEL SECURITY`) but `DeleteTablePolicy` (`:222-247`) only `DROP POLICY` — it never disables RLS. A table that had policies and had them all removed keeps RLS enabled with zero policies = default-deny for every command, so *all* webhook deliveries fail while the editor reports no problem.
**Fix**: either have `DeleteTablePolicy` `DISABLE ROW LEVEL SECURITY` when it removes the last policy for a table (preferred — makes the frontend assumption true), or surface RLS-enabled state in the policies API and warn on zero-policies-but-RLS-on. Higher-risk (touches DDL) — confirm before applying.

### F15 — ℹ️ F4's warning still has zero automated coverage

Unchanged from Addendum 3's own admission. F13 and F14 are both defects a single component test with mocked `useTablePolicies`/`useSystemConfig` would have caught. There is no component-test harness in this repo today, so the realistic option is a Playwright e2e in `webhooks.spec.ts` (create a table, add only an `insert` policy for `webhook`, open the mapping editor, assert the warning names `select`).

### F16 — ℹ️ Delivery-log responses embed `raw_payload` and are captured into the log ring buffer

`isDashboardWebhookTokenPath` deliberately (and correctly, for F1's purpose) does not exclude `.../webhooks/{id}/deliveries`, whose `deliveryResponse.RawPayload` (`webhooks_handler.go:519`) is the provider's full inbound body. Those responses land in the `RingBuffer` and are readable by any `CanReadAnyApp` role including global `auditor` — the same audience F1 was closing off, for provider-side secrets rather than Zeep tokens. M7 already narrowed the verification-challenge case; the general case is a pre-existing exposure of the delivery log itself, not a regression, and out of F1's scope. Recorded so it isn't rediscovered as a new leak.

## Carried-forward, unchanged

F8 (informational, no defect), F9 (`WEBHOOK_TOKEN_ENCRYPTION_KEY` unreachable via the Helm chart), F10 (`useWebhooks` polls indefinitely) — all still open exactly as Addendum 2 described; Addendum 3 correctly scoped them out.

## Gate check (scratch worktree, uncommitted diff applied)

| Gate | Result |
| --- | --- |
| `go build ./...` | ✅ clean |
| `go test -p 1 -count=1 ./internal/server/... ./internal/dashboard/...` (disposable Postgres 16) | ✅ green |
| All 12 F1-F7 tests confirmed running, not skipping (`-v`) | ✅ |
| `tsc -b` (repo-pinned compiler) | ✅ clean |
| Real working tree untouched | ✅ `git status --porcelain` byte-identical before/after (16 entries) |
| Worktree + branch + Postgres container removed | ✅ |

## Verdict

**F1 PASS · F2 PARTIAL · F3 PARTIAL · F4 FAIL · F5 PASS · F6 PASS · F7 PASS.**

**Overall: ❌ FAIL** — but the shape has changed sharply for the better. Every *implementation* fix is correct except F4's insert branch (F13) and its zero-policy assumption (F14); the two partials (F11, F12) are inert-test problems with one-line fixes, not broken behaviour. Nothing here is a live token leak: F1 holds, and B2's cap works (F12 is only the test). The blocking item for a release cut is F13 — a shipped UI check that tells operators their configuration is fine when it isn't. F11 and F12 should land with it since they're trivial and they're what keeps B1/B2 from silently regressing a third time.

## Addendum 5 (2026-08-11, same day, self-fixed — not re-verified by an independent pass)

F11-F14 from Addendum 4 fixed, still on the same uncommitted diff, by the agent that made the prior round of fixes (same author-equals-verifier caveat as Addenda 3 and this note):

- **F11 (fixed)**: `TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture` now asserts `LogEntry.Path` per case, not just `ResBody` — `/hooks/wh1/plaintext-token-in-url` must log as `/hooks/wh1/***`, every other case unchanged. Reverting `redactWebhookToken(...)` back to the raw path now fails this test.
- **F12 (fixed)**: `TestWebhookDelivery_OversizedBodyRejectedAsMalformed`'s payload is now valid JSON (`{"eventType":"x","eventId":"y","padding":"aaa…"}`, padded past 1 MiB via `strings.Repeat`) instead of a zero-byte slice. Removing `MaxBytesReader` now flips the result to `200 {"status":"captured"}`, which the test's `400`+`outcome=malformed` assertions catch.
- **F13 (fixed)**: `WebhookMappingEditor.tsx`'s `insert` branch now returns `["insert", "select"]`, matching Postgres evaluating SELECT policies against `INSERT ... RETURNING *`.
- **F14 (fixed, different approach than suggested)**: rather than having `DeleteTablePolicy` `DISABLE ROW LEVEL SECURITY` on the last-policy delete — which Addendum 4 itself flagged as "higher-risk, touches DDL, confirm before applying" — the frontend check was changed to never skip at zero currently-saved policies. `missingPolicyActions` is now computed unconditionally (guarded only on "policy list has actually loaded yet", to avoid a loading-state flash), so a table with RLS re-enabled-by-history and zero policies now warns correctly. Trade-off: a table that genuinely never had RLS enabled (truly zero policies, truly unrestricted) now also shows the warning — a false positive, not a false negative. Chosen deliberately: for a security-relevant warning, over-warning on a harmless case is an acceptable cost against under-warning on a silent-500 case, and it ships without touching table DDL semantics that back every other RLS-dependent code path in this repo.
- **F15 (unchanged)**: F13/F14's fixes still have no automated coverage (no component-test harness in this repo) — same gap noted in Addendum 4, now covering more logic than before.

Gates re-run after these fixes (real working tree, disposable Postgres 16): `go build ./...`, `go vet ./...`, `gofmt -l` on all changed Go files, `go test -p 1 -count=1 ./...` (all packages, `-v` confirms every F1-F7/F11-F13 test actually runs), `npx tsc -b`, `npm run build` — all clean.

**This addendum does not upgrade the overall verdict to PASS.** F13/F14 (the two substantive logic fixes) and F11/F12 (the two test-quality fixes) were not adversarially mutation-tested by a fresh agent — same limitation Addendum 3 disclosed for F1-F7, now applying to this round too. A genuine independent Verifier pass over the full range, post-fix, is still the recommended gate before this ships.

---

# Addendum 6 — Independent re-verification of F11-F14 (post-Addendum-5 self-fix) + residual sweep

**Date**: 2026-08-11
**Scope**: (a) F11-F14 as Addendum 5 claims to have fixed them, checked against the *actual current code*; (b) a residual sweep for regressions introduced by four consecutive rounds of self-fixing on one uncommitted diff.
**Verifier**: independent sub-agent — did not author `UpdateWebhook`, the F1-F7 fixes, or the F11-F14 fixes. Third independent pass on this feature (after Addenda 2 and 4).
**Diff state**: still uncommitted, 16 working-tree entries. Nothing committed, staged, or modified in the real tree by this pass; `git status --porcelain` byte-identical before and after except this addendum.
**Method**: code read of the real runtime paths → mutation testing in an isolated `git worktree` (`/tmp/verifier-scratch-webhooks-3`, branch `verifier-scratch-webhooks-3`, diff applied via `git apply`, `internal/dashboard/static` + `ui/node_modules` copied in to satisfy `go:embed` and the pinned TS toolchain) against a disposable `postgres:16-alpine` on port 55445. Plus direct empirical Postgres 16 checks of the RLS semantics F13/F14 depend on, and a standalone Node reimplementation of `requiredPolicyActions`/`missingPolicyActions` to produce a full truth table (the component has no test harness). Worktree, branch and container removed afterward.

## Per-finding verdict

| Finding | Verdict | Evidence |
| --- | --- | --- |
| **F11** — `LogEntry.Path` asserted, redaction wiring guarded | ✅ **PASS** | `server_test.go:205-243` now carries `wantLoggedPath` per case and asserts `last.Path` (`:238`). Mutant **N1** — revert `server.go:358` to `logPath := r.URL.Path` — **KILLED**: `path "/hooks/wh1/plaintext-token-in-url": logged Path = "/hooks/wh1/plaintext-token-in-url", want "/hooks/wh1/***"`. Addendum 4's surviving M2 is now dead. |
| **F12** — oversized-body test payload is valid JSON | ✅ **PASS** | `webhook_handler_test.go:325` builds `{"eventType":"x","eventId":"y","padding":"aaa…"}` padded to `maxWebhookBodyBytes+1024` via `strings.Repeat`. Mutant **N2** — delete `r.Body = http.MaxBytesReader(...)` from `HandleWebhookDelivery` (`webhook_handler.go:45`) — **KILLED**: `expected 400 for an oversized body, got 200: {"status":"captured"}`. Addendum 4's surviving M5 is now dead, for the right reason. |
| **F13** — insert mapping requires `insert` **and** `select` | ✅ **PASS (logic correct)** / ⚠️ untested | `WebhookMappingEditor.tsx:104-109` returns `["insert", "select"]`. Requirement independently re-derived, not taken from the comment: `dispatchInsert` (`webhook_handler.go:381`) → `query.BuildInsert` → `builder.go:256-262` emits `INSERT … RETURNING *`, executed inside `WithRLSContext(RLSClaims{Role:"webhook"})` (`db/client.go:131` `SET LOCAL ROLE zeep_app_enduser`, role folded into the policy expression via `app.jwt_role`, `provisioner/policy.go:159`). Empirically on `postgres:16-alpine`, RLS on, insert-only policy: bare `INSERT` → `INSERT 0 1`; `INSERT … RETURNING *` → `ERROR: new row violates row-level security policy`; after adding a SELECT policy → succeeds. Mutant **N3** (`return ["insert"]`) survives `tsc -b` + `npm run build` — no test kills it (F15 unchanged). |
| **F14** — no zero-policy short-circuit | ✅ **PASS (logic correct, tradeoff sound)** / ⚠️ untested | `WebhookMappingEditor.tsx:129-133` computes unconditionally, guarded only on `targetTablePolicies` being defined. Premise re-verified empirically: with RLS enabled and *all* policies dropped, a bare `INSERT` fails (`new row violates row-level security policy`) — so zero policies is genuinely not "no restriction", and `DeleteTablePolicy` (`table_policies_store.go:216-222`) documents that it deliberately never disables RLS. Mutant **N4** (reintroduce `&& targetTablePolicies.length > 0`) survives — no test kills it. |

### The loading guard is sound (the specific subtler bug Addendum 5's approach could have had)

Checked directly, since a permanently-`undefined` policy list would silently re-suppress the warning and make F14's fix cosmetic:

- `ListTablePolicies` (`table_policies_store.go:196`) initializes `result := make([]TablePolicyRow, 0)`, and `handler.go:1365` `writeJSON`s it directly — so a table with zero policies serializes as `[]`, **never** `null`. `[]` is truthy in JS, so the guard lets the check run and the warning fires. ✅ correct.
- The guard therefore suppresses only three states: query in flight, query errored, or query disabled. `useTablePolicies` (`api.ts:234-241`) has `enabled: Boolean(appId) && Boolean(table)` — disabled only when `targetTable === ""`, i.e. the app has no tables, in which case no mapping can be saved at all. Non-issue.
- **Residual (new, minor — F18 below)**: on a *failed* policies fetch (403/network), `data` stays `undefined` forever and the security warning silently disappears with nothing surfaced to the user. Fail-open on an error path.

### Tradeoff assessment (F14)

Sound as chosen. The false positive (a truly-unrestricted table that never had RLS gets warned) costs an operator one glance at the Policies tab; the false negative it replaced (RLS-on, zero policies → every delivery 500s while the editor says "OK") costs a silently broken integration that is hard to diagnose. It also avoids the DDL change Addendum 4 itself flagged as higher-risk — `DeleteTablePolicy`'s "default-deny stays explicit" contract backs every other RLS-dependent path in the repo, and weakening it for a UI hint would have been the wrong trade. **But the warning's copy was not updated to match** — see F17.

## Mutation sensor — 4 mutants (F11-F14 targeted)

Baseline in the scratch worktree: `go build ./...` clean, `go vet ./internal/...` clean, `gofmt -l` clean on all 8 changed Go files, `go test -p 1 -count=1 ./...` fully green, all target tests confirmed **running** not skipping (`-v`), `tsc -b` + `npm run build` clean with the repo-pinned toolchain.

| # | Mutant | Target | Result |
| --- | --- | --- | --- |
| N1 | `logPath := r.URL.Path` (revert F11's redaction wiring) | `server.go:358` | ✅ KILLED — `TestLogMiddleware_ExcludesWebhookTokenPathsFromBodyCapture/public_hooks_route` |
| N2 | Remove `http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)` | `webhook_handler.go:45` | ✅ KILLED — `TestWebhookDelivery_OversizedBodyRejectedAsMalformed` (now flips 400 → 200) |
| N3 | `case "insert": return ["insert"]` (drop F13's `select`) | `WebhookMappingEditor.tsx:109` | ❌ SURVIVED — no automated coverage (F15) |
| N4 | Reintroduce `&& targetTablePolicies.length > 0` (revert F14) | `WebhookMappingEditor.tsx:129` | ❌ SURVIVED — no automated coverage (F15) |

**Score: 2 killed / 2 survived-for-lack-of-any-frontend-test.** Both Go findings from Addendum 4 are genuinely closed; both frontend findings are correct by construction and empirical Postgres checks, and remain **entirely unverified by automation** — stated plainly rather than dressed up.

### Frontend logic truth table (standalone Node reimplementation of `:102-134`)

Used in place of the component test this repo can't run. Confirms F14's fix changes behaviour on **exactly one** input class — the zero-policy case — and leaves every original F4 update/delete case identical:

| Policy state / action | current | N3 (F13 reverted) | N4 (F14 reverted) |
| --- | --- | --- | --- |
| loading (`undefined`) / insert | — | — | — |
| zero policies / insert | `insert, select` | `insert` | — (the F14 bug) |
| insert-only / insert | `select` | — (the F13 bug) | `select` |
| insert+select / insert | — | — | — |
| select+update / update | — | — | — |
| update-only / update | `select` | `select` | `select` |
| select+update / delete (soft on) | — | — | — |
| select+delete / delete (soft on) | `update` | `update` | `update` |
| select+delete / delete (soft off) | — | — | — |
| policies for another role only / insert | `insert, select` | `insert` | `insert, select` |

## Residual sweep (four rounds of self-fix on one diff)

| Check | Result |
| --- | --- |
| F14's removal of the length-gate changed the ORIGINAL F4 update/delete behaviour? | ✅ No. Truth table above: rows 5-9 are identical between current and N4. Only the zero-policy row moved. |
| `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` (F7) still passes? | ✅ Yes, plus the other 3 `TestUpdateWebhookHandler_*` (invalid method / missing fields / non-admin) — all 4 PASS, none skipped. |
| `isDashboardWebhookTokenPath` (F1) still matches every token-bearing response? | ✅ Re-enumerated from scratch: `toWebhookResponse` has exactly 5 call sites (`webhooks_handler.go:151,206,225,241,272` = create/update/list/get/rotate), and `webhookResponse.Token` (`:31`, populated at `:43,49`) is the only token in any dashboard response. All 5 route shapes (len 6 / 7 / 8-with-`rotate-token`, `server.go:188-193`) are matched by `server.go:410-425`; `activate`, `mappings`, `mappings/{id}` and `deliveries` correctly are not. |
| i18n key parity after 4 rounds of edits | ✅ `en.json` 901 / `pt-BR.json` 901, zero orphans either direction, both JSON-valid; every `t()` key used in `Webhooks.tsx` + `WebhookMappingEditor.tsx` resolves. |
| `spec.md` accuracy | ✅ P3 AC3 and the WEBHOOK-23 traceability row now describe the shipped `PATCH` endpoint, the `webhook.update` audit action, and the token/status/captured_sample-untouched contract that `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` actually asserts. Coverage line ("0 partial") matches. |
| `CHANGELOG.md` `[Unreleased]` accuracy | ✅ Entries for the edit endpoint, the broadened token-leak fix, and the RLS-warning semantics (incl. the `insert`+`select` and zero-policy reasoning) all match the code. One nit: it says the mapping form "checks this at save time" — the check renders live as the form changes, not at save. |
| Gates (`go build`, `go vet ./internal/...`, `gofmt -l` on 8 changed files, `go test -p 1 -count=1 ./...`, `tsc -b`, `npm run build`) | ✅ all clean/green |
| Playwright e2e against a live server | ⚪ not run (no live stack) — `webhooks.spec.ts` reviewed statically only, unchanged from Addendum 4's position |

## New findings

### F17 — ⚠️ The RLS warning's copy now contradicts its own trigger condition (both locales)

`en.json:883` / `pt-BR.json:883` — `webhookMapping.policyWarning` still reads *"Table "{{table}}" **has row policies**, but none grant the "{{action}}" action to the webhook role yet — real deliveries will fail until you add one…"*. After F14's fix the warning also fires when the table has **zero** policies, where "has row policies" is factually false, and — for a table that genuinely never had RLS enabled (the false positive F14 deliberately accepted) — "real deliveries will fail" is also false. The one case the copy was written for is now a subset of when it shows.
**Fix**: reword to state the requirement rather than assert the table's state, e.g. *"Table "{{table}}" needs a row policy granting "{{action}}" to the webhook role, or real deliveries may fail — add one on the Policies tab."* Both locales, same change. Low risk, no logic change.

### F18 — ℹ️ The RLS warning fails open when the policies query errors

`WebhookMappingEditor.tsx:129-133` — the `targetTablePolicies ? … : []` guard is correct for the loading state (see above), but `useTablePolicies` has no `onError`/error surface here, so a 403 or network failure leaves `data === undefined` permanently and the security warning silently never appears. Contrast `AGENTS.md` §5 ("a mutation hook without `onError` is an incomplete hook") — the same reasoning applies to a query whose absence silently disables a safety hint.
**Fix**: read `isError` from the hook and either show the warning unconditionally in that state or render a distinct "couldn't check policies" note.

### F19 — ❌ Pre-existing, out of this diff, but it blocks the remediation F13's warning tells operators to perform: `insert` table policies cannot be created at all

`internal/provisioner/policy.go:163-167` (**not touched by this diff**) emits, for an insert policy, `CREATE POLICY … FOR INSERT TO zeep_app_enduser USING (…) WITH CHECK (…)`. Postgres rejects `USING` on an INSERT policy. Verified two ways on `postgres:16-alpine`: printing the generated SQL through `BuildPolicySQL` and executing it →
```
ERROR:  only WITH CHECK expression allowed for INSERT
```
`TestBuildPolicySQL_InsertHasWithCheck` (`policy_test.go:63-79`) only asserts the string *contains* `WITH CHECK`; nothing in the suite ever executes an insert-policy DDL, so the whole suite is green while the feature is broken. Consequence for this feature: an operator correctly warned "this table needs `insert` and `select` policies for the webhook role" can create the `select` one and gets a 500 on the `insert` one — so **insert mappings are unusable on any RLS-enabled table**, and F13's now-correct warning is only half-actionable.
**Fix**: in `buildPolicySQL`, emit `USING` only for `select`/`update`/`delete` and `WITH CHECK` only for `insert`/`update`; add a store-level test that actually executes the DDL for each of the 4 actions against Postgres. Outside the inbound-webhooks diff — route as its own fix task, but it is a real blocker for the insert-mapping path this feature ships.

## Carried-forward, unchanged

F8 (informational, no defect), F9 (`WEBHOOK_TOKEN_ENCRYPTION_KEY` unreachable via the Helm chart), F10 (`useWebhooks` polls indefinitely), F15 (**frontend policy-warning logic still has zero automated coverage** — now guarding more logic than when it was filed), F16 (delivery-log responses embed `raw_payload` into the log ring buffer, pre-existing). All still open exactly as previously described.

## Verdict

**F1 PASS · F2 PASS (closed by F11's fix) · F3 PASS (closed by F12's fix) · F4 PASS (closed by F13+F14, logic verified against Postgres) · F5 PASS · F6 PASS · F7 PASS · F11 PASS · F12 PASS · F13 PASS · F14 PASS.**

**Overall: ✅ PASS (conditional on nothing — F17/F18 are polish, F19 is out-of-diff).** Every finding from Addenda 2 and 4 that was in scope for the fix rounds is now genuinely closed, and for the first time the two Go fixes are backed by tests that demonstrably bite (N1, N2 both killed). No regression was introduced by the four self-fix rounds: the F14 change is behaviour-identical to the prior code on every input except the zero-policy case it was meant to fix, F7's assertions still hold, F1's path list still matches every token-bearing response, and i18n/spec/CHANGELOG are internally consistent.

Two honest caveats, neither blocking:
1. **The frontend RLS-warning logic (F13/F14) has no automated test** — its correctness here rests on this Verifier's static trace plus empirical Postgres checks, not on a test that would catch the next regression. That is the single highest-value follow-up (F15).
2. **F19 is a real functional blocker for the insert-mapping-on-RLS-table path**, caused by a pre-existing provisioner bug outside this diff. This feature is shippable without fixing it (insert mappings work on tables with no policies at all; update/delete mappings are unaffected), but the combination — a warning telling operators to add an insert policy, and an endpoint that 500s when they try — should not stay open long.

F8, F9, F10, F15, F16, F17, F18, F19 remain open; none is a defect in the code this diff changes, except F15's absence of coverage.

## Addendum 7 (2026-08-11, same day, self-fixed — not re-verified by an independent pass)

F17, F18, F19 fixed on top of the same working tree (same author-equals-verifier caveat as every prior addendum's self-fix round):

- **F19 (fixed)**: `internal/provisioner/policy.go`'s `buildPolicySQL` now emits `USING (...)` only for `select`/`update`/`delete`, and `WITH CHECK (...)` only for `insert`/`update` — matching Postgres's actual constraint (`FOR INSERT` policies reject a `USING` clause). Two new DB-backed tests in `internal/provisioner/provisioner_test.go`: `TestBuildPolicySQL_GeneratedDDLExecutesForEveryAction` (executes the real generated DDL for all four actions against a live table — this is the test that would have caught F19 before it ever shipped) and `TestBuildPolicySQL_InsertOnlyPolicyRejectsInsertReturning` (empirically proves the F13 premise: an insert-only policy lets a plain `INSERT` through but rejects `INSERT ... RETURNING`, via `db.Pool.WithRLSContext`). Plus a unit test `TestBuildPolicySQL_InsertHasNoUsingClause` in `policy_test.go`. Full repo-wide `go test ./...` re-run clean — this function is shared by every table-policy code path, not just webhooks.
- **F13/F15 (partial improvement)**: the two new provisioner tests above give the backend half of F13's premise (insert needs select too) real, executed-against-Postgres coverage for the first time. The frontend derivation itself (`WebhookMappingEditor.tsx`) still has zero automated coverage — F15 is unchanged, still open, still the same "no component-test harness in this repo" gap.
- **F17 (fixed)**: `webhookMapping.policyWarning` (`en.json`/`pt-BR.json`) reworded from "has row policies, but none grant..." (a stale claim once F14 removed the zero-policy skip) to "is missing a row policy granting..." — states the requirement, doesn't imply a policy-count precondition that no longer exists in the logic.
- **F18 (fixed)**: `WebhookMappingEditor.tsx` now destructures `isError` from `useTablePolicies` and renders a distinct `webhookMapping.policyCheckFailed` message when the policy check itself fails, instead of silently falling through to "no warning" (the same code path a genuine missing-policy case takes when `targetTablePolicies` is undefined).

Gates re-run: `go build ./...`, `go vet ./...`, `gofmt -l` on all changed Go files, `go test -p 1 -count=1 ./...` (every package, disposable Postgres 16 + `DASHBOARD_BOOTSTRAP_SECRET`), `npx tsc -b`, `npm run build`, `en.json`/`pt-BR.json` key parity — all clean.

**This addendum does not upgrade the overall verdict to a fully independently-verified PASS.** F19's two new tests are real (they execute actual DDL against actual Postgres, not mocks) and directly demonstrate the fix — but no fresh agent has adversarially mutation-tested them yet. F13/F14's frontend logic still has zero automated coverage (F15, unchanged). The recommendation standing since Addendum 3 holds: a genuine independent Verifier pass over the full accumulated diff, by an agent that has not authored any of it, before this ships.

---

# Addendum 8 — Independent re-verification of F17-F19 (post-Addendum-7 self-fix) + full-diff skeptical sweep

**Date**: 2026-08-11
**Scope**: (a) F17, F18, F19 as Addendum 7 claims to have fixed them, checked against the *actual current code*; (b) a genuinely skeptical sweep of the whole accumulated diff — seventh pass, fourth independent one — hunting for cross-round interaction bugs, collateral damage from the `internal/provisioner/policy.go` change, and doc/i18n drift.
**Verifier**: independent sub-agent — authored none of this feature, none of the F1-F7 fixes, none of the F11-F14 fixes, none of the F17-F19 fixes. Fourth independent pass (after Addenda 2, 4, 6).
**Diff state**: still uncommitted, 18 working-tree entries (`git diff HEAD` = 1765 lines, 18 files, +1276/-38). Nothing committed, staged, or modified in the real tree by this pass; `git status --porcelain` byte-identical before and after except this addendum.
**Method**: code read of the real runtime paths → mutation testing in an isolated `git worktree` (`/tmp/verifier-scratch-webhooks-4`, branch `verifier-scratch-webhooks-4`, working-tree diff applied via `git apply`, `internal/dashboard/static` + `ui/node_modules` copied in for `go:embed` and the pinned TS toolchain) against a disposable `postgres:16-alpine` on port 55447. 5 mutants injected. Plus one throwaway DB-backed Go test written in the worktree (deleted afterward) to prove the F13+F19 end-to-end interaction. Worktree, branch and container removed; diff confirmed byte-identical to the pre-mutation capture afterward.

## Per-finding verdict

| Finding | Verdict | Evidence |
| --- | --- | --- |
| **F19** — `USING` conditioned to select/update/delete, `WITH CHECK` to insert/update | ✅ **PASS (real, executed, and the tests bite)** | `policy.go:169-176`: `if def.Action != "insert" { USING }` + `if def.Action == "insert" \|\| def.Action == "update" { WITH CHECK }`. Mutant **P1** — revert to the single always-`USING` `Fprintf` — **KILLED by three tests**, two of which execute real DDL: `TestBuildPolicySQL_GeneratedDDLExecutesForEveryAction` → `exec generated DDL for action=insert failed … ERROR: only WITH CHECK expression allowed for INSERT (SQLSTATE 42601)`, `TestBuildPolicySQL_InsertOnlyPolicyRejectsInsertReturning` → same error at DDL time, plus the unit test `TestBuildPolicySQL_InsertHasNoUsingClause`. Both DB tests confirmed **running, not skipping** (`-v`, 0.10s / 0.07s wall, real `pool.Exec`) and CI does set `TEST_DATABASE_URL` (`.github/workflows/reusable-ci.yml:25`), so they will actually execute in CI rather than silently skip. |
| **F18** — `isError` surfaced as a distinct message | ✅ **PASS (logic + i18n correct)** / ⚠️ untested | `WebhookMappingEditor.tsx:88` destructures `isError: targetTablePoliciesFailed`; `:313-321` renders `t("webhookMapping.policyCheckFailed")` in `--danger` as a ternary *before* falling through to the warning, so an errored policy fetch no longer looks like "no problem". Key exists in **both** locales, takes **no** interpolation on either side (verified programmatically), and both are actually referenced from the component. A disabled query (`enabled: Boolean(appId) && Boolean(table)`) reports `isError === false`, so the new branch can't fire spuriously when `targetTable === ""`. |
| **F17** — `policyWarning` copy matches F14's logic | ✅ **PASS on the reported defect** / ⚠️ residual overclaim (F22) | The stale precondition is gone: "**has row policies**, but none grant…" → "**is missing a row policy** granting…", both locales, semantically equivalent phrasings. It no longer asserts a policy-count state that F14's logic doesn't require. Residual: it still asserts "real deliveries **will** fail", which is false for the false-positive case F14 knowingly accepted — see F22. |

## Cross-round interaction check (F14 frontend + F19 backend, end to end)

The specific worry: after four rounds, does the warning F13/F14 shows actually correspond to a remediation an operator can now perform? Verified with a throwaway DB-backed test in the worktree (created, run, deleted — not left in the diff): create a table via `provisioner.Apply`, `ENABLE ROW LEVEL SECURITY`, then create **only** the `insert` + `select` policy pair for role `webhook` through the *real* `BuildPolicySQL` output, then run the delivery path's actual statement shape inside `pool.WithRLSContext(RLSClaims{Role:"webhook"})`:

```
DDL[insert] = CREATE POLICY "webhook_insert" ON "…"."orders" FOR INSERT TO zeep_app_enduser WITH CHECK ((current_setting('app.jwt_role', true) = ANY (ARRAY['webhook'])) AND ("status" = 'active'::TEXT))
DDL[select] = CREATE POLICY "webhook_select" ON "…"."orders" FOR SELECT TO zeep_app_enduser USING (…same…)
INSERT INTO "…"."orders" (status) VALUES ('active') RETURNING status  -->  OK
```

✅ **Both policies now coexist and `INSERT … RETURNING` succeeds.** The loop the last three addenda opened is genuinely closed: F13's warning names `insert`+`select`, F19 makes the `insert` half creatable, and the pair actually satisfies Postgres. This is the first pass where that full round trip has been demonstrated rather than reasoned about.

## Collateral-damage check on `policy.go`'s callers

| Check | Result |
| --- | --- |
| `CreateTablePolicy` / `UpdateTablePolicy` (`table_policies_store.go:107`, `:257`) affected? | ✅ No. Both treat the return value as an opaque string and `tx.Exec` it; neither parses or post-processes it. `UpdateTablePolicy` is `DROP POLICY IF EXISTS` + the newly-built `CREATE POLICY` in one tx, so an update-action policy is rebuilt with **both** `USING` and `WITH CHECK` — asserted by `TestBuildPolicySQL_ValidEqualityClaim` (exact `USING (…)` string **and** `WITH CHECK (`, action `update`) and executed by the new DDL test. |
| Any other place in the repo builds `CREATE POLICY` DDL? | ✅ No. `buildPolicySQL` is the only generator (grep over `internal/`, `cmd/`); the other hits are comments and two hand-written `deny_all` policies in tests. |
| Any existing test elsewhere implicitly relying on always-`USING` and now silently wrong? | ✅ No. Full `go test -p 1 -count=1 ./...` green pre-mutation, and under mutant P1 exactly the three intended tests fail — nothing else flips in either direction. Note as a coverage fact, not a defect: `./internal/dashboard/...` stays **green** under P1, i.e. the `CreateTablePolicy`/`UpdateTablePolicy` handler+store layer has **no** insert-action DDL coverage of its own; the provisioner package is the only guard. |
| `allowedActions` sanity (would `truncate`/`all` reach the DDL?) | ✅ No — allowlist is exactly select/insert/update/delete, with a negative test for `truncate`. |

## Mutation sensor — 5 mutants

Baseline in the scratch worktree: `go build ./...` clean, `go vet ./...` clean, `gofmt -l internal cmd` clean, `go test -p 1 -count=1 ./...` **fully green** (all packages, disposable Postgres 16 + `DASHBOARD_BOOTSTRAP_SECRET`), `tsc -b` clean and `npm run build` clean with the repo-pinned toolchain, the new untracked `internal/dashboard/middleware_test.go` present and its two `TestRateLimiter_*` confirmed running.

| # | Mutant | Target | Result |
| --- | --- | --- | --- |
| P1 | Revert to the single always-`USING` `Fprintf` (the original F19 bug) | `provisioner/policy.go:169` | ✅ KILLED ×3 — `TestBuildPolicySQL_GeneratedDDLExecutesForEveryAction`, `TestBuildPolicySQL_InsertOnlyPolicyRejectsInsertReturning` (both against real Postgres, exact original error), `TestBuildPolicySQL_InsertHasNoUsingClause` |
| P2 | `if def.Action != "insert" && def.Action != "delete"` — **delete** policies emitted with no `USING` at all | `provisioner/policy.go:172` | ❌ **SURVIVED** → F20 (security-relevant) |
| P3 | Drop the `isError` branch + the `isError` destructure (revert F18) | `WebhookMappingEditor.tsx:88,313` | ❌ SURVIVED — `tsc -b` + `npm run build` both clean, no test exists (F15) |
| P4 | Revert `policyWarning` to the pre-F17 "has row policies, but none grant…" copy | `en.json:883` | ❌ SURVIVED — no test asserts any user-facing copy (F15); an orphaned `policyCheckFailed` key also goes unflagged |
| P5 | (control) restore everything, re-diff | whole tree | ✅ `git diff HEAD` byte-identical to the pre-mutation capture — no mutant leaked |

**Score: 1 killed (×3 tests) / 3 survived.** P3/P4 are the known, unchanged F15 gap (this repo has **no** frontend unit-test harness — no `vitest`/`jest` in `internal/dashboard/ui/package.json`, and `e2e/` never references either key). P2 is new and is the one finding from this pass I'd want landed alongside the diff.

## Documentation / i18n sweep (whole files, not just touched keys)

| Check | Result |
| --- | --- |
| `en.json` / `pt-BR.json` key parity | ✅ **902 / 902**, zero orphans either direction, both JSON-valid, no duplicate raw keys in either file |
| `{{placeholder}}` parity across **all** 902 shared keys | ✅ zero mismatches (checked programmatically, not by eye) |
| Every `t("…")` key used in `Webhooks.tsx` + `WebhookMappingEditor.tsx` resolves | ✅ yes; both new/changed keys are actually referenced (no dead key) |
| `--` vs `—` consistency in the new copy | ✅ `policyCheckFailed`'s `--` matches the three sibling `webhooks.*` strings already shipped |
| Duplicate-value keys `webhooks.editWebhook` / `webhooks.edit` (identical text, both locales) | ℹ️ both are genuinely referenced (modal title vs. icon-button `title`); redundant but not a defect |
| `spec.md` P3 AC3 still accurate post-Addendum-3..7? | ✅ Yes. It describes the shipped `PATCH /dashboard/api/apps/{id}/webhooks/{webhookId}`, `webhook.update` audit action, and the "name/method/event-shape paths only — token, status, captured_sample untouched" contract, which is exactly what `UpdateWebhook` (`webhooks_store.go:340-345`, no `status`/`captured_sample`/`token_secret` in the SET list) does and what `TestUpdateWebhookHandler_ChangesFieldsPreservesTokenAndAudits` asserts. Nothing since Addendum 3 changed that surface. |
| `CHANGELOG.md` `[Unreleased]` accuracy | ✅ Substantively accurate, including the new "Row policies: creating an `insert`-action policy on any table no longer fails" entry (correctly filed as a general provisioner fix, not a webhook one). Two nits: F24 below. |
| `policy.go`'s own exported doc comment | ❌ stale → F21 |
| Gates (`go build ./...`, `go vet ./...`, `gofmt -l internal cmd`, `go test -p 1 -count=1 ./...`, `tsc -b`, `npm run build`) | ✅ all clean/green in the scratch worktree with the diff applied |
| Playwright e2e against a live server | ⚪ not run (no live stack) — `webhooks.spec.ts` reviewed statically only, same position as Addenda 4 and 6 |

## New findings

### F20 — ❌ New, security-relevant, and introduced *by* F19's fix: a `delete` policy silently generated without `USING` passes the whole suite

Mutant P2 (`if def.Action != "insert" && def.Action != "delete"`) makes `buildPolicySQL` emit, for a delete-action policy, `CREATE POLICY … FOR DELETE TO zeep_app_enduser` with **no** `USING` and no `WITH CHECK`. Postgres accepts that and defaults the policy to permissive — i.e. **every end-user role can delete every row** of that RLS-enabled table, the app-defined role check and the clause fold both discarded. `go test ./internal/provisioner/... ./internal/dashboard/...` stays **fully green**.

Root cause is a coverage hole that F19's fix newly exposed: `Action: "delete"` appears **zero** times in `internal/provisioner/policy_test.go` (24× `select`, 2× `insert`, 1× `update`, 1× `truncate`-as-negative), and the new `TestBuildPolicySQL_GeneratedDDLExecutesForEveryAction` only asserts that the generated DDL *executes* — never that the clause it's supposed to carry is present. Before F19, `USING` was unconditional and this whole class of mistake was structurally impossible; F19 introduced the branch without a test pinning its non-`insert` side.

**Fix** (small, and it belongs with this diff since this diff created the branch): in the new DDL test, additionally assert `strings.Contains(sql, "USING (")` for `select`/`update`/`delete` and `strings.Contains(sql, "WITH CHECK (")` for `insert`/`update`, or add a `delete`-action unit test mirroring `TestBuildPolicySQL_ValidEqualityClaim`. Either kills P2.

### F21 — ⚠️ `BuildPolicySQL`'s exported doc comment is now stale (same class as F17)

`internal/provisioner/policy.go:103-104` still promises "a complete `CREATE POLICY ... TO zeep_app_enduser USING (...) [WITH CHECK (...)]` statement" — as of F19 that is no longer true for `insert`, where `USING` is *absent* and `WITH CHECK` is the mandatory part, i.e. the optionality is exactly inverted for one of the four actions. The in-function comment F19 added (`:163-168`) is correct and thorough; only the exported godoc drifted. Literally the same failure mode F17 was filed for, one layer down.
**Fix**: reword to `CREATE POLICY ... TO zeep_app_enduser [USING (...)] [WITH CHECK (...)]` and point at the in-function comment for which action gets which.

### F22 — ⚠️ F17's new copy still overclaims "real deliveries **will** fail"

`webhookMapping.policyWarning`, both locales: "…is missing a row policy granting the "{{action}}" action to the webhook role — real deliveries **will fail** until you add one on the Policies tab." F14 deliberately accepted a false positive: a table that genuinely never had RLS enabled (truly zero policies, truly unrestricted) now also triggers this warning, and for that table deliveries will **not** fail. Addendum 6's suggested wording was "…or real deliveries **may** fail" precisely to cover this. The half of F17 about the stale "has row policies" precondition is fixed; the half about an unconditional failure claim is not.
**Fix**: `will fail` → `may fail` (`vão falhar` → `podem falhar`). One word per locale, no logic change.

### F23 — ℹ️ Clearing `event_id_path` via the new PATCH has no test, and the frontend uses the exact key-omission pattern AGENTS.md §4 forbids

`Webhooks.tsx:112` sends `...(eventIdPath.trim() ? { event_id_path: eventIdPath.trim() } : {})` on the **update** path — the key is omitted when the user blanks the field. AGENTS.md §4: "any frontend form that lets a user **clear** a field must always send that field's key explicitly … never omit it." This is safe *today* only because `UpdateWebhook` (`webhooks_store.go:340-345`) is full-replace, not merge-on-absent: the absent key unmarshals to `""`, which the store maps to a SQL `NULL`. No test asserts that, though — `webhooks_store_test.go:296` and `webhooks_handler_test.go:331` only ever set `event_id_path` to a non-empty value. So the one behaviour that makes the omission harmless is unverified, and `event_id_path` is the field the delivery dedup key depends on.
**Fix**: send the key unconditionally (`event_id_path: eventIdPath.trim()`), and/or add one store-test case asserting a PATCH with an empty `event_id_path` nulls the column.

### F24 — ℹ️ Two `CHANGELOG.md` nits, one carried unfixed from Addendum 6

1. Both mapping-editor bullets still say the form "checks this at save time" — the check renders **live** as the form changes (`WebhookMappingEditor.tsx:129-133` is plain derived state, not save-gated). Addendum 6 filed this exact nit; Addendum 7's edits went through the same sentence and left it.
2. The `insert`+`select` / zero-policy reasoning is now stated twice, nearly verbatim, in the "Changed" mapping-editor bullet **and** the new "Fixed" bullet. Harmless, but a reader hits the same explanation twice.

### Test-hygiene note (not a finding against this diff)

Both new provisioner tests use `defer pool.Close()` *and* `t.Cleanup(func(){ dropSchema(t, pool, schema) })`. `t.Cleanup` runs after the deferred `Close`, so `dropSchema` always fails and only emits `t.Logf("warn: cleanup drop schema …: closed pool")` — the test schemas leak. This is identical to all **10** pre-existing tests in the same file (`defer pool.Close()` ×12, `t.Cleanup(dropSchema)` ×12, zero `defer dropSchema`), so it is a long-standing repo-wide pattern the new tests correctly imitated, not something this diff introduced. Worth a separate cleanup task; irrelevant to CI, whose Postgres is ephemeral.

## Carried-forward, unchanged

F8 (informational, no defect), F9 (`WEBHOOK_TOKEN_ENCRYPTION_KEY` unreachable via the Helm chart), F10 (`useWebhooks` polls indefinitely), **F15** (frontend policy-warning + policy-check-failed logic still has zero automated coverage — mutants P3 and P4 both survive; still the single highest-value follow-up), F16 (delivery-log responses embed `raw_payload` into the log ring buffer, pre-existing). All still open exactly as previously described.

## Gate check (scratch worktree, uncommitted diff applied)

| Gate | Result |
| --- | --- |
| `go build ./...` | ✅ clean |
| `go vet ./...` | ✅ clean |
| `gofmt -l internal cmd` | ✅ clean |
| `go test -p 1 -count=1 ./...` (all packages, disposable Postgres 16 on :55447) | ✅ fully green |
| New DB-backed provisioner tests confirmed running, not skipping (`-v`), and CI sets `TEST_DATABASE_URL` | ✅ |
| `tsc -b` + `npm run build` (repo-pinned toolchain) | ✅ clean |
| `en.json`/`pt-BR.json` JSON-valid, 902/902 parity, placeholder parity, no dupes | ✅ |
| Mutants fully reverted (`git diff HEAD` byte-identical to pre-mutation capture) | ✅ |
| Real working tree untouched | ✅ `git status --porcelain` identical before/after (18 entries); worktree, branch and container removed |

## Verdict

**F17 ✅ PASS on the reported defect (residual wording overclaim → F22) · F18 ✅ PASS · F19 ✅ PASS — and for the first time in this feature's history a *backend* fix ships with tests that execute real DDL against real Postgres and demonstrably reproduce the original error when reverted.**

**Overall: ✅ PASS — this is a legitimately clean state to commit.** Seven rounds have converged: every finding from Addenda 2, 4 and 6 that was in scope is closed, the F13↔F19 loop is now proven end-to-end (insert+select pair created via the real generator, `INSERT … RETURNING` succeeds), no cross-round regression surfaced, all gates are green, and i18n/`spec.md`/`CHANGELOG.md` are substantively accurate.

Four things remain genuinely open, none of them a defect in shipped behaviour and none blocking a commit:

1. **F20** is the only one I'd ask to land *with* this diff rather than after: F19's fix introduced a conditional whose `delete` side, if broken, silently produces a fully-permissive delete policy and the entire suite stays green. It's a two-line assertion in a test that already exists.
2. **F15 / P3 / P4** — the frontend RLS-warning logic (now including F18's new error branch) still has zero automated coverage, because the repo has no frontend unit-test harness at all. Correctness here rests on this pass's static trace plus empirical Postgres checks. Unchanged and honestly stated, not papered over.
3. **F21, F22, F24** — three small wording/doc drifts, two of them the same "stale prose after a logic change" pattern this feature keeps producing.
4. **F23** — a latent AGENTS.md §4 deviation that is currently harmless only because of an untested backend property.

## Addendum 9 (2026-08-11, same day, self-fixed — not re-verified by an independent pass)

F20-F24 all fixed on the same working tree (same author-equals-verifier caveat as every prior self-fix round):

- **F20 (fixed)**: `TestBuildPolicySQL_DeleteHasUsingAndNoWithCheck` and `TestBuildPolicySQL_UpdateHasBothUsingAndWithCheck` added to `internal/provisioner/policy_test.go` — direct clause-presence assertions for the two actions the DDL-execution test alone couldn't distinguish (a fully-permissive policy still executes without error). Also fixed a stale comment referencing a test name (`TestBuildPolicySQL_InsertDDLExecutesOnRealPostgres`) that was never the actual name of the DB-backed test.
- **F21 (fixed)**: `BuildPolicySQL`'s godoc in `internal/provisioner/policy.go` no longer claims `USING (...) [WITH CHECK (...)]` is the shape for every action — now states the per-action selection explicitly.
- **F22 (fixed)**: `webhookMapping.policyWarning` (`en.json`/`pt-BR.json`) reworded from "is missing... will fail" to "may be missing... could fail" — F14's fix deliberately accepts false positives (warn on a table that never had RLS at all), so the warning can no longer honestly claim certainty either way.
- **F23 (fixed)**: `Webhooks.tsx`'s create/update submit handlers now always send `event_id_path` (empty string when blank) instead of conditionally omitting the key — removes the merge-on-absent-key-shaped pattern AGENTS.md §4 flags, even though it was already behaviorally harmless here (both `CreateWebhookInput`/`UpdateWebhookInput` are full-replace, and Go's JSON decode treats an absent key and an empty-string value identically for a non-pointer field).
- **F24 (fixed)**: `CHANGELOG.md`'s RLS-warning bullet no longer says "at save time" (the warning is live, updating as target table/action change) and now states the best-effort/false-positive-accepted nature of the check and its distinct failure-to-load message, instead of implying a guarantee.

Gates re-run: `go build ./...`, `go vet ./...`, `gofmt -l` on all changed Go files, `go test -p 1 -count=1 ./...` (every package, disposable Postgres 16 + `DASHBOARD_BOOTSTRAP_SECRET`), `npx tsc -b`, `npm run build`, `en.json`/`pt-BR.json` key parity — all clean.

**Still open, unchanged**: F8, F9, F10, F15 (frontend RLS-warning logic has zero automated coverage — no frontend test harness exists in this repo), F16. None of these block a commit; F15 is the only one with any real weight, and it has been honestly disclosed, not fixed, across every addendum since it was first raised.

This addendum does not claim a fifth independent verification — F20-F24 are all small, mechanical, and low-risk (test additions, doc/string wording, one style consistency change), unlike the F1/F13/F19-class fixes that got independent passes each round. Given the number of rounds already run and the size of what's left, this is a reasonable point to stop and commit.
