# AI Builder Backend Specification

> Status: **Draft for review (2026-10-28).** Child of `.specs/features/ai-frontend-builder/`. Read its `spec.md` first. This feature **reverses one decision of the parent spec**: the parent fixes one existing backend app per session and lets the agent only propose single changes. Here the builder can also create the backend from scratch, like Lovable does, while keeping human confirmation for every backend change.

## Problem Statement

Today a builder session needs an existing Orbit backend app (`builder_sessions.backend_app_id` is `NOT NULL`, `CreateSession` requires a valid app the user can see). A non-technical user who wants "a CRM for my clinic" must first build the backend with "Build with AI", then open the builder. The agent can change the backend only through `propose_backend_change`: one operation per call, each pausing the turn for a dashboard confirmation. A backend from scratch (5 to 10 tables, relations, policies) would need 15 to 30 confirmations. There is also no operation to create an RLS policy, and no check that the published app is not exposing unprotected tables.

## Reference model (Lovable)

Studied from public docs (2026-10-28, summary by a smaller model, quotes not verified literally; see the dev log for the gaps). What we copy: backend created automatically with the project, plan before build, backend permissions split by tool with a separate destructive tier that is denied by default, a security check at publish. What we do not copy: free SQL as the unit of approval (Orbit uses structured operations, easier to validate and safer), edge functions and secrets (Orbit has none), a publish check that is opt-in (Lovable's block is an admin option).

## Goals

- [ ] A user can start a builder session without choosing a backend and end with a working frontend plus a backend (tables, relations, RLS) that the agent designed, with one confirmation per plan instead of one per operation.
- [ ] A user can still start a session on top of an existing backend (current flow, unchanged).
- [ ] Every backend change is applied through the same `*ForUser` functions, RBAC and audit as the REST/MCP/dashboard paths; the builder gains no new privilege.
- [ ] Every table the agent creates has an explicit, user-confirmed access decision (public, owner or policy); nothing is left public by omission, and publishing warns about any table with row protection off that nobody declared public.
- [ ] A failed or rejected plan never leaves the session in an unknown state: the agent is told exactly what was applied.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Free SQL / raw migrations authored by the agent | Structured operations are validated and mapped to audited `*ForUser` calls. SQL is an injection and review burden. |
| Destructive operations (drop table/column, rename, type change) | No such operation exists in the tool and none is added. Lovable also denies these by default. Revisit after the schema-rollback question is answered. |
| Schema rollback / undo of an applied plan | Not documented by Lovable either. Needs its own design (inverse operations or snapshots). |
| Secrets, edge functions, server-side custom logic | Orbit has no such runtime. Backend logic stays tables, RLS, auth and webhooks. |
| Auth providers that need credentials (Google), storage buckets, webhooks | Need secrets or external config; dashboard only. `toggle_auth` for email stays. |
| Creating the backend lazily, only when the agent needs persistence | The workspace is initialized with the app name and API URL (`orbit.config`) at session creation. Lazy creation changes that init. Revisit as P3. |
| Switching the session's backend after creation | The app is fixed per session, as in the parent spec. |
| Moving the generated backend between Orbit instances | Separate feature. |

---

## Assumptions & Open Questions

| # | Decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | When the backend is created | **At session start**, as the default mode, named from the session title with a unique suffix. "Use an existing backend" stays as the alternative in the create dialog. | Matches Lovable's default and fits the NOT NULL column and the workspace init. | Yes (maintainer, 2026-10-28) |
| 2 | Approval of backend changes | Per-company setting `ask` (default) or `auto`. `auto` applies additive operations without pausing, **only** for sessions whose backend was created by the builder. Existing backends always ask. | Orbit runs in production with health data in some installations. Auto on a production backend is not acceptable by default. | `ask` default: Yes. The restriction of `auto` to builder-created backends is my addition, not yet reviewed. |
| 3 | Tables with no row protection | **The agent decides per table, from the app's context, and declares it** (`access`: `public`, `owner` or `policy`) in every `add_table` of a plan; the plan card shows it in plain words and the user confirms it. The publish check is **warn-only by default** and lists tables with row protection off that no confirmed plan declared public; the company can switch it to `block`. | Some tables are public by design (catalogs, schedules), so state alone cannot tell "intentionally public" from "forgotten": the RLS mode `""` is both the unset default and the public mode. The decision has to be explicit and human-confirmed, not blocked by a rule. The risk behind Lovable's CVE-2025-48757 is the unexamined default, which the declaration removes. | **Yes** (maintainer, 2026-10-28): do not forbid; the agent decides from the app context. The explicit declaration and warn-by-default are my design for it, to be reviewed. |
| 4 | Plan size | At most 30 operations per plan. | Bounds the card, the apply time and a runaway model. | No |
| 5 | Atomicity of a plan | **Not atomic.** Operations apply in order and stop at the first failure; the agent gets the applied/failed/untouched lists. | Each operation goes through its own `*ForUser` function and provisioner call (verified in `applyEditOperation`); there is no single transaction. | Fact, no decision needed |
| 6 | `propose_backend_change` (single op) | Kept, unchanged, so old session histories resume. The plan tool becomes the one the prompt recommends. | Resume-safe history (tool calls persisted in messages). | No |
| 7 | Who can create a backend through the builder | Any user who passes the builder access check, same as `CreateApp` (`RequireAuth` only). | Parity with the dashboard. A stricter rule is a separate decision. | No |

---

## User Stories

### P1: Start from scratch ⭐ MVP

**US-1**: As a builder user, I want to start a session without having a backend so that I can describe an app and get both frontend and backend.

**Acceptance Criteria**

1. BBE-01 WHEN the create-session request has `backend.mode = "new"` (the default when `backend` is absent in the UI) THEN the system SHALL create an Orbit app owned by the caller through the same operation as `CreateApp` (`CreateAppForUser`, email auth enabled, audited as `app.create` with the builder origin) and bind the session to it.
2. BBE-02 WHEN the session insert or the workspace init fails after the app was created in the same request THEN the system SHALL remove the empty app it just created; IF that removal fails THEN it SHALL log the error server-side and leave the empty app (harmless), never touching an app it did not create in this request.
3. BBE-03 The session SHALL record `backend_origin` (`builder` or `existing`); the UI SHALL show which one it is, and the existing-backend flow SHALL behave exactly as today (`backend.mode = "existing"` with `app_id`; the legacy body with only `backend_app_id` keeps working).
4. BBE-04 WHEN the session starts on an empty backend THEN the generated project SHALL still build (empty schema is a valid state) and the agent SHALL propose the schema before writing code that depends on it.

### P1: Plan before build ⭐ MVP

**US-2**: As a user, I want to approve the whole backend design once so that I do not confirm 20 small steps.

**Acceptance Criteria**

1. BBE-05 The agent SHALL have a tool `propose_backend_plan` taking an ordered list of operations (same kinds as `propose_backend_change` plus `add_policy`) and a short plan summary. It does not apply anything.
2. BBE-06 WHEN the plan is submitted THEN the system SHALL validate it before showing it: every operation parses with the same validator as the single-op tool; at most 30 operations; names and types valid; every `add_table` carries `access` (`public`, `owner` or `policy`) and, for `owner` or `policy`, the plan contains the matching RLS mode and, for `policy`, an `add_policy` for that table; every table, column and reference an operation uses exists in the current schema or is created by an earlier operation of the same plan. A failure SHALL return an actionable tool error to the agent and show nothing to the user.
3. BBE-07 WHEN the plan is valid and approval mode is `ask` (or the session is not builder-created) THEN the turn SHALL pause and the dashboard SHALL show one card with the summary and every operation in order, and for each new table a plain-language access line (for example "Public: anyone with access to this app can read every row" versus "Private: each user sees only their own rows"); the user confirms or rejects the whole plan.
4. BBE-08 WHEN the user confirms THEN the system SHALL re-check write access, re-parse the stored arguments, and apply the operations in order through `applyEditOperation`, stopping at the first failure. The tool result to the agent SHALL state which operations were applied, which one failed (with a safe message) and which were not touched. The agent SHALL be told to call `orbit_get_app_schema` before continuing.
5. BBE-09 WHEN the user rejects THEN nothing is applied and the agent is told to adapt the frontend to the existing schema or ask.
6. BBE-10 Confirm and reject SHALL be audited (`builder.proposal.confirm|reject`) with the operation kinds and counts, never row data.

### P1: Policies

**US-3**: As a user, I want the agent to protect the tables it creates.

**Acceptance Criteria**

1. BBE-11 The operation `add_policy` SHALL create a row-level policy on a table from the existing policy templates (`internal/policytemplates`) through `CreateTablePolicyForUser`, with the same validation and RBAC as the dashboard and MCP paths. Free-form advanced policies are out of scope.
2. BBE-12 The prompt and the built-in skill `orbit-schema-design` SHALL tell the agent to decide the access of every table it creates from what the app is for (who should see which rows), to declare it in the plan with a one-line reason, and never to default to public to make something work. Public is a valid, expected choice when the data is meant to be seen by everyone (catalog, public schedule, reference data).

### P1: Publish check

**US-4**: As the company, I want to be told when an app is about to go out with a table nobody consciously made public.

**Acceptance Criteria**

1. BBE-13 WHEN a session is published AND the backend has a table with row protection off (RLS mode `""`) that is not declared public by a confirmed plan of this session THEN the system SHALL publish and return the table names as a warning, IF the company setting is `warn` (default); IF it is `block` AND `backend_origin = builder`, it SHALL refuse with `409` and list the names. For `existing` backends it SHALL only warn. A table declared `public` in a confirmed plan is never listed.
2. BBE-14 The warning or refusal SHALL offer a one-click path back to the chat ("review the access of these tables"). The check SHALL NOT read row data.

### P2: Approval mode and credential guard

1. BBE-15 The superadmin SHALL be able to set backend approval to `ask` or `auto` and the publish check to `warn` (default) or `block` in the builder settings (partial-update semantics: the UI always sends both keys). `auto` SHALL apply to additive operations only, only for builder-created backends, and the timeline SHALL show what was applied without a confirmation.
2. BBE-16 WHEN a user prompt contains a credential (same detector as skills) THEN the system SHALL reject the message with `400 credentials_detected` without echoing the value and without persisting it, and the UI SHALL tell the user not to paste secrets. Prompts reach the LLM provider and are stored.

### P3

1. BBE-17 Eval fixtures for from-scratch tasks (plan quality, every table has a declared access consistent with the app, no undeclared open tables) once the parent T42 runner exists.
2. BBE-18 Lazy backend creation; schema rollback; destructive operations; credential-bearing providers.

---

## Edge Cases

- WHEN `add_table` in a plan collides with an existing table THEN validation fails before the card, with the table named in the tool error.
- WHEN the user's write access is lost between the card and the confirm THEN the confirm returns 403/404 as today and the turn stays paused.
- WHEN the process dies in the middle of applying a plan THEN the turn stays paused (the proposal resolves only after the apply returns). On retry the agent reads the real schema; validation rejects already-applied `add_table` operations, so the agent re-plans the remainder. A partial apply is a known limit, not hidden.
- WHEN two confirmations race THEN the existing `ErrProposalNotPending` / `ErrProposalBusy` handling decides; the plan is applied at most once.
- WHEN the session archives or is deleted THEN the builder-created app stays (it may hold data). The UI states it; deleting the app stays a dashboard action.
- WHEN the model proposes a plan on an `existing` backend with `auto` configured THEN it still asks (BBE-15).

## Requirement Traceability

| ID | Story | Status |
| --- | --- | --- |
| BBE-01 to BBE-04 | US-1 | Pending |
| BBE-05 to BBE-10 | US-2 | Pending |
| BBE-11, BBE-12 | US-3 | Pending |
| BBE-13, BBE-14 | US-4 | Pending |
| BBE-15, BBE-16 | P2 | Pending |
| BBE-17, BBE-18 | P3 | Deferred |

## Success Criteria

- A new session on an empty backend, with a prompt like "a booking system for a clinic", ends with tables, relations and policies created from one confirmed plan, and a frontend that builds and reads them. Measured on the local Orbit with a real model (ZeepLabs-controlled infra only).
- No tool or path the builder uses writes the schema without a user confirmation, except `auto` on a builder-created backend, and that is off by default.
- No new route or function bypasses `*ForUser` RBAC and audit.
