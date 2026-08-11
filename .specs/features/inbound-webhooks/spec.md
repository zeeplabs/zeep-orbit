# Inbound Webhooks Specification

## Problem Statement

App owners today have no way to let an external system (Google Workspace, or any third-party provider) push data into a Zeep Orbit app table without a custom integration. Every such integration currently requires bespoke backend code. This feature lets an app owner create a webhook endpoint that receives an external provider's events, maps the incoming payload to a table's columns, and writes rows automatically — no code required.

## Goals

- [ ] App owner can create a webhook, capture a real sample payload from their provider, and map it to a table without writing code.
- [ ] Once activated, a webhook correctly applies insert/update/delete actions based on the incoming payload's event-type field, using a configured match key for update/delete.
- [ ] Every call the webhook receives (successful or not) is recorded, so the app owner can debug integration issues from the dashboard.

## Out of Scope

| Feature | Reason |
|---|---|
| Outbound webhooks (Orbit fires HTTP calls when a row changes) | Separate feature (`ROADMAP.md:46`), opposite direction, different trigger model. Tracked independently. |
| Drag-and-drop mapping canvas (n8n-style free-form UI) | V1 ships a simpler click-to-link picker (JSON tree → column list). Canvas is future polish, not required for the mapping outcome. |
| Granular per-webhook RBAC (who specifically can manage a given webhook) | `rbac-per-app` (admin/editor/viewer roles) is not yet shipped. This feature reuses whatever access control already gates other app-config resources today and inherits `rbac-per-app` automatically once that ships. |
| Payload transformation logic (computed/derived columns, scripting) | V1 mapping is a direct field→column link only. Transform functions are a future extension. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
|---|---|---|---|
| Response status codes for failure modes not explicitly discussed | 401/403 for invalid/missing token, 400 for malformed request body, 200 for unmapped event or no-matching-row (silent, logged), 500 for internal write failure | Follows REST convention; 200 on "expected but unresolvable" cases avoids providers retry-storming a webhook that's still being configured or targets a since-deleted row | n |
| Second test event arrives while webhook is still in capture mode (before mapping is saved) | Overwrite the stored sample with the latest received payload | Owner is actively iterating during setup; keeping only the latest capture is simplest and matches "test until you get the shape you want" | n |
| Update/delete request whose match-key value has no corresponding row | Treat as no-op: respond 200, log delivery as "row not found" | Consistent with the unmapped-event-type default; avoids provider retry loops for a case the provider can't resolve by retrying | n |
| Request method/body shape for GET-configured webhooks | Query parameters are normalized into the same JSON-like structure used for POST/PUT bodies before capture/mapping, so the mapping picker works uniformly regardless of configured method | Keeps one mapping engine regardless of HTTP method choice, per the user's requirement that method is chooseable at creation | n |
| Concurrent update/delete calls targeting the same match-key row | Resolved at the database layer (row-level locking / `ON CONFLICT` semantics), not a product-facing behavior | Technical concern, not a gray area the user needs to decide; finalized in Design | n |

**Open questions:** none — all resolved above or through discussion in `context.md`.

---

## User Stories

### P1: App owner creates a webhook and captures a real sample ⭐ MVP

**User Story**: As an app owner, I want to create a webhook, point my external provider at its URL, and see the real payload it sends, so that I can build a mapping without guessing the payload shape.

**Why P1**: Without a working capture step, there is nothing to map — this is the entry point of the entire feature.

**Acceptance Criteria**:

1. WHEN an app owner creates a webhook (choosing an HTTP method) THEN the system SHALL generate a unique URL containing a token scoped to that webhook alone.
2. WHILE a webhook has no saved mapping THEN the system SHALL treat every incoming call as capture-only: store the received payload as the sample and SHALL NOT write to any table.
3. WHEN a second call arrives while the webhook is still unmapped THEN the system SHALL overwrite the previously stored sample with the newest payload.
4. IF the incoming request's HTTP method does not match the webhook's configured method THEN the system SHALL reject the request with `404`.
5. IF the request's token is missing or does not match the webhook's token THEN the system SHALL reject the request with `401` and SHALL still record the attempt in the delivery log.
6. The system SHALL record every call received by a webhook — regardless of outcome — in the webhook's delivery log (timestamp, raw payload, matched status, and any error detail).

