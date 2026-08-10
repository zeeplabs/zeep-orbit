# Inbound Webhooks Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/inbound-webhooks/design.md`
**Status**: Draft — pending approval

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` (section 3 — gate commands). Backend follows the existing integration-test pattern in `table_policies_store_test.go`/`table_policies_handler_test.go` (real Postgres pool, one test per outcome branch). Frontend has no component-level unit framework (confirmed by the `table-policy-edit`/`enduser-roles-config` sibling features) — only Playwright e2e (`internal/dashboard/ui/e2e/*.spec.ts`). `internal/webhookengine` is new, dependency-free domain logic — gets real unit tests since it needs no DB/HTTP fixture.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration DDL (`provisioner.go` CREATE TABLE) | none | build gate only | `internal/dashboard/provisioner.go` | `go build ./...` |
| Mapping engine (`internal/webhookengine/mapping.go`) | unit | All branches: nested path, array index, missing path, type mismatch — 1:1 to the resolution logic in design.md | `internal/webhookengine/mapping_test.go` | `go test ./internal/webhookengine/...` |
| Backend stores (`webhooks_store.go`, `webhook_deliveries_store.go`) | integration | Every listed edge case: happy path, not-found, soft-delete visibility, dedup lookup, purge cutoff — same depth as `TestCreateTablePolicy_*` | `internal/dashboard/webhooks_store_test.go`, `internal/dashboard/webhook_deliveries_store_test.go` | `go test ./internal/dashboard/...` |
| Public delivery handler (`internal/server/webhook_handler.go`) | integration | Every outcome branch from the Error Handling Strategy table: method mismatch, invalid token, malformed body, captured, unmapped, duplicate-skipped, inserted, updated, deleted, row-not-found, write-error | `internal/server/webhook_handler_test.go` | `go test ./internal/server/...` |
| Dashboard config handler (`internal/dashboard/webhooks_handler.go`) | integration | Happy path + validation errors (zero-mapping activate, unknown table/column, unknown webhook id) — same depth as `TestUpdateTablePolicyHandler_*` | `internal/dashboard/webhooks_handler_test.go` | `go test ./internal/dashboard/...` |
| Retention wiring (`cmd/zeep/main.go`) | none | build gate only | `cmd/zeep/main.go` | `go build ./...` |
| Frontend types/hooks (`api.ts`) | none | build gate only | `internal/dashboard/ui/src/lib/api.ts` | `npm run build` |
| Frontend UI flow (create → capture → map → activate → deliveries) | e2e | One Playwright spec covering: create webhook, capture a test payload, build a mapping, activate, confirm a mapped write, view delivery log entries | `internal/dashboard/ui/e2e/webhooks.spec.ts` | `cd internal/dashboard/ui && npx playwright test webhooks` |

## Gate Check Commands

> Generated from `AGENTS.md` section 3 and `internal/dashboard/ui/package.json`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Backend-only task, no frontend touched | `go build ./... && go test ./internal/dashboard/... ./internal/server/... ./internal/webhookengine/... && go vet ./internal/dashboard/... ./internal/server/... ./internal/webhookengine/...` |
| Full | Task touches frontend types/hooks/UI, no new/changed e2e spec yet | Quick gate + `cd internal/dashboard/ui && npm run build` |
| Build | Phase completion, or task adds/changes the Playwright e2e spec | Full gate + `cd internal/dashboard/ui && npx playwright test webhooks` + `gofmt -l <changed .go files>` |

---

## Execution Plan

Phases are ordered and run sequentially — each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Data Layer & Migration

```
T1 -> T2
T2 -> T3
T1 -> T4
```

### Phase 2: Delivery Engine

```
T2 -> T6
T4 -> T6
T6 -> T7
T3 -> T7
T5 -> T7
T7 -> T8
```

### Phase 3: Dashboard API & Retention

```
T2 -> T9
T9 -> T10
T3 -> T10
T9 -> T11
T4 -> T11
T4 -> T12
```

### Phase 4: Frontend

```
T9 -> T13
T10 -> T13
T11 -> T13
T13 -> T14
T13 -> T15
T14 -> T15
T13 -> T16
```

### Phase 5: End-to-End Validation

```
T6 -> T17
T7 -> T17
T8 -> T17
T14 -> T17
T15 -> T17
T16 -> T17
```

---

## Task Breakdown

### T1: Add webhook catalog tables to the provisioner

**What**: Add `CREATE TABLE IF NOT EXISTS` (+ any `ALTER TABLE ADD COLUMN IF NOT EXISTS`) for `zeep_system.webhook_subscriptions`, `zeep_system.webhook_event_mappings`, `zeep_system.webhook_deliveries`, matching the Data Models in `design.md`.
**Where**: `internal/dashboard/provisioner.go`
**Depends on**: None
**Reuses**: Existing statement-list pattern (e.g. `table_policies` block at `provisioner.go:324`)
**Requirement**: WEBHOOK-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] All three tables created idempotently, columns match `design.md` Data Models exactly (types, nullability, FKs, unique constraint on `webhook_event_mappings(webhook_id, event_type_value)`)
- [x] `webhook_deliveries.webhook_id` has no `ON DELETE CASCADE`
- [x] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Status**: ✅ Complete

---

### T2: `WebhookConfigStore` — subscription CRUD

**What**: Implement subscription lifecycle: `CreateWebhook` (generates token, stores only its SHA-256 hash, returns plaintext once), `GetWebhookByID`, `ListWebhooks`, `StoreCapturedSample`, `ActivateWebhook` (returns `ErrNoMappings` if zero mappings), `RotateToken`, `SoftDeleteWebhook`.
**Where**: `internal/dashboard/webhooks_store.go`
**Depends on**: T1
**Reuses**: `table_policies_store.go` CRUD shape; `handler.go:1794` `generateToken` pattern (32-byte `crypto/rand`) for the plaintext token before hashing
**Requirement**: WEBHOOK-01, WEBHOOK-21, WEBHOOK-22

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Every method above implemented against a real Postgres pool
- [x] `SoftDeleteWebhook` sets `deleted_at`, never issues `DELETE`
- [x] `ActivateWebhook` on a webhook with zero mappings returns `ErrNoMappings`, no state change
- [x] Token is never persisted in plaintext (verified in test by asserting `token_hash` differs from the returned plaintext and re-deriving the hash matches)
- [x] Gate check passes: `go test ./internal/dashboard/...`
- [x] Test count: at least 6 tests pass (happy path create, get, list, capture-overwrite, activate-empty-mapping-error, rotate, soft-delete) — no silent deletions (7 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T3: `WebhookConfigStore` — event mapping CRUD

**What**: Implement `SaveEventMapping` (validates target table/columns exist via `registry.GetTable`, requires `match_key_column` when action is `update`/`delete`), `ListEventMappings`, `DeleteEventMapping`.
**Where**: `internal/dashboard/webhooks_store.go` (extends T2's file)
**Depends on**: T2
**Reuses**: `registry.GetTable` (`internal/registry/registry.go:110`) for target validation
**Requirement**: WEBHOOK-07, WEBHOOK-13, WEBHOOK-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `SaveEventMapping` rejects an unknown table or column with a typed validation error (not a raw DB error)
- [x] `SaveEventMapping` rejects `update`/`delete` action with no `match_key_column`
- [x] Unique constraint violation on `(webhook_id, event_type_value)` surfaces as a typed conflict error
- [x] Gate check passes: `go test ./internal/dashboard/...`
- [x] Test count: at least 5 new tests (happy path insert/update/delete mapping, unknown table rejected, missing match key rejected, duplicate event-type-value conflict) (7 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T4: `WebhookDeliveryStore`

**What**: Implement `InsertDelivery`, `ListDeliveries` (paginated, reverse-chronological), `PurgeExpiredDeliveries(retentionDays)`.
**Where**: `internal/dashboard/webhook_deliveries_store.go`
**Depends on**: T1
**Reuses**: `purge.go:26` `PurgeExpiredSoftDeletes` signature/return convention
**Requirement**: WEBHOOK-06, WEBHOOK-18, WEBHOOK-19, WEBHOOK-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `InsertDelivery` accepts every `outcome` value from `design.md`'s Data Models
- [x] `ListDeliveries` returns newest-first
- [x] `PurgeExpiredDeliveries` removes only rows older than the cutoff, leaves newer rows and rows from other webhooks untouched
- [x] Gate check passes: `go test ./internal/dashboard/...`
- [x] Test count: at least 4 tests (insert+list happy path, list ordering, purge removes old only, purge no-op when nothing expired) (5 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T5: Mapping engine — path resolution and field mapping

**What**: Implement `ExtractPath(payload map[string]any, path string) (any, bool)` (dot-notation, numeric segments index into arrays) and `ResolveFields(payload map[string]any, mappings []FieldMapping) (map[string]any, error)`.
**Where**: `internal/webhookengine/mapping.go`
**Depends on**: None (pure, no dependency on T1-T4)
**Reuses**: Nothing existing — new dependency-free package per `design.md`
**Requirement**: WEBHOOK-07, WEBHOOK-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ExtractPath` resolves a flat key, a nested key, and an array-index segment (e.g. `items.0.id`)
- [x] `ExtractPath` returns `found=false` (no error) for a missing path
- [x] `ResolveFields` returns an error naming the missing source path when a mapping's `source_path` isn't found in the payload
- [x] Gate check passes: `go test ./internal/webhookengine/...`
- [x] Test count: at least 6 unit tests (flat, nested, array index, missing path, empty mappings, full multi-field resolution) (7 tests, all pass)

**Tests**: unit
**Gate**: quick

**Status**: ✅ Complete

---

### T6: Public webhook handler — routing, auth, and capture mode

**What**: Register `/hooks/{webhookId}/{token}` as a top-level route (outside `/dashboard` and the JWT-guarded `{app}/{table}` group). Implement `HandleWebhookDelivery`: method-mismatch → `404` (no log); token missing/invalid → `401` + delivery logged `invalid_token`; malformed body → `400` + delivery logged `malformed`; while `status = capture` → store payload as `captured_sample` (overwrite), delivery logged `captured`, respond `200`.
**Where**: `internal/server/webhook_handler.go`, route registration in `internal/server/server.go`
**Depends on**: T2, T4
**Reuses**: `WebhookConfigStore`, `WebhookDeliveryStore`
**Requirement**: WEBHOOK-01, WEBHOOK-02, WEBHOOK-03, WEBHOOK-04, WEBHOOK-05, WEBHOOK-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Route resolves for any HTTP method; handler itself 404s on a method mismatch against the stored config
- [x] Invalid/missing token rejected with `401`, delivery row written with `outcome = invalid_token`
- [x] Malformed JSON body rejected with `400`, delivery row written with `outcome = malformed`
- [x] A second capture-mode call overwrites the first `captured_sample`
- [x] Every branch above writes exactly one delivery row (except the 404 method-mismatch, per design)
- [x] Gate check passes: `go test ./internal/server/...`
- [x] Test count: at least 6 tests (method mismatch, missing token, wrong token, malformed body, first capture, overwrite capture) (8 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T7: Public webhook handler — active mode, insert action

**What**: Extend `HandleWebhookDelivery` for `status = active`: resolve event-type value via `event_type_path`; no matching mapping → `200` + logged `unmapped`; matching event id already processed → `200` + logged `duplicate_skipped`, no write; otherwise resolve fields via the mapping engine, run `query.BuildInsert` inside `WithRLSContext(Role: "webhook", Sub: webhookID)`; success → `200` + logged `inserted` with `target_row_id`; DB failure → `500` + logged `write_error` with the real error server-side only.
**Where**: `internal/server/webhook_handler.go` (extends T6)
**Depends on**: T6, T3, T5
**Reuses**: `query.BuildInsert` (`internal/query/builder.go:201`), `WithRLSContext` (`internal/db/client.go:116`), `MappingEngine.ExtractPath`/`ResolveFields`
**Requirement**: WEBHOOK-07, WEBHOOK-08, WEBHOOK-09, WEBHOOK-10, WEBHOOK-11, WEBHOOK-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Unmapped event-type value → `200`, logged `unmapped`, no write
- [x] Repeated event id (per `event_id_path`) → `200`, logged `duplicate_skipped`, no second row created
- [x] Valid insert mapping → row created in target table with mapped values, `200`, logged `inserted`
- [x] Missing/insufficient RLS permission on target table (role `webhook` not permitted) → `500`, logged `write_error`, response body contains no raw DB error text
- [x] Gate check passes: `go test ./internal/server/...`
- [x] Test count: at least 5 new tests (unmapped, duplicate, insert happy path, insert RLS-denied write error, insert with missing mapped field) (5 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T8: Public webhook handler — update and delete actions

**What**: Extend `HandleWebhookDelivery` for `update`/`delete` mappings: resolve the match-key value via the mapping engine, look up the row's id inside the same `WithRLSContext` transaction; no match → `200` + logged `row_not_found`; match found → `query.BuildUpdate` (overwrite linked columns) or `query.BuildDelete` (respecting `SystemConfig().SoftDeleteEnabled`); success → `200` + logged `updated`/`deleted`.
**Where**: `internal/server/webhook_handler.go` (extends T7)
**Depends on**: T7
**Reuses**: `query.BuildUpdate`/`BuildDelete` (`internal/query/builder.go:262,331`), `h.reg.SystemConfig().SoftDeleteEnabled` (soft-delete convention from `handler.go:313`)
**Requirement**: WEBHOOK-13, WEBHOOK-14, WEBHOOK-15, WEBHOOK-16, WEBHOOK-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `update` mapping with a matching row overwrites only the linked columns, `200`, logged `updated`
- [x] `delete` mapping with a matching row removes it per the app's soft-delete config, `200`, logged `deleted`
- [x] `update`/`delete` with no row matching the match-key value → `200`, logged `row_not_found`, no write
- [x] A single webhook holding an `insert`, an `update`, and a `delete` mapping for different event-type values all resolve correctly in sequence (the Google Workspace employee-sync scenario from `context.md`)
- [x] Gate check passes: `go test ./internal/server/...`
- [x] Test count: at least 5 new tests (update happy path, delete happy path, update-not-found, delete-not-found, full create→update→delete sequence) (5 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T9: Dashboard handler — webhook subscription CRUD

**What**: Implement `CreateWebhook`, `ListWebhooks`, `GetWebhook`, `RotateWebhookToken`, `DeleteWebhook` dashboard endpoints, each RBAC-checked and calling `h.audit(...)` on mutation, mounted under `/dashboard/api/apps/{id}/webhooks` behind `dashboard.RequireAuth(pool)`.
**Where**: `internal/dashboard/webhooks_handler.go`, route registration in `internal/server/server.go`
**Depends on**: T2
**Reuses**: RBAC/audit pattern from `handler.go`'s `UpdateTablePolicy`
**Requirement**: WEBHOOK-01, WEBHOOK-21, WEBHOOK-22, WEBHOOK-23

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Create returns the plaintext token exactly once in the response body, never again on subsequent `Get`/`List`
- [x] Rotate and delete each produce one `audit_log` entry with the correct action name
- [x] Delete is a soft-delete visible in the store but the webhook's URL now 404s (verified via T6's route)
- [x] Gate check passes: `go test ./internal/dashboard/...`
- [x] Test count: at least 5 tests (create, list, rotate invalidates old token, delete soft-deletes, unauthorized rejected) (5 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T10: Dashboard handler — mapping CRUD and activation

**What**: Implement `SaveEventMapping`, `ListEventMappings`, `DeleteEventMapping`, `ActivateWebhook` dashboard endpoints (same auth/audit pattern as T9).
**Where**: `internal/dashboard/webhooks_handler.go` (extends T9)
**Depends on**: T9, T3
**Reuses**: T9's handler scaffolding
**Requirement**: WEBHOOK-07, WEBHOOK-08, WEBHOOK-13, WEBHOOK-23

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Save mapping surfaces the store's validation errors (unknown table/column, missing match key) as `400`, not `500`
- [x] Activate on a webhook with zero mappings returns `400` with a clear message, not a silent no-op
- [x] Every mutation produces an `audit_log` entry
- [x] Gate check passes: `go test ./internal/dashboard/...`
- [x] Test count: at least 5 new tests (save mapping happy path, unknown table 400, activate-empty 400, activate-success, delete mapping) (5 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T11: Dashboard handler — delivery log listing

**What**: Implement `ListWebhookDeliveries` (paginated) dashboard endpoint.
**Where**: `internal/dashboard/webhooks_handler.go` (extends T10)
**Depends on**: T9, T4
**Reuses**: `WebhookDeliveryStore.ListDeliveries`
**Requirement**: WEBHOOK-18, WEBHOOK-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Endpoint returns deliveries newest-first with raw payload and error detail included per entry
- [x] Gate check passes: `go test ./internal/dashboard/...`
- [x] Test count: at least 2 new tests (happy path listing, empty list for a fresh webhook) (2 tests, all pass)

**Tests**: integration
**Gate**: quick

**Status**: ✅ Complete

---

### T12: Wire delivery-log retention into the existing purge ticker

**What**: Add a call to `WebhookDeliveryStore.PurgeExpiredDeliveries(ctx, pool, 30)` inside the existing periodic goroutine, alongside `PurgeExpiredSoftDeletes`.
**Where**: `cmd/zeep/main.go` (extends the ticker at lines ~116-134)
**Depends on**: T4
**Reuses**: The existing `time.Ticker`/boot-then-tick pattern already running `PurgeExpiredSoftDeletes`
**Requirement**: WEBHOOK-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Purge call runs once at boot and on every subsequent tick, same as the existing soft-delete purge
- [x] No new goroutine or ticker introduced — reuses the existing one
- [x] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Status**: ✅ Complete

---

### T13: Frontend — webhook types and API hooks

**What**: Add `WebhookSubscription`, `WebhookEventMapping`, `WebhookDelivery` types and TanStack Query hooks (`useWebhooks`, `useCreateWebhook`, `useSaveEventMapping`, `useActivateWebhook`, `useRotateWebhookToken`, `useDeleteWebhook`, `useWebhookDeliveries`) to `src/lib/api.ts`, each with `onError: toast.error`.
**Where**: `internal/dashboard/ui/src/lib/api.ts`
**Depends on**: T9, T10, T11
**Reuses**: Existing mutation-hook pattern (e.g. `useUpdateTablePolicy`)
**Requirement**: WEBHOOK-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Types match the backend response shapes from T9-T11 exactly
- [x] Every mutation hook invalidates the correct query key and has `onError: toast.error`
- [x] Gate check passes: `npm run build`

**Tests**: none
**Gate**: full

**Status**: ✅ Complete

---

### T14: Frontend — webhook list and creation UI

**What**: `Webhooks.tsx`: list an app's webhooks (name, status, URL), create-webhook form (name, HTTP method, event-type path, optional event-id path), shows the generated URL+token once on creation. i18n keys added to `en.json` and `pt-BR.json` in the same change.
**Where**: `internal/dashboard/ui/src/components/Webhooks.tsx`
**Depends on**: T13
**Reuses**: Existing table/list component conventions from `TablePolicies.tsx`
**Requirement**: WEBHOOK-01, WEBHOOK-04, WEBHOOK-21, WEBHOOK-22

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Every user-facing string routed through `t()`, present in both locale files
- [x] Create form's method field offers the same options as the backend accepts (GET/POST/PUT/PATCH)
- [x] Gate check passes: `npm run build`

**Tests**: none
**Gate**: full

**Status**: ✅ Complete

---

### T15: Frontend — mapping editor

**What**: `WebhookMappingEditor.tsx`: shows the captured sample as a JSON tree on one side and the target table's columns on the other; click a field then a column to link it; choose action (insert/update/delete) and match key (when applicable) per event-type value; save mapping; activate button (disabled until ≥1 mapping saved).
**Where**: `internal/dashboard/ui/src/components/WebhookMappingEditor.tsx`
**Depends on**: T13, T14
**Reuses**: TanStack Query mutation pattern; existing table-column-listing utilities used by `TablePolicies.tsx`
**Requirement**: WEBHOOK-07, WEBHOOK-08, WEBHOOK-13, WEBHOOK-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Activate button disabled with zero saved mappings, matching the backend's `400`
- [x] Match-key field only shown/required for `update`/`delete` actions
- [x] i18n keys added to both locale files
- [x] Gate check passes: `npm run build`

**Tests**: none
**Gate**: full

**Status**: ✅ Complete

---

### T16: Frontend — delivery log view

**What**: Read-only list of a webhook's deliveries (timestamp, outcome badge, event-type value), expandable to show raw payload and error detail.
**Where**: `internal/dashboard/ui/src/components/Webhooks.tsx` (extends T14, or a co-located sub-component if the file grows past a cohesive size)
**Depends on**: T13
**Reuses**: `useWebhookDeliveries` hook from T13
**Requirement**: WEBHOOK-18, WEBHOOK-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Outcome values are visually distinguishable (e.g. success vs error styling)
- [ ] i18n keys added to both locale files
- [ ] Gate check passes: `npm run build`

**Tests**: none
**Gate**: full

---

### T17: End-to-end Playwright test — full webhook lifecycle

**What**: One Playwright spec: create a webhook, `POST` a sample payload directly to its URL (simulating the external provider) and confirm no row is written, build a mapping in the UI, activate, `POST` a second real payload and confirm the mapped row is created, open the delivery log and confirm both calls are listed with the correct outcomes.
**Where**: `internal/dashboard/ui/e2e/webhooks.spec.ts`
**Depends on**: T6, T7, T8, T14, T15, T16
**Reuses**: Existing e2e helpers (`bootstrapOrSkip`, exact-match `getByRole` lesson from `enduser-roles.spec.ts` — avoid `:has-text()` substring collisions)
**Requirement**: WEBHOOK-01, WEBHOOK-02, WEBHOOK-06, WEBHOOK-07, WEBHOOK-08, WEBHOOK-09, WEBHOOK-13, WEBHOOK-14, WEBHOOK-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Full lifecycle passes against a real ephemeral Postgres + test server, same setup convention as `enduser-roles.spec.ts`
- [ ] Test asserts zero rows written during capture mode
- [ ] Test asserts exactly one row written after activation
- [ ] Gate check passes: `cd internal/dashboard/ui && npx playwright test webhooks`
- [ ] Test count: 1 e2e test (full lifecycle) passes

**Tests**: e2e
**Gate**: build

---

## Phase Execution Map

Phases run in sequence: Phase 1, then Phase 2, then Phase 3, then Phase 4, then Phase 5. Within a phase, tasks run in header order (T1 before T2 before T3 before T4, and so on). The exact dependency edges are the ones drawn under each phase heading above — this section is a plain-language restatement, not a second diagram, so there is exactly one source of truth for the dependency graph.

Execution is strictly sequential — there is no intra-phase parallelism.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add catalog tables to provisioner | 1 file, DDL only | ✅ Granular |
| T2: Subscription CRUD store | 1 file, 1 concept (subscription lifecycle) | ✅ Granular |
| T3: Mapping CRUD store | 1 file (extends T2), 1 concept (mapping lifecycle) | ✅ Granular |
| T4: Delivery store | 1 file, 1 concept | ✅ Granular |
| T5: Mapping engine | 1 file, 2 pure functions | ✅ Granular |
| T6: Public handler — routing/auth/capture | 1 file, 1 concept (pre-mapping request handling) | ✅ Granular |
| T7: Public handler — insert action | 1 file (extends T6), 1 concept (active insert dispatch) | ✅ Granular |
| T8: Public handler — update/delete | 1 file (extends T7), 1 concept | ✅ Granular |
| T9: Dashboard subscription CRUD | 1 file, 1 concept | ✅ Granular |
| T10: Dashboard mapping CRUD + activate | 1 file (extends T9), 1 concept | ✅ Granular |
| T11: Dashboard delivery listing | 1 file (extends T10), 1 endpoint | ✅ Granular |
| T12: Purge wiring | 1 file, 1 line of new logic | ✅ Granular |
| T13: Frontend types/hooks | 1 file, 1 concept (data layer) | ✅ Granular |
| T14: Webhook list/create UI | 1 component | ✅ Granular |
| T15: Mapping editor UI | 1 component | ✅ Granular |
| T16: Delivery log UI | 1 component (or co-located sub-component) | ✅ Granular |
| T17: E2E lifecycle test | 1 spec file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (Phase 1 start, no incoming edge) | ✅ Match |
| T2 | T1 | T1 -> T2 | ✅ Match |
| T3 | T2 | T2 -> T3 | ✅ Match |
| T4 | T1 | T1 -> T4 | ✅ Match |
| T5 | None | (Phase 2 start, no incoming edge) | ✅ Match |
| T6 | T2, T4 | T2 -> T6, T4 -> T6 | ✅ Match |
| T7 | T6, T3, T5 | T6 -> T7, T3 -> T7, T5 -> T7 | ✅ Match |
| T8 | T7 | T7 -> T8 | ✅ Match |
| T9 | T2 | T2 -> T9 | ✅ Match |
| T10 | T9, T3 | T9 -> T10, T3 -> T10 | ✅ Match |
| T11 | T9, T4 | T9 -> T11, T4 -> T11 | ✅ Match |
| T12 | T4 | T4 -> T12 | ✅ Match |
| T13 | T9, T10, T11 | T9 -> T13, T10 -> T13, T11 -> T13 | ✅ Match |
| T14 | T13 | T13 -> T14 | ✅ Match |
| T15 | T13, T14 | T13 -> T15, T14 -> T15 | ✅ Match |
| T16 | T13 | T13 -> T16 | ✅ Match |
| T17 | T6, T7, T8, T14, T15, T16 | T6 -> T17, T7 -> T17, T8 -> T17, T14 -> T17, T15 -> T17, T16 -> T17 | ✅ Match |

**Rule check**: every dependency points to a task in the same or an earlier phase — no forward-phase dependency exists.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Provisioner tables | Migration DDL | none | none | ✅ OK |
| T2: Subscription store | Backend store | integration | integration | ✅ OK |
| T3: Mapping store | Backend store | integration | integration | ✅ OK |
| T4: Delivery store | Backend store | integration | integration | ✅ OK |
| T5: Mapping engine | Mapping engine | unit | unit | ✅ OK |
| T6: Public handler (routing/auth/capture) | Public delivery handler | integration | integration | ✅ OK |
| T7: Public handler (insert) | Public delivery handler | integration | integration | ✅ OK |
| T8: Public handler (update/delete) | Public delivery handler | integration | integration | ✅ OK |
| T9: Dashboard subscription CRUD | Dashboard config handler | integration | integration | ✅ OK |
| T10: Dashboard mapping CRUD | Dashboard config handler | integration | integration | ✅ OK |
| T11: Dashboard delivery listing | Dashboard config handler | integration | integration | ✅ OK |
| T12: Purge wiring | Retention wiring | none | none | ✅ OK |
| T13: Frontend types/hooks | Frontend types/hooks | none | none | ✅ OK |
| T14: Webhook list/create UI | Frontend UI flow | e2e (covered by T17) | none (component itself), full build gate | ✅ OK — component tasks build-gate only; the matrix's "e2e" requirement is satisfied by T17, which exercises T14-T16 together, not deferred without a task |
| T15: Mapping editor UI | Frontend UI flow | e2e (covered by T17) | none, full build gate | ✅ OK — same as T14 |
| T16: Delivery log UI | Frontend UI flow | e2e (covered by T17) | none, full build gate | ✅ OK — same as T14 |
| T17: E2E lifecycle test | Frontend UI flow | e2e | e2e | ✅ OK — this is the task that actually satisfies the matrix's e2e requirement for T14-T16's combined flow |

**Note on T14-T16**: the matrix's "e2e" row for frontend UI flow is satisfied by the single lifecycle test in T17, which explicitly depends on T14, T15, and T16 and cannot pass until all three exist — this is the documented "merge forward" resolution from the Tasks process (untestable-until-wired UI tasks have their required test merged into the task where the flow becomes runnable end-to-end), not silent test deferral.

---

## MCPs and Skills

No project or user MCP configuration applies to this feature's tasks (backend Go, frontend React/TS, no external API integration requiring an MCP). Every task above is marked `MCP: NONE`, `Skill: NONE`. If you'd like a specific MCP or skill applied to any task (e.g. a Postgres MCP for the store tasks, or a frontend-design skill for T14-T16), say so before Execute starts — otherwise these tasks proceed with direct tool use only.
