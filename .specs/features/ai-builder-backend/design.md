# AI Builder Backend Design

**Spec**: `.specs/features/ai-builder-backend/spec.md`
**Status**: Draft (2026-10-28). Items marked **[verify]** are assumptions about code I read only partially; the task that touches them starts by confirming it.

---

## 1. What exists and is reused (read from the code)

| Piece | Where | Use here |
| --- | --- | --- |
| `CreateAppForUser(ctx, user, AppRequestBody, ip)` | `internal/dashboard/handler.go` | BBE-01: create the backend. Same path as `POST /api/apps` and MCP `orbit_create_app`. Route only needs `RequireAuth`. |
| `DeleteApp(ctx, pool, appID, user)` | `apps_store.go` | BBE-02 compensation. **[verify]** that it also drops the provisioned schema and unregisters the app from the registry (the handler `Handler.DeleteApp` may do more than the store function). |
| `applyEditOperation(ctx, user, appID, op, origin)` | `ai_edit_chat_handlers.go` | BBE-08: applies each operation through the `*ForUser` functions with RBAC (`CanWrite` via `GetApp`) and audit origin `ai_builder`. |
| `ParseBuilderProposal`, `proposeBackendChangeSchema` | `builder_tools.go` | Single-op validator and JSON schema. The plan reuses the per-operation validation. |
| `ConfirmProposal` / `RejectProposal`, `store.ResolveProposal`, `PendingProposal` | `builder_proposals.go`, `internal/agent/store/proposals.go` | The pause/confirm machinery. A plan is one tool call with one argument blob, so it fits `pending_proposal` unchanged. |
| `CreateTablePolicyForUser(ctx, user, appID, table, PolicyDef, ip)` and `internal/policytemplates` | `handler.go`, `policytemplates/` | BBE-11. |
| `UpdateTableRLSModeForUser` | `handler.go` | Already behind `set_rls_mode`. |
| `skills.DetectCredentials` | `internal/agent/skills/secrets.go` | BBE-16. |
| Loop pause for proposals | `internal/agent/loop.go` (the `kind == tools.KindProposal || KindExternalMutating` branch) | BBE-15 needs a way for a proposal handler to resolve inline. |

## 2. Data model (DDL, higher-risk, AGENTS.md §8: show the diff and get confirmation before B01)

```sql
ALTER TABLE zeep_system.builder_sessions
  ADD COLUMN IF NOT EXISTS backend_origin TEXT NOT NULL DEFAULT 'existing'
  CHECK (backend_origin IN ('existing', 'builder'));

ALTER TABLE zeep_system.builder_settings
  ADD COLUMN IF NOT EXISTS backend_approval_mode TEXT NOT NULL DEFAULT 'ask'
  CHECK (backend_approval_mode IN ('ask', 'auto')),
  ADD COLUMN IF NOT EXISTS publish_rls_gate TEXT NOT NULL DEFAULT 'warn'
  CHECK (publish_rls_gate IN ('warn', 'block'));
```

Additive, defaulted, idempotent (`IF NOT EXISTS`); existing rows keep today's behavior (`existing`, `ask`, `warn`). No data migration. Rollback is dropping the three columns; nothing depends on them in an older binary (older code ignores unknown columns because every query lists columns explicitly, **[verify]** that `builder_settings` reads use explicit column lists).

## 3. Session creation (BBE-01 to BBE-04)

Request body:

```json
{ "title": "...", "template_id": "...", "locale": "pt-BR",
  "backend": { "mode": "new" } }
```
or `{ "backend": { "mode": "existing", "app_id": "<uuid>" } }`. The legacy `backend_app_id` field is still accepted and means `existing`. Both given and conflicting is a 400.

Flow for `new` in `CreateSession`, after the access and availability checks and before the template download:

1. Derive the app name: slug of the title (lowercase, `[a-z0-9-]`, 3 to 40 chars, fallback `app`), plus a short random suffix; retry on a name collision. Validated by `validateAppInput` inside `CreateAppForUser`.
2. `CreateAppForUser(ctx, user, AppRequestBody{Name: name, AuthEmailEnabled: true}, ip)`. **[verify]** `AppRequestBody` field names. Audit gets the builder origin by an extra audit entry `builder.backend.create` on the session (the `app.create` row already exists).
3. Continue with the existing code using the new app (`renderOrbitConfig(apiURL, app.Name)`), and set `params.BackendOrigin = builder`.
4. On any failure after step 2 in the same request: `deleteCreatedApp(ctx, app)`; the helper only runs for an app created in this call, logs on failure and never returns the error to the client (BBE-02).

`GetApp` access check for `existing` stays as today. The create response includes `backend_origin` so the UI can label it.

Orphan risk, stated: the compensation is best effort. A crash between steps 2 and 3 leaves an empty app owned by the user, visible in the dashboard, deletable there. Accepted; no sweeper in v1.

## 4. The plan tool (BBE-05 to BBE-10)