**Independent Test**: Create a webhook, `POST` a sample JSON body to its URL with the correct token, confirm the response is `200`, confirm no table row was created, and confirm the sample is retrievable for mapping.

---

### P2: App owner maps captured sample and activates the webhook for inserts

**User Story**: As an app owner, I want to pick a target table and click-link fields from my captured sample to columns, so that activating the webhook turns future real events into real rows.

**Why P2**: Builds directly on P1's capture — this is the first story where the feature produces actual data.

**Acceptance Criteria**:

1. WHEN an app owner selects a target table and links one or more sample fields to columns for a given event-type value THEN the system SHALL save that mapping as `insert` for that event-type value.
2. WHEN an app owner activates a webhook that has at least one saved mapping THEN the system SHALL start applying mappings to subsequent real calls instead of capturing.
3. WHEN an activated webhook receives a call whose event-type value matches a saved `insert` mapping THEN the system SHALL create a row in the mapped table using the linked column values.
4. WHEN the incoming payload carries an event identifier AND that identifier was already processed by this webhook THEN the system SHALL skip the write, respond `200`, and log the delivery as duplicate-skipped.
5. IF the incoming payload's event-type value has no saved mapping THEN the system SHALL respond `200` and log the delivery as "unmapped event".
6. IF applying a saved mapping fails at the database layer THEN the system SHALL respond `500`, log the delivery with the error detail, and SHALL NOT partially write the row.

**Independent Test**: With a webhook already in capture mode holding a sample (from P1), map its event-type value to `insert` on a real table, activate, send a real event with a fresh event id, and confirm exactly one row is created with the mapped values.

---

### P2: App owner maps update and delete actions with a match key

**User Story**: As an app owner, I want to configure update and delete mappings — each with a column that identifies which row to touch — so that one webhook can keep a table in sync across a resource's full lifecycle (create, edit, remove).

