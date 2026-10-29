# AI Builder Backend Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. If the skill cannot be activated, stop and tell the user.

Commits only when the user asks (AGENTS.md §2); one commit per task when authorized; never add Claude/Anthropic attribution; never force push. **B01 adds DDL to `provisioner.go` (higher-risk, AGENTS.md §8): show the DDL diff and get confirmation before applying.** Tests and measurements run locally or on ZeepLabs-controlled infra, never on Starbem infra.

---

**Spec**: `.specs/features/ai-builder-backend/spec.md`
**Design**: `.specs/features/ai-builder-backend/design.md`
**Parent**: `.specs/features/ai-frontend-builder/` (B14 depends on T42)
**Status**: Draft, awaiting maintainer approval (2026-10-28). Nothing implemented.

---

## Test Coverage Matrix

| Code Layer | Required Test Type | Location Pattern | Run Command | Parallel-Safe |
| --- | --- | --- | --- | --- |
| `internal/agent` loop/prompt with fakes, `internal/agent/tools`, `internal/agent/skills` | unit | `*_test.go` next to code | `go test ./internal/agent/...` | Yes |
| Plan validator, name derivation, publish decision table | unit | `internal/dashboard/builder_*_test.go` (pure functions) | `go test ./internal/dashboard -run <Name>` | Yes |
| Dashboard stores and handlers (real Postgres) | integration | `internal/dashboard/builder_*_test.go` | `go test -p 1 -parallel 4 ./...` (use `-timeout 25m`) | No (shared DB) |
| DDL in `provisioner.go` | none | build gate (exercised by B01/B02 tests) | build gate | — |
| Frontend | none (no framework) | `src/**` | frontend gate | — |
| Eval | eval (opt-in, real LLM) | `internal/agent/eval` | `go test -tags eval ./internal/agent/eval/...` | — |

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 25m ./...` (env from the scratchpad `env.sh`, `DASHBOARD_BOOTSTRAP_SECRET=ci-test-bootstrap-secret-not-for-prod`; never print the DSN; do not run concurrently with `npm run build`) |
| Frontend | UI tasks | `cd internal/dashboard/ui && rtk proxy node_modules/.bin/tsc -b && npm run build` plus i18n JSON validation for `en.json` and `pt-BR.json` |

---

## Dependency Graph

```
B01 DDL + store ──► B02 session create (new/existing) ──► B03 dialog UI
        │
        ├──► B04 add_policy op ──► B05 plan validator + tool ──► B06 confirm/apply plan ──► B08 plan card UI
        │                                                              │
        │                                                              └──► B07 approval mode (setting + loop auto)
        ├──► B09 publish gate ──► B10 gate UI + settings UI
        └──► B11 prompt + orbit-schema-design skill (after B05)