`propose_backend_plan`, `Kind: KindProposal`, registered next to the existing tool in `RegisterBuilderTools`.

```json
{ "summary": "string (<= 500 chars)",
  "operations": [ { "kind": "add_table", "add_table": { ... } }, ... ] }
```

Operation objects are exactly the `operation` object of `propose_backend_change`, with two additions. First, `add_table` takes a required `access` (`public`, `owner` or `policy`) and a one-line `access_reason`, which the card shows. Second, a new kind `add_policy`: `{ "table": "...", "template": "...", "params": { ... } }` (shape taken from `policytemplates` **[verify]**).

**Validation (`ParseBuilderPlan`)**, pure function, returns `*tools.ToolError` messages the model can act on, no `err.Error()` echo:
1. `1 <= len(operations) <= 30`; `summary` non-empty.
2. Each operation through the same per-operation parser as the single tool (extracted from `ParseBuilderProposal` into `parseBuilderOperation`).
3. Access declaration: every `add_table` needs `access` and `access_reason` (at most 200 chars). `owner` requires a `set_rls_mode` for that table (`owner` or `enabled`) later in the plan; `policy` requires `set_rls_mode` `policy` and at least one `add_policy` for it; `public` requires nothing but is shown prominently. A plan that leaves a created table undeclared fails with a tool error naming the table. The model chooses from the app's purpose; the validator only checks that a decision was made and is consistent with the operations, never which decision is right.
4. Dependency simulation: start from the current schema (`GetAppSchema` snapshot, the same data `orbit_get_app_schema` returns), then walk the operations in order, keeping a set of tables and columns. `add_table` fails if the table exists; every other operation fails if its table or referenced column is not in the set at that point; `add_policy` and `set_rls_mode` fail if the table is missing. This catches ordering mistakes before the card is shown.
5. The handler also re-checks the effective role like `proposeBackendChange` does.

The handler returns the same "recorded and sent for confirmation" text. No schema change happens in the tool.

**Confirm (BBE-08)** in `ConfirmProposal`, branch on `p.Name`:
- `propose_backend_change`: unchanged.
- `propose_backend_plan`: re-parse with `ParseBuilderPlan` **without** the live-schema simulation being trusted blindly (it runs again against the schema at confirm time, because the schema may have changed while the card was open; a failure there is a 400 with "the schema changed, ask the agent to re-plan" and the turn stays paused so the user can reject).
  Then loop `applyEditOperation` in order. On the first error: stop, build the result below, mark `applied=false` overall, `partial=true` if at least one operation applied.

Tool result text (model-facing):
```
The user confirmed the plan. Applied 3 of 5 operations: add_table notes, add_table tags, add_foreign_key notes.tag_id.
Operation 4 (add_policy on notes) failed: <safe message>. Operations 4 and 5 were NOT applied.
Call orbit_get_app_schema, then propose a plan for the remaining changes only.
```
The safe message uses the existing `builderApplyErrorMessage` (unsafe errors are logged and become a generic 500, as today; in that case the turn stays paused like the single-op path).

Not atomic: stated in the spec (Assumption 5) and in this document. Wrapping the whole plan in one Postgres transaction would require changing each `*ForUser` function to accept a transaction; not worth it for v1.

Event payload for the timeline: `{tool_call_id, kind: "plan", applied_count, total, failed_index?, ops: [{kind, table}]}`. No column defaults, no row data.

## 5. `add_policy` (BBE-11)

Extend `ai.EditOperation` with `AddPolicy *PlanPolicyOp` and `ParseEditOperation` with the kind. **[verify]** that the edit-chat tool definitions (`editToolDefs`) enumerate their own tools, so adding a kind to the struct does not expose it to "Edit with AI". `applyEditOperation` gets a `case "add_policy"` calling `CreateTablePolicyForUser`. The policy comes from a template resolved server-side (`policytemplates`); the model chooses the template name and parameters, never SQL. Ownership-style templates need an owner column and email auth (same check as `UpdateTableRLSModeForUser` does today); a failure is a safe 400-class message returned to the agent.

## 6. Approval mode and the loop (BBE-15)

Setting read per turn (not cached across turns). In `ask` or for `existing` backends: today's pause.

For `auto` + `backend_origin = builder`: the plan handler applies inline, using the same apply routine as confirm, and returns a normal tool result. The loop must not pause for it. Mechanism: add `Resolved bool` to `tools.Result`; the loop's proposal branch (`loop.go`) pauses only when the call kind is `KindProposal` **and** `!res.Resolved`. **[verify]** by reading the branch at `loop.go:453` and the `FinishTurn` path in the implementing task before choosing between this and a second tool name. Audit for an auto apply: `builder.proposal.auto` with the same fields as confirm. The inline path still calls `applyEditOperation`, so RBAC and audit origin are identical.

