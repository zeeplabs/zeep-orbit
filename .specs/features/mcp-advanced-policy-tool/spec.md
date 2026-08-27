# orbit_create_policy_advanced MCP Tool Specification

## Problem Statement

`orbit_create_policy_from_template` (`internal/mcpserver/tools.go`, mcp-server spec T12) only creates policies from 6 fixed templates (`owner_only`, `open_read`, `read_only`, `value_match`, `open_read_owner_write`, `blocked_by_default`). Any policy that needs a clause shape outside those templates — e.g. two chained conditions with `AND`/`OR`, or an operator/claim combination no template covers — can only be created through the Dashboard's advanced policy form or a direct `POST /dashboard/api/apps/{id}/tables/{table}/policies` call. There is no MCP tool for it, so an LLM driving Orbit end-to-end (the "Create/Edit with AI" story, `mcp-server` spec) hits a wall for any non-templated policy shape. Reported by an Orbit user as a gap in the MCP tool-calling surface (tracked externally as SETUP-06).

## Goals

- [x] Through a single new MCP tool, `orbit_create_policy_advanced`, an admin's LLM can create a row policy with an arbitrary structured clause set (any valid `column`/`operator`/`value_source`/`value`/`logic` combination the Dashboard's advanced form already supports) — the same policy shape the REST endpoint accepts, not a new one.
- [x] The tool authorizes, validates, and persists through the exact same code path as `POST /dashboard/api/apps/{id}/tables/{table}/policies` (`CreateTablePolicyForUser`) — no parallel validation logic, per `AGENTS.md`'s "MCP is a new transport onto existing handlers" rule and the `mcp-server` spec's tool-granularity assumption.

## Out of Scope

| Feature | Reason |
|---|---|
| Raw/free-form SQL clause field | Already rejected for the whole MCP surface (`mcp-server` spec, Out of Scope: "Raw/free-form SQL tool") — same reasoning: bypasses schema validation and RLS structural safeguards. This tool stays 100% structured, identical constraint set to `orbit_create_policy_from_template` and the REST endpoint. |
| Editing or deleting a policy via MCP (`UpdateTablePolicy`, `DeleteTablePolicy`) | Not requested by the reported gap (which is about *creating* non-templated policies), and deletion is explicitly out of scope for the whole MCP surface per `mcp-server` spec's V1 exclusion of destructive tools. A future `orbit_update_policy_advanced` is a separate spec if needed. |
| Creating more than one policy per call | The REST endpoint this tool mirrors (`CreateTablePolicy`) accepts exactly one `PolicyDef` per call. `orbit_create_policy_from_template`'s multi-policy return (e.g. `open_read_owner_write`) is a template-specific fan-out, not a general multi-policy input shape — this tool doesn't need or replicate that. |
| New validation rules beyond what `provisioner.BuildPolicySQL` already enforces (operator allowlist, `claim`/`literal` value sources, `AND`/`OR` logic, action enum, column-exists check) | The tool is a transport, not a new policy engine. Relaxing or extending validation is a `provisioner` change, out of this spec's scope entirely. |
| Adding a new authorization tier | Reuses the same `role.CanManage()` tier `orbit_list_table_policies` and `orbit_create_policy_from_template` already use — table policies are one access-control surface regardless of how the policy was shaped. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
|---|---|---|---|
| Tool authorization tier | `role.CanManage()`, same as `orbit_create_policy_from_template` / `orbit_list_table_policies` | Table policies are a single access-control surface (`tools.go` comment on `orbit_list_table_policies`); no reason an "advanced" creation path would need a different tier than the templated one. | y — confirmed by user 2026-08-25 |
| Input shape | Tool input mirrors `dashboard.PolicyDef` 1:1: `app_id`, `table_name`, `name`, `action`, `roles []string`, `clauses []{column, operator, value_source, value, logic}` — no fields beyond what `CreateTablePolicyForUser` already accepts | Matches the `mcp-server` spec's tool-granularity rule ("one MCP tool per existing REST operation... mirrors the structured request body the handler already validates") | y — derived directly from `CreateTablePolicyForUser`'s existing signature, not a product decision |
| Validation and error mapping | Tool performs zero validation of its own; it calls `deps.DashH.CreateTablePolicyForUser` directly (same as `orbit_create_policy_from_template` does per built `PolicyDef`) and maps errors through the existing `mapWriteError` helper | Avoids a second validation implementation that could drift from `provisioner.BuildPolicySQL`'s allowlists (operators, `claim`/`literal`, `AND`/`OR`, action enum, column-exists) — single source of truth stays in `provisioner` | y — confirmed by user 2026-08-25 |
| Relationship to `orbit_create_policy_from_template` | Both tools stay registered side by side; `orbit_create_policy_advanced` does not replace or deprecate the template tool — templates remain the fast path for the 6 common shapes, advanced is the escape hatch for anything else | Templates exist specifically to keep the common case a one-field call (`template_id` + minimal params); collapsing them into one tool would regress that ergonomics win for no benefit | y — confirmed by user 2026-08-25 |
| Tool naming | `orbit_create_policy_advanced`, matching the name given in the reported gap and the `orbit_<verb>_<noun>` convention already used across `tools.go` | Consistency with existing tool names (`orbit_create_policy_from_template`, `orbit_create_table`, etc.) | y |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: Admin creates a non-templated policy via MCP ⭐ MVP

