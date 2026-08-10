# Inbound Webhooks Validation

**Date**: 2026-08-10
**Spec**: `.specs/features/inbound-webhooks/spec.md`
**Diff range**: `573f07f..cf58718` (18 commits, `main`, local/unpushed)
**Verifier**: independent sub-agent (author ≠ verifier) — did not participate in implementation

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
| AC1: create webhook → unique URL+token | non-empty token, distinct from `token_hash` | `internal/dashboard/webhooks_store_test.go:70-78` — `TestCreateWebhook_HappyPath`: `token == ""` fails, `row.TokenHash == token` fails, `hashWebhookToken(token) != row.TokenHash` fails; also `internal/dashboard/ui/e2e/webhooks.spec.ts:106-111` (webhookId/token length > 0) | ✅ PASS |
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
| AC1: rotate token invalidates old, generates new, mappings untouched | old token no longer verifies, new token verifies, distinct values | `internal/dashboard/webhooks_store_test.go:246-276` — `TestRotateToken_InvalidatesOldToken`: `got.TokenHash == hashWebhookToken(oldToken)` fails, `got.TokenHash != hashWebhookToken(newToken)` fails; HTTP layer: `webhooks_handler_test.go:224-266` — `TestRotateWebhookTokenHandler_InvalidatesOldTokenAndAudits` (also asserts old token no longer verifies via `VerifyWebhookToken`) | ✅ PASS |
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
