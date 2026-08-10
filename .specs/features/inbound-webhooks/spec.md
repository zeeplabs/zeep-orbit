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
3. Every webhook create, edit, delete, and token-rotation action SHALL be recorded in the existing dashboard `audit_log`.

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
| WEBHOOK-01 | P1: Create webhook + capture sample | Design | Implementing |
| WEBHOOK-02 | P1: Create webhook + capture sample | Design | Implementing |
| WEBHOOK-03 | P1: Create webhook + capture sample | Design | Implementing |
| WEBHOOK-04 | P1: Create webhook + capture sample | Design | Implementing |
| WEBHOOK-05 | P1: Create webhook + capture sample | Design | Implementing |
| WEBHOOK-06 | P1: Create webhook + capture sample | Design | Implementing |
| WEBHOOK-07 | P2: Map + activate for inserts | Design | Implementing |
| WEBHOOK-08 | P2: Map + activate for inserts | Design | Pending |
| WEBHOOK-09 | P2: Map + activate for inserts | Design | Pending |
| WEBHOOK-10 | P2: Map + activate for inserts | Design | Pending |
| WEBHOOK-11 | P2: Map + activate for inserts | Design | Pending |
| WEBHOOK-12 | P2: Map + activate for inserts | Design | Pending |
| WEBHOOK-13 | P2: Update/delete with match key | Design | Implementing |
| WEBHOOK-14 | P2: Update/delete with match key | Design | Pending |
| WEBHOOK-15 | P2: Update/delete with match key | Design | Pending |
| WEBHOOK-16 | P2: Update/delete with match key | Design | Pending |
| WEBHOOK-17 | P2: Update/delete with match key | Design | Implementing |
| WEBHOOK-18 | P2: Dashboard delivery log | Design | Implementing |
| WEBHOOK-19 | P2: Dashboard delivery log | Design | Implementing |
| WEBHOOK-20 | P2: Dashboard delivery log | Design | Implementing |
| WEBHOOK-21 | P3: Webhook lifecycle | Design | Implementing |
| WEBHOOK-22 | P3: Webhook lifecycle | Design | Implementing |
| WEBHOOK-23 | P3: Webhook lifecycle | Design | Pending |

**ID format:** `WEBHOOK-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 23 total, 0 mapped to tasks, 23 unmapped ⚠️ (expected — Design/Tasks not yet run)

---

## Success Criteria

- [ ] An app owner can go from "create webhook" to "first real row written" without any code, using only the dashboard.
- [ ] Every call a webhook receives — success, failure, unmapped, duplicate — is visible in its delivery log within the dashboard.
- [ ] A single webhook correctly handles insert, update, and delete for the same external resource type (validated against the Google Workspace employee-sync use case).
- [ ] Delivery log entries older than 30 days are purged with zero manual intervention.
