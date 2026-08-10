# Inbound Webhooks Context

**Gathered:** 2026-08-10
**Spec:** `.specs/features/inbound-webhooks/spec.md`
**Status:** Ready for design

---

## Feature Boundary

App owners can create webhook endpoints (one URL per webhook, per app) that external systems (Google Workspace, or any provider) call to push events into Zeep Orbit. Each webhook maps an incoming payload's event-type field to an action (insert/update/delete) against a chosen app table, with a field-to-column mapping the owner builds from a captured sample payload. Outbound webhooks (Orbit firing events on row changes) are explicitly a separate, future feature — not part of this boundary.

---

## Implementation Decisions

### Webhook model

- One webhook = one URL = one app. An app can have multiple webhooks.
- A single webhook receives all event types for its integration; an incoming payload carries a field whose value identifies the event type (e.g. `eventType: "user.created"`).
- Per event-type value, the owner configures: action (insert/update/delete), target table, field→column mapping, and (for update/delete) a match key column used to locate the row.

### Security

- Each webhook has its own token (not shared per app) — scoped so revoking one webhook's token doesn't affect others.
- The token travels embedded in the URL itself (e.g. `/hooks/{webhookId}/{token}`), not in a custom header — chosen because several providers (Google Workspace push callbacks included) only accept a bare callback URL with no custom header configuration.

### Setup / mapping flow

- A new webhook starts in **capture mode**: no mapping exists yet, and no table writes happen.
- The owner points the external provider at the webhook URL and triggers one real event there.
- Orbit receives it and stores the raw payload as a sample (capture mode does not write to any table).
- The owner maps the sample: picks the target table, then builds field→column links by clicking a JSON field then a column (V1 UI — see below).
- Once mapped, the owner activates the webhook. From that point, subsequent real calls apply the saved mapping and write to the table.

### Mapping UI (V1)

- V1 ships a simple picker: JSON tree of the captured sample on one side, table columns on the other; click a field, click a column, link created.
- A free-form drag-and-drop canvas (n8n-style) is explicitly deferred as future polish, not a V1 requirement.

### Duplicate delivery handling

- When the incoming payload includes an event identifier, Orbit stores it and skips reprocessing an already-seen id — prevents duplicate rows when a provider retries a `create` event.
- No payload-level event identifier is not treated as an error; there is simply no dedup for that call (update/delete remain naturally idempotent via the match-key key).

### Unmapped event type

- If an incoming payload's event-type value has no configured mapping, Orbit responds `200` (avoids the provider's retry/backoff loop) and logs the call as "unmapped event" in the delivery log — visible to the app owner in the dashboard, not silently lost.

### Auditing (two layers)

- **Config changes** (webhook created/edited/deleted, token rotated) go through the existing `audit_log` mechanism (`internal/dashboard/audit_store.go`), same pattern as every other dashboard-managed resource.
- **Every inbound call** (regardless of outcome — success, unmapped event, invalid token, write failure) is recorded in a dedicated `webhook_deliveries` log: received payload, matched event type, action taken, target row, status, error detail. This is the feature's own audit trail, distinct from admin-action `audit_log` since the actor is an external caller, not a dashboard user.
- `webhook_deliveries` entries are retained 30 days, then purged — prevents unbounded growth on high-traffic integrations.

### Agent's Discretion

- Exact response status codes for failure modes not explicitly discussed (invalid/missing token, malformed JSON body, internal write failure during mapping application) — will follow REST convention (401/403 for auth failure, 400 for malformed input, 500 for internal errors) and be finalized in Design.
- Concurrency handling for simultaneous update/delete calls targeting the same match-key row — resolved via DB-level `ON CONFLICT`/row-locking in Design, not a product decision.
- Who can create/manage webhooks in the dashboard: `rbac-per-app` (granular admin/editor/viewer roles) is not yet shipped (`README.md` M4 — unchecked). Webhook management will follow the same access control already applied to other app-config resources (frontend apps, deploy config) today, and will automatically inherit granular roles once `rbac-per-app` ships — no new access-control mechanism introduced by this feature.

### Declined / Undiscussed Gray Areas → Assumptions

- None declined — every gray area raised during discussion was resolved with the user. Items under "Agent's Discretion" above are logged as spec assumptions (not user decisions) per the closure gate.

---

## Specific References

- Concrete use case: Google Workspace admin events (user created/edited/deleted) pushed into an `employees` table, matching Google's user id to a column for update/delete targeting.
- UI reference mentioned: n8n's canvas-style field mapping — explicitly deferred past V1, replaced with a simpler click-to-link picker for this spec.

---

## Deferred Ideas

- **Outbound webhooks** (Orbit fires HTTP calls on row insert/update/delete) — the original roadmap item (`ROADMAP.md:46`). Confirmed as a separate future feature, not in this boundary.
- **Drag-and-drop mapping canvas** — n8n-style free-form UI. Deferred past V1's simpler click-to-link picker.