**Why P2**: Completes the core value proposition (the user's Google Workspace employee-sync example needs all three actions on one webhook) but is independently demoable once inserts already work.

**Acceptance Criteria**:

1. WHEN an app owner maps an event-type value to `update` or `delete` THEN the system SHALL require a match key: a column mapped from a sample field, used to locate the target row.
2. WHEN an activated webhook receives a call matching an `update` mapping THEN the system SHALL locate the row by the match key's mapped value and SHALL overwrite the linked columns.
3. WHEN an activated webhook receives a call matching a `delete` mapping THEN the system SHALL locate the row by the match key's mapped value and SHALL remove it following the app's existing soft-delete configuration.
4. IF no row matches the match key's value for an `update` or `delete` call THEN the system SHALL respond `200` and log the delivery as "row not found".
5. The system SHALL allow a single webhook to hold mappings for multiple event-type values simultaneously, each with its own action, target table, field links, and (where applicable) match key.

**Independent Test**: On the same webhook from the insert story, add an `update` mapping and a `delete` mapping (each with a match key), send a real event for each, and confirm the target row is updated then removed (soft-deleted per system config) accordingly.

---

### P2: App owner reviews webhook deliveries in the dashboard

**User Story**: As an app owner, I want to see the history of calls a webhook has received — success, failure, unmapped, duplicate — so that I can debug my provider integration without server access.

**Why P2**: The delivery log already exists as a write-time requirement (P1 AC6); this story is the read-side dashboard view that makes it useful to a non-technical app owner.

**Acceptance Criteria**:

1. WHEN an app owner opens a webhook's delivery log in the dashboard THEN the system SHALL list its recorded deliveries in reverse-chronological order, each showing timestamp, outcome, and event-type value.
2. WHEN an app owner opens a single delivery entry THEN the system SHALL show its raw received payload and, if it failed, the recorded error detail.
3. The system SHALL purge delivery log entries older than 30 days.

**Independent Test**: Trigger a mix of successful, unmapped, and invalid-token deliveries against one webhook, then confirm the dashboard lists all of them with correct outcomes and that opening one shows its raw payload.

---

### P3: App owner manages webhook lifecycle

**User Story**: As an app owner, I want to rotate a webhook's token or delete a webhook entirely, so that I can respond to a leaked credential or decommission an integration.

**Why P3**: Important operationally, but the feature is fully functional and demoable without it — an app owner can work around a leaked token by deleting and recreating the webhook until this ships.

**Acceptance Criteria**:

1. WHEN an app owner rotates a webhook's token THEN the system SHALL invalidate the old token immediately and SHALL generate a new one, without affecting the webhook's saved mappings.
2. WHEN an app owner deletes a webhook THEN the system SHALL stop accepting calls at its URL and SHALL retain its delivery log until the standard 30-day purge (not deleted immediately alongside the webhook).
3. Every webhook create, ~~edit,~~ delete, and token-rotation action SHALL be recorded in the existing dashboard `audit_log`. **Gap, not covered**: there is no webhook-edit endpoint (name/method/paths are set at creation and never mutable afterward) — "edit" was listed here before that was noticed; either build the edit endpoint (+ its own audit entry) or strike it from this AC. Flagged by the Verifier (`validation.md`, L-011) and not yet resolved.

**Independent Test**: Rotate a webhook's token, confirm the old token is rejected and the new one works with the mapping intact; delete a different webhook and confirm its URL now rejects all calls.

---

## Edge Cases

- IF the request body is not valid JSON (for POST/PUT webhooks) THEN the system SHALL respond `400` and log the delivery as malformed.
- IF a GET-configured webhook's query parameters cannot be normalized into a usable structure (e.g. no parameters sent) THEN the system SHALL still capture/process it as an empty payload rather than erroring.
- WHEN the delivery log purge runs THEN entries older than 30 days SHALL be removed regardless of a webhook's active/inactive/deleted state.
- IF an app owner tries to activate a webhook with zero saved mappings THEN the system SHALL reject the activation with a clear validation error.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| WEBHOOK-01 | P1 AC1: create webhook, unique URL + token | Tasks | Verified (e2e: `webhooks.spec.ts` — URL/token asserted non-empty after creation) |
| WEBHOOK-02 | P1 AC2: unmapped webhook is capture-only, no write | Tasks | Verified (e2e: row count asserted 0 after two capture-mode calls) |
| WEBHOOK-03 | P1 AC3: second capture call overwrites the sample | Tasks | Verified (e2e: mapping editor shows the 2nd payload's field, not the 1st's) |
| WEBHOOK-04 | P1 AC4: method mismatch rejects with 404 | Tasks | Verified (e2e: GET against a POST-configured webhook → 404) |
| WEBHOOK-05 | P1 AC5: missing/invalid token rejects with 401, attempt logged | Tasks | Verified (e2e: wrong-token call → 401; confirmed in delivery log) |
| WEBHOOK-06 | P1 AC6: every call recorded in the delivery log | Tasks | Verified (e2e: delivery log lists captured/invalid_token/unmapped/write_error/inserted/duplicate_skipped entries) |
| WEBHOOK-07 | P2 (map+activate) AC1: save an insert mapping via click-linked fields | Tasks | Verified (e2e: field→column link + save mapping via UI) |
| WEBHOOK-08 | P2 (map+activate) AC2: activate switches capture→active | Tasks | Verified (e2e: Activate button, status becomes Active) |
| WEBHOOK-09 | P2 (map+activate) AC3: matching event creates a row | Tasks | Verified (e2e: real POST creates exactly one row with mapped values) |
| WEBHOOK-10 | P2 (map+activate) AC4: duplicate event id is skipped | Tasks | Verified (e2e: repeated event id → `duplicate_skipped`, row count unchanged) |
| WEBHOOK-11 | P2 (map+activate) AC5: unmapped event-type value is a no-op | Tasks | Verified (e2e: unmapped event-type → 200/`unmapped`, no write) |
| WEBHOOK-12 | P2 (map+activate) AC6: field-resolution/write failure → 500, no partial write | Tasks | Verified (e2e: payload missing the mapped field → 500/`write_error`, row count unchanged) |
| WEBHOOK-13 | P2 update/delete AC1: match key required for update/delete | Tasks | Not covered by e2e — out of T17's scope by design (P2 update/delete story excluded per task instructions); covered by backend integration tests (`webhook_event_mappings_store_test.go`) |
| WEBHOOK-14 | P2 update/delete AC2: update locates row by match key, overwrites | Tasks | Not covered by e2e (same scope note as WEBHOOK-13); covered by `internal/server/webhook_active_update_delete_test.go` |
| WEBHOOK-15 | P2 update/delete AC3: delete locates row by match key, soft-deletes | Tasks | Not covered by e2e (same scope note); covered by `webhook_active_update_delete_test.go` |
| WEBHOOK-16 | P2 update/delete AC4: no match → row_not_found, no write | Tasks | Not covered by e2e (same scope note); covered by `webhook_active_update_delete_test.go` |
| WEBHOOK-17 | P2 update/delete AC5: one webhook holds mappings for multiple event types/actions | Tasks | Not covered by e2e (same scope note); covered by `webhook_active_update_delete_test.go`'s full-lifecycle case |
| WEBHOOK-18 | P2 delivery log AC1: dashboard lists deliveries reverse-chronological | Tasks | Verified (e2e: delivery log view opened, entries visible with outcome) |
| WEBHOOK-19 | P2 delivery log AC2: opening an entry shows raw payload/error detail | Tasks | Not directly asserted by the e2e (delivery entries are visible but the test doesn't expand one to check raw payload/error text); covered by `TestListWebhookDeliveriesHandler_NewestFirstWithPayloadAndError` |
| WEBHOOK-20 | P2 delivery log AC3: 30-day purge | Tasks | Not covered by e2e (time-based, impractical at this layer); covered by `webhook_deliveries_store_test.go` purge tests and the ticker wiring (T12) |
| WEBHOOK-21 | P3: rotate token | Tasks | Not covered by e2e (P3, out of T17's scope); covered by `TestRotateWebhookTokenHandler_InvalidatesOldTokenAndAudits` |
| WEBHOOK-22 | P3: delete webhook, retain delivery log | Tasks | Not covered by e2e (P3, out of T17's scope); covered by `TestDeleteWebhookHandler_SoftDeletesAndAudits` / `TestSoftDeleteWebhook_NeverHardDeletes` |
| WEBHOOK-23 | P3: audit log entry per lifecycle action | Tasks | ❌ Partial: create/rotate/delete/activate handler tests cover their own audit assertions, but "edit" has no implementation to test at all — see the gap noted directly on P3 AC3 above. Not covered by e2e (P3, out of T17's scope) |
| WEBHOOK-24 | Provider verification handshake: a payload carrying a top-level non-empty `challenge` string is echoed back verbatim, bypassing capture/mapping | Design (Error Handling Strategy) | Covered by `TestWebhookDelivery_VerificationChallengeEchoedBeforeCapture` (`internal/server/webhook_handler_test.go`). **Added post-launch** (found during real-world Slack integration testing, after the original Verifier PASS) — never had its own AC until now; retrofitted per M2 finding in the Opus review. |

**ID format:** `WEBHOOK-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 24 total (WEBHOOK-24 added post-launch, M2), 23 mapped to tasks (T1-T17) + 1 mapped to design's Error Handling Strategy directly, 12 Verified end-to-end by `webhooks.spec.ts` (P1 full story + P2 map+activate-for-inserts full story), 1 Verified by a dedicated handler test (WEBHOOK-24), 10 covered only by backend integration tests (P2 update/delete story + delivery-log AC2/AC3), 1 ❌ partial (WEBHOOK-23 — no edit endpoint exists, see P3 AC3), 0 fully unmapped.

---

## Success Criteria

- [ ] An app owner can go from "create webhook" to "first real row written" without any code, using only the dashboard.
- [ ] Every call a webhook receives — success, failure, unmapped, duplicate — is visible in its delivery log within the dashboard.
- [ ] A single webhook correctly handles insert, update, and delete for the same external resource type (validated against the Google Workspace employee-sync use case).
- [ ] Delivery log entries older than 30 days are purged with zero manual intervention.
