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