Only additive operations exist in the tool, so there is no destructive tier to gate; if a destructive kind is ever added it must be excluded from `auto` by construction (a test asserts the set of kinds allowed in auto).

## 7. Publish check (BBE-13, BBE-14)

The RLS modes are `""`, `owner`, `enabled`, `policy` (`internal/config/rls.go`). `""` means no filtering and is also the unset default, so a table in that mode is either public by design or forgotten; the table state alone cannot say which. The decision therefore comes from the plan: a confirmed plan records which tables it declared `public` (event payload `public_tables`, names of tables actually applied). **[verify]** that `system_config.require_rls_default` (global flag in `provisioner.go`) does not already change what `""` means for new tables, and how the event store is queried for a session's confirmed events.

In `Publish` (`builder_publish.go`), after the session and access checks and before pushing: `undeclaredOpenTables(ctx, sessionID, appID)` reads the app's table metadata (name, `rls`) and the session's confirmed plan events, and returns tables with `rls = ""` that no confirmed plan of this session declared public. Tables created by the single-op tool carry no declaration, so they are listed; that is intended (nobody decided).

| `backend_origin` | setting | result |
| --- | --- | --- |
| `builder` | `warn` (default) | publish; response carries `warnings.open_tables: [...]` |
| `builder` | `block` | `409` `backend_open_tables` with `tables: [...]` |
| `existing` | any | publish; warning only |

Metadata and event payloads only. No row reads.

UI: the warning (or the 409 under `block`) is handled inline (same pattern as `builderSkillErrorInfo`): the list of tables with the note "these tables are readable by anyone with access to the app" and a button that prefills the chat message "review the access of these tables" (the user still sends it). A user who knows the table is public can ignore the warning and publish.

## 8. Prompt and skills (BBE-04, BBE-12)

- `core.md`, "Backend" section: when the schema lacks what the request needs, read the schema, then call `propose_backend_plan` once with the full design (tables, relations, policies) before writing code that uses it; one plan, small and ordered; decide the access of every table from the app's purpose and declare it with a reason (public is valid when the data is meant for everyone; never default to public to make a screen work); if the plan fails partway, re-read the schema and plan only the rest. The single-op tool stays documented for one-off changes. Authority order unchanged.
- New built-in `orbit-schema-design` (written from scratch, in the lint/lock): naming, column types, ownership column, FK `on_delete`, indexes for list filters, how to decide public vs owner vs policy from the app's purpose (worked examples of each), which policy template fits which case, "ask the user when it is ambiguous who should see the data". Description at most 200 chars.
- `orbit-api-and-backend` (the adapted `orbit-usage`, task S18 of the skills feature) is independent of this feature; the MCP and SDK chapters stay out.
- Template guide: **[verify]** the default template's `ORBIT_AGENT.md` and the company template path tolerate an empty schema (no hardcoded table names).

## 9. UI

- `NewBuilderSessionDialog`: a choice "New backend (the agent designs it)" as default and "Use an existing backend" with the app picker. i18n en and pt-BR.
- Session header shows the backend origin chip.
- Timeline: a plan card (summary, ordered operations, confirm/reject), then "applied N of M" and the failed operation when partial. Reuse the proposal card components; the plan view lists operations as `kind table` plus the access line, never values.
- Builder settings: two selects (approval mode, publish check) with a plain-language description of each, always sending both keys.
- Plan card: each new table shows its access line and the agent's reason; `public` is visually distinct.
- Publish warning panel as in section 7.

## 10. Security and compliance

- Confirmation by a human stays the default for every schema change. `auto` is off by default and restricted to builder-created backends.
- No new endpoint bypasses RBAC or audit: creation goes through `CreateAppForUser`, application through `applyEditOperation`.
- Plan text and error strings are English; never `err.Error()` to the client on 500 or to the model.
- Prompt credential guard (BBE-16) rejects before persisting and does not echo the value.
- Health data: the builder never reads rows. The plan and the events carry names and kinds, not values.
- LGPD and region: no change versus the parent feature (self-hosted). Any claim about legal fitness stays a recommendation for internal validation.

## 11. Testing strategy

- Unit: `ParseBuilderPlan` (limits, ordering, collisions, unknown kinds, dependency simulation); name derivation; `add_policy` parsing; decision table of the publish check; the auto-eligibility predicate.
- Integration (real Postgres, `builder_*_test.go`): create session `new` creates the app and rolls it back on failure; legacy body still works; plan confirm applies all, stops at the first failure and reports it; two concurrent confirms apply once; schema drift between card and confirm; RBAC loss; publish check for both origins and both settings, and for declared-public versus undeclared tables; credential guard.
- Loop test with fakes: plan pause and resume; auto path does not pause; `existing` + `auto` pauses.
- Manual UAT on the local Orbit with a real model (never Starbem infra), including a partial failure forced by a colliding table name.
- Eval (P3, after the parent T42): from-scratch fixtures, measuring plan validity rate, policy coverage and first-build success per model.