B12 credential guard (independent, P2)        B13 docs + CHANGELOG (last)        B14 eval + UAT (needs T42)
```

---

## Tasks

### B01 — DDL and store fields  `[higher-risk: confirm DDL first]`
- **What**: add `builder_sessions.backend_origin`, `builder_settings.backend_approval_mode`, `builder_settings.publish_rls_gate` (design §2); expose them in the session store/params/view and the settings store/input/view with partial-update semantics (absent key keeps the value).
- **Where**: `internal/dashboard/provisioner.go`, `internal/agent/store` (session params and struct), `internal/dashboard/builder_settings_store.go`, `builder_common.go` (views).
- **Done when**: DDL diff shown and confirmed; idempotent re-run; old rows read as `existing`/`ask`/`warn`; **[verify]** `builder_settings` reads use explicit column lists; settings round-trip test; validation rejects other values.
- **Requirements**: BBE-03, BBE-15. **Tests**: integration. **Gate**: Full.

### B02 — Create session with `new` or `existing` backend
- **What**: `CreateSession` accepts `backend: {mode, app_id?}`; legacy `backend_app_id` kept; `new` derives the name, calls `CreateAppForUser`, sets `backend_origin = builder`, compensates on later failure (design §3). Verify `DeleteApp` behavior first; if the store function does not drop the schema and unregister, use the handler path.
- **Where**: `internal/dashboard/builder_handlers.go`, new helper file `builder_backend_create.go`.
- **Done when**: tests for: `new` creates app + session; failure after app creation removes the app; compensation failure is logged and not surfaced; legacy body works; conflicting body is 400; a user without access to `existing` app gets 404; name collision retried; audit row `builder.backend.create`.
- **Requirements**: BBE-01, BBE-02, BBE-03. **Depends**: B01. **Tests**: integration + unit (name derivation). **Gate**: Full.

### B03 — Create dialog: new backend by default
- **What**: `NewBuilderSessionDialog` offers "New backend" (default) and "Existing backend"; `api.ts` types and hook send `backend`; header chip shows the origin; i18n en and pt-BR.
- **Where**: `internal/dashboard/ui/src/components/patterns/NewBuilderSessionDialog.tsx`, `src/lib/api.ts`, locales.
- **Done when**: both modes create sessions in the local Orbit; the existing-backend flow is unchanged; mutation has `onError`; i18n validated.
- **Requirements**: BBE-01, BBE-03. **Depends**: B02. **Gate**: Frontend.

### B04 — `add_policy` operation
- **What**: extend `ai.EditOperation` and `ParseEditOperation` with `add_policy`; `applyEditOperation` case calling `CreateTablePolicyForUser` from a `policytemplates` template. **[verify]** edit-chat tool definitions do not expose the new kind to "Edit with AI".
- **Where**: `internal/dashboard/ai/client.go`, `internal/dashboard/ai_edit_chat_handlers.go`, `internal/policytemplates`.
- **Done when**: unit tests for parsing; integration test applies a template policy with RBAC and audit; unknown template and missing owner column give safe errors; edit chat still rejects the kind.
- **Requirements**: BBE-11. **Depends**: B01. **Tests**: unit + integration. **Gate**: Full.

### B05 — `propose_backend_plan` tool and validator
- **What**: extract `parseBuilderOperation`; add `ParseBuilderPlan` with the limits, the access-declaration rules (`access` and `access_reason` on every `add_table`, consistency with `set_rls_mode`/`add_policy`) and the dependency simulation against the current schema (design §4); register the tool with its JSON schema; same role check as the single tool.
- **Where**: `internal/dashboard/builder_tools.go`, new `builder_plan.go`, `internal/agent/skills/builtin.go` (`KnownToolName` list) and the real-registry test.
- **Done when**: unit tests for each rule (empty, over 30, bad kind, collision, missing table, FK to a table created later in the plan, policy on a missing table, undeclared access, `owner` without RLS mode, `policy` without `add_policy`, `public` accepted, ordering ok); tool errors are model-actionable and never echo Go errors; `TestBuiltinLint` and the registry-known-tools test pass with the new name; `propose_backend_change` behavior unchanged.
- **Requirements**: BBE-05, BBE-06. **Depends**: B04. **Tests**: unit. **Gate**: Quick then Full.

### B06 — Confirm and reject a plan
- **What**: `ConfirmProposal` branches on `p.Name`; sequential apply with stop at first failure; model-facing result with applied/failed/untouched; event payload; audit with kinds and counts; revalidation against the schema at confirm time; reject path.
- **Where**: `internal/dashboard/builder_proposals.go`, `builder_plan.go`.
- **Done when**: integration tests: all applied; failure in the middle reports exactly the split; schema drift gives 400 with the turn still paused; RBAC loss; concurrent double confirm applies once; reject applies nothing; audit has no row data; `propose_backend_change` tests still green.
- **Requirements**: BBE-07, BBE-08, BBE-09, BBE-10. **Depends**: B05. **Tests**: integration. **Gate**: Full.

### B07 — Approval mode `auto` (P2)
- **What**: setting wired into the plan handler; `Result.Resolved` (or the alternative chosen after reading `loop.go:453`) so the loop does not pause; inline apply through the same routine; only for `backend_origin = builder`; audit `builder.proposal.auto`; a test asserts the kinds allowed in auto are additive only.
- **Where**: `internal/agent/tools/registry.go`, `internal/agent/loop.go`, `internal/dashboard/builder_plan.go`, `builder_settings*.go`.
- **Done when**: loop test with fakes (auto does not pause; ask pauses; existing + auto pauses); integration test for inline apply and its audit; settings API validates the value.
- **Requirements**: BBE-15. **Depends**: B06. **Tests**: unit (loop) + integration. **Gate**: Full.

### B08 — Plan card in the timeline
- **What**: pending-proposal view for the plan (summary, ordered operations, the access line and reason of each new table with `public` visually distinct, confirm/reject), then the result line "applied N of M" and the failed operation; reuse the existing proposal components; i18n.
- **Where**: `internal/dashboard/ui/src/components/patterns/builder/*`, `src/lib/api.ts`, locales.
- **Done when**: local UAT shows the card, confirm and reject, partial failure; operations listed as kind and table plus the access line, never values.
- **Requirements**: BBE-07, BBE-08. **Depends**: B06. **Gate**: Frontend.

### B09 — Publish check on undeclared open tables
- **What**: record `public_tables` in the confirmed-plan event payload (B06 emits it; add here if B06 is already done); `undeclaredOpenTables` (table metadata plus confirmed-plan events, no row reads) and the decision table of design §7 in `Publish`; `409 backend_open_tables` only under `block` for builder backends; `warnings.open_tables` otherwise. First **[verify]** `system_config.require_rls_default` and how session events are queried.
- **Where**: `internal/dashboard/builder_publish.go`, new `builder_publish_gate.go`.
- **Done when**: unit tests for the decision table; integration tests for builder + warn (default), builder + block, existing; a table declared public by a confirmed plan is not listed; a table from the single-op tool is listed; a partially applied plan only declares tables that were applied; no row read (asserted by query inspection or by using only metadata functions).
- **Requirements**: BBE-13, BBE-14. **Depends**: B01. **Tests**: unit + integration. **Gate**: Full.

### B10 — Publish error panel and settings UI
- **What**: inline handling of the warning and of the 409 under `block` (table list, "review the access of these tables" prefilled prompt, publish still possible under `warn`); builder settings selects for approval mode and publish check, always sending both keys; i18n.
- **Where**: `src/pages/settings/AIBuilderSettings.tsx` (or a new section file following the skills split), publish UI component, `src/lib/api.ts`, locales.
- **Done when**: local UAT of block, warn, and both settings round-trips; clearing is not applicable (enums), keys always sent.
- **Requirements**: BBE-14, BBE-15. **Depends**: B09, B07. **Gate**: Frontend.

### B11 — Prompt rules and `orbit-schema-design`
- **What**: `core.md` Backend section (design §8: decide access from the app's purpose, declare it with a reason, never default to public); new built-in skill written from scratch, with worked examples of public, owner and policy tables, description at most 200 chars, in the lint and `builtin.lock` (`-update-lock`); **[verify]** template guides tolerate an empty schema and fix them if not (template version bump is the maintainer's decision, flag it).
- **Where**: `internal/agent/prompts/core.md`, `internal/agent/skills/builtin/orbit-schema-design/SKILL.md`, lock, template guide if needed.
- **Done when**: `go test ./internal/agent/...` green; lint passes; the skill is listed locked and loadable (store test `ShippedBuiltinsAreListedLockedAndLoadable` updated); read-through by the maintainer before the commit is final.
- **Requirements**: BBE-04, BBE-12. **Depends**: B05. **Gate**: Quick then Full.

### B12 — Credential guard on prompts (P2)
- **What**: reject a prompt containing a credential with `400 credentials_detected`, not persisted, not echoed; UI message.
- **Where**: `internal/dashboard/builder_messages.go`, locales.
- **Done when**: test with runtime-built fake secrets; nothing stored; audit has no value.
- **Requirements**: BBE-16. **Tests**: integration. **Gate**: Full.

### B13 — Docs and CHANGELOG
- **What**: `docs/docs/ai-frontend-builder.md` (new backend mode, plan approval, publish check, limits: not atomic, no destructive ops); `CHANGELOG.md` `[Unreleased]`; amend the parent spec wording where it says the backend is fixed and existing (`ai-frontend-builder/spec.md`, tasks); update this tasks file status.
- **Requirements**: all. **Depends**: B01 to B12. **Gate**: Frontend (if docs build) plus i18n JSON check.

### B14 — Eval fixtures and UAT  `[blocked on parent T42 for the eval part]`
- **What**: from-scratch eval fixtures (plan validity, policy coverage, first-build success per model); manual UAT on the local Orbit with a real model: "booking system for a clinic" from an empty session, forced partial failure, publish block and warn, `auto` on a builder backend and `ask` on an existing one. Record results in the design notes.
- **Requirements**: BBE-17, Success Criteria. **Depends**: B13 (UAT), T42 (eval). **Tests**: eval + manual.

---

## Risks to watch

| Risk | Mitigation |
| --- | --- |
| Partial apply leaves a half-built schema | Stop at first failure, exact report to the agent, validation at confirm time, honest docs. Atomicity deferred. |
| Model proposes huge or incoherent plans | 30-op cap, dependency simulation, tool errors that name the offending operation. |
| Orphan empty apps from failed session creation | Compensation plus a dashboard-visible, deletable app; no sweeper in v1. |
| `auto` misused on production data | Off by default; builder-created backends only; additive kinds only; asserted by a test. |
| The agent picks the wrong access for a table (public when it should be private, or the reverse) | Explicit declaration with a reason on the card, the user confirms it, the publish warning lists undeclared open tables, skill examples, eval measures it (B14). The system cannot know the right answer; it makes the decision visible. |
| Prompt asks for a policy template that does not fit | Skill guidance, safe template errors, agent re-plans. |
| Scope reversal of the parent spec confuses later work | B13 amends the parent spec in the same change. |
