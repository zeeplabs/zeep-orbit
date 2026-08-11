# Inbound Webhooks Design

**Spec**: `.specs/features/inbound-webhooks/spec.md`
**Context**: `.specs/features/inbound-webhooks/context.md`
**Status**: Draft

---

## Architecture Overview

A public, top-level route (no dashboard session, no end-user JWT) receives external calls. Its handler resolves the webhook by ID, verifies the URL-embedded token, then either captures the payload as a sample (capture mode) or applies the webhook's saved event-type→mapping to write a row (active mode) — reusing the existing generic write engine (`query.BuildInsert/Update/Delete`) and RLS transaction wrapper (`WithRLSContext`) exactly as end-user requests do, under a dedicated `webhook` RLS role. Every call, regardless of outcome, is logged to `webhook_deliveries`. Dashboard-side CRUD for webhook config and mapping lives alongside the existing `table_policies`-style handlers.

```mermaid
graph TD
    Ext[External Provider] -->|POST /hooks/{id}/{token}| PubH[Public Webhook Handler<br/>internal/server]
    PubH -->|token invalid| Log1[webhook_deliveries: invalid_token]
    PubH -->|capture mode| CaptureStore[Store payload as captured_sample]
    PubH -->|active mode| Engine[Mapping Engine<br/>internal/webhookengine]
    Engine -->|event type unmapped| Log2[webhook_deliveries: unmapped]
    Engine -->|duplicate event id| Log3[webhook_deliveries: duplicate_skipped]
    Engine -->|resolved fields| RLS[WithRLSContext role=webhook]
    RLS --> Build[query.BuildInsert/Update/Delete]
    Build --> DB[(App table, schema-per-app)]
    Build --> Log4[webhook_deliveries: inserted/updated/deleted/row_not_found/write_error]

    Dash[Dashboard UI] -->|CRUD| DashH[Webhook Dashboard Handler<br/>internal/dashboard]
    DashH --> ConfigStore[(webhook_subscriptions /<br/>webhook_event_mappings)]
    DashH -->|view| Log4

    Ticker[cmd/zeep/main.go ticker, existing 6h loop] --> Purge[PurgeExpiredWebhookDeliveries]
    Purge --> DB2[(webhook_deliveries, 30d retention)]
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
|---|---|---|
| `query.BuildInsert/BuildUpdate/BuildDelete` | `internal/query/builder.go:201,262,331` | Called directly with a resolved `map[string]any` — no HTTP request needed, already the case for these functions. |
| `Pool.WithRLSContext` | `internal/db/client.go:116` | Wraps every webhook-driven write in the same RLS-enforcing transaction end-user requests use, with `RLSClaims{Role: "webhook", Sub: webhookID}`. |
| `registry.Get` / `registry.GetTable` | `internal/registry/registry.go:102,110` | Validates a mapping's target table/columns exist before saving, and resolves `SchemaName` at delivery time. |
| Soft-delete config + purge pattern | `internal/dashboard/system_config_store.go:20`, `internal/dashboard/purge.go:26` | `delete` mappings pass `h.reg.SystemConfig().SoftDeleteEnabled` into `BuildDelete`, same as `HandleDelete` (`internal/server/handler.go:313`) — no new soft-delete logic. |
| `table_policies_store.go` CRUD shape | `internal/dashboard/table_policies_store.go` | Structural template for `webhook_subscriptions`/`webhook_event_mappings` stores — catalog row + `created_by`/`updated_by`, same error-sentinel pattern (`ErrPolicyNotFound`-style). |
| Provisioner migration pattern | `internal/dashboard/provisioner.go` (`CREATE TABLE IF NOT EXISTS` + `ALTER TABLE ADD COLUMN IF NOT EXISTS`) | New catalog tables added the same way, no versioned migration framework introduced. |
| Periodic purge ticker | `cmd/zeep/main.go:116-134` | Extended with one more call (`PurgeExpiredWebhookDeliveries`) inside the existing 6-hour tick — no new goroutine. |
| Random token generation | `internal/dashboard/handler.go:1794` (`generateToken`, `crypto/rand`, 32 bytes) | Reused verbatim for the webhook token; hashing before storage is new (see Tech Decisions). |

### Integration Points

| System | Integration Method |
|---|---|
| Router (`internal/server/server.go`) | New top-level route `/hooks/{webhookId}/{token}`, registered outside `/dashboard` and outside the `{app}/{table}` JWT-guarded group — same tier as `/health` (`server.go:149`). |
| `table_policies` (RLS) | Webhook writes carry `app.jwt_role = "webhook"`. An app owner must add `"webhook"` to a table's policy `roles` (existing table-policy-edit UI) for webhook writes to pass — no bypass, no new RLS mechanism. |
| `audit_log` | Webhook config actions (create/edit/delete/rotate token) call the existing `h.audit(...)` wrapper, same as every other dashboard resource. |

---

## Components

### `WebhookConfigStore`

- **Purpose**: CRUD for webhook subscriptions and their per-event-type mappings.
- **Location**: `internal/dashboard/webhooks_store.go`
- **Interfaces**:
  - `CreateWebhook(ctx, pool, input CreateWebhookInput) (WebhookRow, plaintextToken string, error)` — generates token, stores its SHA-256 hash only.
  - `GetWebhookByID(ctx, pool, appID, webhookID string) (WebhookRow, error)`
  - `ListWebhooks(ctx, pool, appID string) ([]WebhookRow, error)`
  - `StoreCapturedSample(ctx, pool, webhookID string, payload []byte) error` — capture-mode overwrite.
  - `ActivateWebhook(ctx, pool, webhookID string) error` — rejects with `ErrNoMappings` if zero mappings exist.
  - `RotateToken(ctx, pool, webhookID string) (plaintextToken string, error)`
  - `SoftDeleteWebhook(ctx, pool, webhookID string) error` — sets `deleted_at`, never a hard `DELETE` (keeps `webhook_deliveries` FK intact per WEBHOOK-22).
  - `SaveEventMapping(ctx, pool, webhookID string, def EventMappingDef) (EventMappingRow, error)`
  - `ListEventMappings(ctx, pool, webhookID string) ([]EventMappingRow, error)`
  - `DeleteEventMapping(ctx, pool, mappingID string) error`
- **Dependencies**: `db.Pool`, `registry.Registry` (to validate target table/columns on save).
- **Reuses**: Structural pattern from `table_policies_store.go`.

### Public Webhook Handler

- **Purpose**: Receives external provider calls, resolves capture-vs-active behavior, always logs.
- **Location**: `internal/server/webhook_handler.go`
- **Interfaces**:
  - `(h *Handler) HandleWebhookDelivery(w http.ResponseWriter, r *http.Request)` — single entrypoint for the `/hooks/{webhookId}/{token}` route (method-agnostic registration; the handler itself checks the stored method against `r.Method` and returns `404` on mismatch, per WEBHOOK-04, before touching the token).
- **Dependencies**: `WebhookConfigStore`, `WebhookDeliveryStore`, `MappingEngine`, `db.Pool`, `registry.Registry`.
- **Reuses**: `query.BuildInsert/Update/Delete`, `WithRLSContext`, soft-delete config.

### `MappingEngine`

- **Purpose**: Pure functions that turn a raw payload + saved mapping into a write-ready `map[string]any`, with no I/O.
- **Location**: `internal/webhookengine/mapping.go` (new package — kept dependency-free and unit-testable without a DB).
- **Interfaces**:
  - `ExtractPath(payload map[string]any, path string) (value any, found bool)` — resolves a dot-notation path (e.g. `user.id`, `items.0.email`); custom resolver, see Tech Decisions.
  - `ResolveFields(payload map[string]any, mappings []FieldMapping) (map[string]any, error)` — applies every `source_path → column` link.
- **Dependencies**: none (pure).
- **Reuses**: nothing — this is new domain logic; no existing path-resolution utility found in the codebase.

### `WebhookDeliveryStore`

- **Purpose**: Append-only delivery log + retention purge.
- **Location**: `internal/dashboard/webhook_deliveries_store.go`
- **Interfaces**:
  - `InsertDelivery(ctx, pool, entry DeliveryEntry) error`
  - `ListDeliveries(ctx, pool, webhookID string, limit, offset int) ([]DeliveryRow, error)`
  - `PurgeExpiredDeliveries(ctx, pool, retentionDays int) (int, error)`
- **Dependencies**: `db.Pool`.
- **Reuses**: Shape of `purge.go:26`'s `PurgeExpiredSoftDeletes` for the purge function signature/return convention.

### Dashboard Webhook Handler

- **Purpose**: Dashboard-session-authenticated CRUD for webhooks, mappings, and delivery viewing.
- **Location**: `internal/dashboard/webhooks_handler.go`
- **Interfaces**: `CreateWebhook`, `ListWebhooks`, `GetWebhook`, `SaveEventMapping`, `DeleteEventMapping`, `ActivateWebhook`, `RotateWebhookToken`, `DeleteWebhook`, `ListWebhookDeliveries` — mirrors `TablePolicies` handler pattern (RBAC check → validate → store call → `h.audit(...)` → JSON response), mounted under `/dashboard/api/apps/{id}/webhooks/...` in `server.go` behind `dashboard.RequireAuth(pool)`.
- **Dependencies**: `WebhookConfigStore`, `WebhookDeliveryStore`.
- **Reuses**: RBAC/audit pattern from `handler.go`'s existing resource handlers (e.g. `UpdateTablePolicy`).

### Frontend: Webhook management

- **Purpose**: Create webhooks, view captured sample, build field→column mappings by click-linking, view delivery log.
- **Location**: `internal/dashboard/ui/src/components/Webhooks.tsx` (list + create) and `WebhookMappingEditor.tsx` (JSON-tree-left / column-list-right picker + delivery log table).
- **Interfaces**: React Query hooks in `src/lib/api.ts` — `useWebhooks(appId)`, `useCreateWebhook`, `useSaveEventMapping`, `useActivateWebhook`, `useWebhookDeliveries(webhookId)`.
- **Dependencies**: existing `TablePolicies.tsx`/`api.ts` conventions (mutation + `toast.error` on failure, i18n keys in both locale files).
- **Reuses**: TanStack Query mutation pattern, `LoadingScreen`/table UI primitives already in the dashboard.

---

## Data Models

### `webhook_subscriptions` (zeep_system)

```typescript
interface WebhookSubscription {
  id: string
  app_id: string
  name: string
  method: "GET" | "POST" | "PUT" | "PATCH"
  token_hash: string        // sha256 hex, never the plaintext token
  event_type_path: string   // dot-path into payload, e.g. "eventType"
  event_id_path: string | null  // dot-path used for dedup, optional
  status: "capture" | "active"
  captured_sample: object | null  // last received payload while in capture mode
  deleted_at: string | null       // soft-delete; URL rejects all calls once set
  created_by: string
  created_at: string
  updated_at: string
}
```

### `webhook_event_mappings` (zeep_system)

```typescript
interface WebhookEventMapping {
  id: string
  webhook_id: string
  event_type_value: string        // matches the value found at event_type_path
  action: "insert" | "update" | "delete"
  target_table: string
  match_key_column: string | null  // required when action is update/delete
  field_mappings: Array<{ source_path: string; column: string }>
  created_at: string
  updated_at: string
}
```

**Relationships**: `webhook_event_mappings.webhook_id → webhook_subscriptions.id`; unique on `(webhook_id, event_type_value)` — one mapping per event-type value per webhook (WEBHOOK-13/17).

### `webhook_deliveries` (zeep_system)

```typescript
interface WebhookDelivery {
  id: string
  webhook_id: string
  received_at: string
  http_status: number
  outcome:
    | "captured" | "inserted" | "updated" | "deleted"
    | "unmapped" | "duplicate_skipped" | "row_not_found"
    | "invalid_token" | "malformed" | "write_error"
  event_type_value: string | null
  event_id: string | null           // extracted via event_id_path, used for dedup lookups
  raw_payload: object
  target_row_id: string | null
  error_detail: string | null       // populated only on write_error; dashboard-visible, never returned to the caller
}
```

**Relationships**: `webhook_id → webhook_subscriptions.id` (no `ON DELETE CASCADE` — soft-delete on the parent keeps this row alive for its own 30-day retention window, per WEBHOOK-22).

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
|---|---|---|
| Method mismatch | `404`, not logged (route doesn't apply to this webhook) | Provider sees plain `404`. |
| Missing/invalid token | `401`, delivery logged as `invalid_token` | Provider sees `401`; app owner sees the attempt in the delivery log. |
| Malformed JSON body | `400`, delivery logged as `malformed` | Provider sees `400`. |
| Unmapped event-type value | `200`, delivery logged as `unmapped` | Provider sees success (no retry storm); app owner sees "unmapped event" in the log. |
| Duplicate event id (already processed) | `200`, delivery logged as `duplicate_skipped`, no write | Provider sees success; no duplicate row created. |
| No row for match key (update/delete) | `200`, delivery logged as `row_not_found`, no write | Provider sees success; app owner can spot the mismatch in the log. |
| Target table's RLS policy doesn't permit role `webhook` | `500`, delivery logged as `write_error` with the Postgres permission error captured server-side | Provider sees generic `500`; app owner sees the real error only in the dashboard delivery log, never in the HTTP response (per `AGENTS.md` §4). |
| Any other DB write failure | `500`, delivery logged as `write_error`, generic message returned | Same as above. |
| Call to a deleted webhook's URL | `404` | Provider sees plain `404`; no delivery logged (subscription itself is gone from the routable set). |
| Activate with zero mappings | `400` from the dashboard handler, `ErrNoMappings` | App owner sees a validation error in the dashboard, not a silent no-op activation. |

---

## Risks & Concerns

| Concern | Location (file:line) | Impact | Mitigation |
|---|---|---|---|
| Webhook writes require the target table's `table_policies` to explicitly permit role `"webhook"` — easy to forget, silent `write_error` otherwise | `internal/dashboard/table_policies_store.go` (RLS role model) | App owner maps fields, activates, then every real call 500s until they separately edit the table's RLS policy | Dashboard mapping editor checks the target table's current policies when a mapping is saved and shows an inline warning if `"webhook"` isn't yet permitted — non-blocking, but visible before activation. |
| No existing bearer-token-hash pattern in the codebase (existing tokens are JWTs verified by signature, not raw secrets compared by value) | `internal/dashboard/handler.go:1794`, `internal/dashboard/app_tokens_store.go:15` | First hashed-secret-at-rest pattern in this codebase; a future reviewer might assume plaintext storage is the convention | Documented explicitly in Tech Decisions below; `token_hash` column name makes the convention obvious at the schema level. |
| Fully synchronous delivery handling — no queue, no retry on our side | Confirmed: no queue/worker infra exists in the repo today | A burst of concurrent deliveries is bound by normal request-level DB pool concurrency; very high-throughput providers aren't well served | Accepted for V1 given expected traffic (admin-event-style providers, not high-frequency streams); revisit with an async/queue design only if real usage data shows it's needed. |
| Match-key lookup and the subsequent update/delete run as two statements inside one `WithRLSContext` transaction | New: `internal/server/webhook_handler.go` | Two concurrent deliveries for the same match-key value could race between lookup and write | Both statements run inside the same transaction opened by `WithRLSContext`; Postgres's normal transaction isolation bounds this to the same level of risk the generic `HandleUpdate`/`HandleDelete` paths already accept — not a new class of race introduced by this feature. |

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
|---|---|---|
| Token storage | SHA-256 hash of a 32-byte `crypto/rand` token, never the plaintext, after creation/rotation | Token is high-entropy (256 bits), not a human password — a slow hash (bcrypt, used for user passwords at `internal/auth/handler.go:97`) buys nothing here and adds latency to every inbound call; SHA-256 + constant-time compare is the right tool for a random secret. |
| RLS identity for webhook writes | `RLSClaims{Role: "webhook", Sub: <webhook_id>}` inside `WithRLSContext` | Reuses the exact mechanism `table_policies` already checks (`app.jwt_role`) instead of introducing a second, parallel authorization path or bypassing RLS outright. |
| Field path resolution | Custom dot-notation resolver (`a.b.0.c`), not a JSONPath library | Target payloads (Google Workspace-style admin events) are shallow/flat; a full JSONPath engine is an unjustified new dependency for this shape of data. |
| Webhook deletion | Soft-delete (`deleted_at`), no hard `DELETE` | `webhook_deliveries` has no cascade; the delivery log must survive its parent's deletion until the independent 30-day purge (WEBHOOK-22). |
| Delivery retention job | Extends the existing `time.Ticker` loop in `cmd/zeep/main.go:116-134` with one more purge call | No cron dependency exists in the repo; the soft-delete purge already established this exact pattern six hours at a time. |
| Duplicate-event dedup storage | `event_id` column directly on `webhook_deliveries`, queried by `(webhook_id, event_id)` before processing — no separate dedup table | Avoids a second table for what is just an existence check; the delivery log already has to be written for every call regardless. |

---

## Tips

None — see feature Tips in `references/design.md` for general guidance; nothing feature-specific to add beyond what's captured in Risks & Concerns.
