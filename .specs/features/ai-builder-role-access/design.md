# AI Builder Role Access Design

**Spec**: `.specs/features/ai-builder-role-access/spec.md`
**Status**: Draft (2026-11-16). Nothing implemented. Items marked **[verify]** were not checked against the code or the running stack and must be before the task that depends on them.

---

## Architecture Overview

No new service and no schema change. The feature adds one policy template that three existing code paths already know how to consume, one plan operation, one validation step and the text and cards around them.

```
agent                      builder (dashboard pkg)                      platform
  │ propose_backend_plan      │                                           │
  ├──────────────────────────►│ ParseBuilderPlan  (+ add_role op)         │
  │                           │ Validate          (+ role rules, RAC-04/09/11)
  │                           │ ── user confirms ──                       │
  │                           │ applyBuilderPlan                          │
  │                           │   add_role   ──► roles *ForUser ─────────►│ apps.enduser_roles_config
  │                           │   add_policy ──► policytemplates.Resolve ─►│ CreateTablePolicyForUser ► BuildPolicySQL
```

Reused unchanged: `provisioner.BuildPolicySQL` (clauses, actions, roles), `CreateTablePolicyForUser` (RBAC, audit), the plan loop (order, partial outcome, audit origin `ai_builder`), the publish check.

## Components

### C1 — Template `role_access` (RAC-01 to RAC-03)

Go, `internal/policytemplates`:

- `TemplateIDRoleAccess = "role_access"`, added to `List()` with `RequiresOwnerColumn: true` (the clause uses `owner_id`), `Kind: "composite"`, no `ActionsFixed` (the caller chooses; default all four).
- `BuildRoleAccessPolicies(actions, roles)`: one `PolicyDef` per action, `Name: GeneratedPolicyName(TemplateIDRoleAccess, action)`, `Roles: roles`, `Clauses: []PolicyClause{openReadClause}`.
- `Resolve` gets a `role_access` case: `roles` required (`MissingInputError{"roles"}`), `actions` defaulted, each action checked against the four; an unknown or duplicate action is a `MissingInputError{"actions"}`.
- Role format is checked in the builder (`identRe`), as for the other templates; the platform checks it again at creation.

TypeScript, `internal/dashboard/ui/src/lib/policyTemplates.ts`: the same id in `TemplateId`, an entry in `TEMPLATE_DEFINITIONS`, a `buildRoleAccessPolicies` mirror, and the template picker entry with its i18n strings (`en`, `pt-BR`).

MCP, `internal/mcpserver/tools.go`: description map (`...TemplateIDRoleAccess: "Any of the given roles can read, add, change and delete every row."`), required-inputs map (`{"roles"}`, with `actions` optional), and the template switch of `orbit_create_policy_from_template`. The MCP tool still carries its own copy of the switch (`resolve.go` says so); the cheapest honest fix is to make it call `policytemplates.Resolve` and delete the copy. Treat that as part of the task if the diff stays small, otherwise add the case and note the debt.

**Parity (RAC-03).** The TS file has no test runner (`package.json` has no `test` script). Options, in order of preference:
1. A Go test that reads `policyTemplates.ts` and asserts the template ids, `requiresOwnerColumn` and `actionsFixed` match `List()`. It catches an id or a flag drift, not a clause drift.
2. A golden fixture `internal/policytemplates/testdata/role_access.json` read by the Go tests, plus a manual UAT line for the dashboard picker.
3. Adding vitest. Rejected for now: a new dependency (AGENTS §9) for one file.

I recommend 1 plus 2. **Not confirmed.**

### C2 — Operation `add_role` (RAC-07 to RAC-10)

- `internal/dashboard/ai/client.go`: `EditOperation.Kind` gains `add_role`; a new `PlanRoleOp{Role string}` field; the edit chat keeps rejecting it (the way it rejects `add_policy` today: **[verify]** the exact guard in `ai_edit*.go`).
- `builder_tools.go` / `builder_plan.go`: the operation schema (`builderOperationKinds`, the `kind` enum, the `add_role` object), `ParseBuilderPlan`, `Validate`, `applyBuilderPlan` (switch on `op.Kind`), the table-name helper (an `add_role` has no table).
- **Apply path.** `UpdateAppEnduserRoles` is an HTTP handler (`handler.go:3408`) and there is no `...ForUser` function. Extract `UpdateAppEnduserRolesForUser(ctx, pool, appID, user, roles)` from the handler, keep the handler as a thin wrapper (same pattern as the other `*ForUser` functions, same audit), and call it from the builder with `list ∪ {role}`. Concurrency: two confirmations adding different roles read-modify-write the list; lock the app row (`SELECT ... FOR UPDATE`) inside the function. **[verify]** how the current handler guards a concurrent PUT.
- Idempotent: adding a role already in the list is a no-op that reports applied (not a `409 role already exists`).

### C3 — Validation (RAC-04, RAC-09, RAC-11)

`BuilderPlan.Validate(tables, authEmail, requireRLSDefault)` becomes `Validate(tables, roles, authEmail, requireRLSDefault)`, where `roles` is the app's current role list (`apps.enduser_roles_config`, always includes `member`).