**User Story**: As a dashboard admin driving Orbit through an MCP client (or the internal "Create/Edit with AI" chat), I want to create a row policy with a custom clause set that doesn't match any of the 6 fixed templates, so that I don't have to switch to the Dashboard UI or hand-write a REST call mid-conversation.

**Why P1**: This is the entire reported gap (SETUP-06) — without it, any policy shape outside the 6 templates is unreachable from MCP tool-calling, which breaks the "describe an app, get an app fully secured" story the `mcp-server` spec exists for.

**Acceptance Criteria**:

1. WHEN a caller with `role.CanManage()` on the app calls `orbit_create_policy_advanced` with a valid `PolicyDef` (non-empty `name`, `action` in `select`/`insert`/`update`/`delete`, at least one role, at least one clause where the first clause's `logic` is empty and every subsequent clause's `logic` is `AND` or `OR`) THEN the system SHALL create the policy through `CreateTablePolicyForUser` and return the created `TablePolicyRow` (including its generated `id` and `pg_policy_name`).
2. WHEN the policy is created THEN the system SHALL record the action in the existing `audit_log`, same as the REST endpoint and `orbit_create_policy_from_template` already do.
3. IF the caller does not have `role.CanManage()` on the app THEN the system SHALL return a forbidden error and SHALL NOT create any policy row or DDL.
4. IF any field fails `provisioner.BuildPolicySQL`'s existing validation (unknown column, disallowed operator, invalid `value_source`, disallowed claim name, invalid `logic` value or placement, invalid `action`, empty `roles`, or empty `clauses`) THEN the system SHALL return the mapped, fixed-message error (never a raw `err.Error()`, per `AGENTS.md` §4) and SHALL NOT create any policy row or DDL.
5. IF the submitted policy `name` collides with an existing policy on the same table and `action` THEN the system SHALL return the same "already exists" error the REST endpoint returns for that case (`ErrPolicyAlreadyExists`), and SHALL NOT create a duplicate.
6. The system SHALL NOT accept any field that carries a raw SQL fragment or expression string — every field remains one of `column`, `operator`, `value_source`, `value`, `logic` as already defined by `dashboard.PolicyClause`, preserving the `mcp-server` spec's rejection of free-form SQL.

**Independent Test**: Call `orbit_create_policy_advanced` with two chained clauses (e.g. `status = 'active'` AND `owner_id = claim:sub`) on a table with no matching template, confirm the policy is created and visible via `orbit_list_table_policies`; call it again with an unknown column name and confirm it fails with the mapped validation error, no policy created.

---

## Edge Cases

- IF `clauses` is an empty array THEN system SHALL return the "at least one clause is required" validation error.
- IF the first clause's `logic` is non-empty, or any non-first clause's `logic` is neither `AND` nor `OR` THEN system SHALL return the mapped `provisioner` validation error.
- IF `operator` is `IS NULL` or `IS NOT NULL` and the clause also sets `value_source` or `value` THEN system SHALL return the mapped validation error (unary operators reject operands).
- IF `value_source` is `claim` and `value` is not one of the allowed claim names (`role`, `sub`, `email`) THEN system SHALL return the mapped validation error.
- WHEN the table has zero existing policies THEN system SHALL enable native row-level security on it before creating the first policy (same idempotent behavior `CreateTablePolicy` already implements).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
|---|---|---|---|
| MAPT-01 | P1: Admin creates a non-templated policy via MCP | Execute | Verified |
| MAPT-02 | P1: Admin creates a non-templated policy via MCP | Execute | Verified |
| MAPT-03 | P1: Admin creates a non-templated policy via MCP | Execute | Verified |
| MAPT-04 | P1: Admin creates a non-templated policy via MCP | Execute | Verified |
| MAPT-05 | P1: Admin creates a non-templated policy via MCP | Execute | Verified |
| MAPT-06 | P1: Admin creates a non-templated policy via MCP | Execute | Verified |

**ID format:** `MAPT-[NUMBER]` (mcp-advanced-policy-tool)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 6 total, 6 mapped to a single P1 story, 0 unmapped, 6 verified

---

## Success Criteria

- [x] An LLM driving Orbit via MCP can create any policy shape the Dashboard's advanced form supports, without switching to the UI or a raw REST call.
- [x] Zero new validation logic outside `provisioner.BuildPolicySQL` — the tool is a pure transport, verified by code review at Execute.
- [x] Every creation attempt (success or rejected) is traceable in `audit_log`, matching the REST endpoint's existing behavior (inherited from `CreateTablePolicyForUser`, not independently tested by this feature — see spec MAPT-02 note).