- Track roles: `known := roles ∪ {add_role ops so far}`; a `role_access` whose role is not in `known` is refused (RAC-09).
- Track `(table, action)` pairs per `role_access` op; a repeated pair is refused (RAC-04).
- `role_access` counts as an `add_policy` for the existing rule "declared `policy` needs at least one `add_policy`" (`c.policies++` already runs for every `add_policy`).
- RAC-11 needs the brief, which `Validate` does not see. Add it as a second, separate check in `proposeBackendPlan`: load the current brief (`SELECT plan FROM zeep_system.builder_briefs WHERE session_id=$1 ORDER BY version DESC LIMIT 1`, through the store if it exposes it to the dashboard package; **[verify]**), decode `data[].{table,access}` with the planning package's own decoder, and refuse `public` for a table declared `owner` or `role`. Sessions with no brief (planning off, or the legacy path) skip it. The brief's table names are the agent's words, so the comparison is by exact name and a missing match is not an error.

### C4 — Cards and text (RAC-12, RAC-13, RAC-16, RAC-17)

- `builderModel.ts` (`summarizePlan` and the operation summaries) renders `role_access` and `add_role` as plain sentences, never values. `ProposalCard.tsx` shows the line about the administrator once per plan (RAC-17).
- `en.json`, `pt-BR.json`: new keys under `builder.page.proposal.op.*` and `builder.page.plan.*`; validate both with `python3 -c "import json; ..."`.
- `internal/agent/prompts/core.md`: replace the interim sentence ("...you cannot create that policy yet...") with the instruction to use `access: policy` + `add_role` + `role_access` for brief entries marked `role`, and keep the sign-up rules. The tool description of `propose_backend_plan` and the `add_policy` schema line list the new template.
- `internal/agent/skills/builtin/orbit-crud-screens/` (or a new skill): the sign-up and "waiting for approval" pattern (RAC-14, RAC-15). Built-in skill changes go through the lint test (`lint_test.go`) and `builtin.lock`.

### C5 — Docs (RAC-18, RAC-19)

`docs/docs/ai-frontend-builder.md`, `CHANGELOG.md`, and the translated READMEs only if a feature table changes.

## Data Models

No DDL. `apps.enduser_roles_config` (JSON array, default `["member"]`) and `table_policies` already exist. The new plan operation is JSON inside the existing proposal payload:

```json
{"kind": "add_role", "add_role": {"role": "staff"}}
{"kind": "add_policy", "add_policy": {"table": "pacientes", "template": "role_access", "roles": ["staff"], "actions": ["select","insert","update","delete"]}}
```

`PlanPolicyOp` already has `Roles` and `Actions`; the template adds no field.

## Error Handling

All messages are English, model-actionable, never echo Go errors (AGENTS §4). Examples: `role "staff" is not in this app's role list; add an add_role operation for it before using it in role_access`; `table "pacientes": role_access actions overlap for "select"; merge the roles into one operation`; `table "pacientes" is declared role in the approved plan but public here; use access policy with role_access`.

## Security Notes

- The feature closes the 2026-11-16 exposure only if an end user cannot give themselves a role. `PATCH /auth/me` takes `name`, `phone`, `avatar_url` only (assumption 7) **[verify with a test]**.
- `role_access` gives write on every row to the role. It is as strong as the administrator's role assignment: the dashboard's app-users screen is the only way to set a role, and it needs the admin of the app.
- `member` is the default role of self-registered accounts. A `role_access` on `member` is the old hole with a different name (spec edge case): the card warns, the prompt forbids it for data about people. A hard refusal in the validator for a table whose brief entry holds personal data is not possible, because the validator cannot tell what is personal data.
- The operator setting `system_config.require_rls_default` (tables restricted by default, `public` refused with email auth on) stays the strongest guard for installations with health data. Not changed here.
- No data of a row is read by any step of this feature; the checks use metadata only.

## Tech Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Template, not advanced policy, in the builder | `role_access` template | The structured, named template is what the user confirms and what the audit shows; free clauses written by the model are a larger review burden (parent spec, out of scope). |
| Explicit `add_role` | New operation | Visible on the card. The side-effect alternative is cheaper to implement and hides a change to the role list (spec assumption 2, **not confirmed**). |
| One policy per action | Four `CreatePolicy` calls | `BuildPolicySQL` creates one policy per action; same shape as `owner_only`. A failure midway leaves the earlier policies, like `owner_only` today (`policyApplyError`). |
| Refuse brief/plan divergence | Separate check from `Validate` | `Validate` stays a pure function of the plan and the app; the brief lookup needs the pool. |

## Risks and Concerns

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Template drift across Go, TS and MCP | A dashboard user and the builder get different policies for the same template | Parity test (C1), and calling `Resolve` from the MCP tool instead of keeping a copy. |
| The role in the list is not enough to assign it | Admin cannot select `staff` in the app-users screen | `add_role` and the card line (RAC-17); UAT on the local stack. |
| Staff sign-up is invisible in the generated app | Employees cannot create an account, so the role is never assigned | US-4; without it the feature is incomplete. **[verify]** the SDK exposes the role. |
| Demotion window of one hour | A removed employee keeps access up to `TokenTTL` | Documented (RAC-18). A shorter TTL or a revocation list is a separate feature. |
| Partial plan after `add_role` | A role in the list with no policy, or a table with mode `policy` and no policy (closed by default) | Closed by default is safe; the outcome lists it; the agent is told what applied. |
| Agent still picks `public` | The same exposure | RAC-11 (cross-check), the prompt, the cards, and the check in RAC-20 with a real model. The prompt is a mitigation, not a guarantee: only the validator and the platform setting enforce. |
