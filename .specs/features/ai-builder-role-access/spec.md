# AI Builder Role Access Specification

> Status: **Draft for review (2026-11-16).** Child of `.specs/features/ai-builder-backend/`. Read its `spec.md` first. Nothing here is implemented; no code was written for this spec.

## Problem Statement

A real builder session for a clinic CRM (2026-11-16, session `e6bd2b46`, local stack) asked for "access only for the logged-in team". The agent declared the `pacientes` table `access: "public"` (no row protection) and told the user "only staff can enter". Both were wrong in practice: email sign-up on an Orbit app is open (`POST /{app}/auth/register` has no guard), so any stranger could create an account and then read and change every patient. The test proved it: an account created with no credentials got HTTP 200 on `GET /pacientes`.

The agent had no better option. The builder's `add_policy` operation accepts only the six policy templates, and none of them says "accounts with role X can read and write every row": `owner_only` shows each user only their own rows, `open_read` and `read_only` grant select only. Orbit itself can express it, though. The same day, on the same app, four policies created through the dashboard API (`select`, `insert`, `update`, `delete`, each with `roles: ["staff"]` and the always-true clause `owner_id IS NOT NULL`, table in `policy` mode) gave this result:

| Caller | Result |
| --- | --- |
| Account created by self-registration (role `member`) | `GET` empty list, `POST` 403 `row-level security policy denied this write`, `PATCH` and `DELETE` on a staff-created row 404 |
| The same account after an admin set its role to `staff` | `GET`, `POST` work, the row appears |
| No token | 401 |

So the capability exists in the platform (policies bound to business roles, `PUT /apps/{id}/roles`, role assignment in the app-users screen, `orbit_create_policy_advanced` in the MCP server). What is missing is the **builder's access to it**. An interim fix (commits `2377306`, `ad5cabc` on `fix/builder-open-signup-wording`) stops the agent from claiming sign-up is closed and makes every card say that `public` means anyone who signs up; it does not change what the builder can enforce. This feature does.

## Goals

- [ ] A builder session whose plan says "only the team / only role X" ends with a backend that enforces it: no table with data about people left reachable by every account.
- [ ] One new policy template, `role_access`, available on the three surfaces that already share the template set (dashboard UI, MCP `orbit_create_policy_from_template`, builder `add_policy`), with identical output on all three.
- [ ] The role the policy names is registered in the app's role list in the same confirmed plan, so the administrator can assign it from the app-users screen.
- [ ] The user sees, before confirming, which role gets access to which table and that an administrator must assign that role.
- [ ] The agent no longer relies on prose to describe access: the access of every table in the brief and in the backend plan agree, or the plan is refused.

## Out of Scope

| Feature | Reason |
| --- | --- |
| A per-app switch that closes self-registration | A platform feature with its own migration, API and UI. Not needed once access is role-based: a stranger can still register but sees nothing. Revisit if the unconfirmed-account clutter becomes a problem. |
| An admin creating end-user accounts from the dashboard | Listed as a future feature in `enduser-roles-config`. The workaround here is that staff register themselves and wait for the role (US-4). |
| Different permissions per role on the same action (reception reads, doctors write) | The policy name is `tpl_role_access_<action>`, unique per table and action. Different roles may take disjoint actions (US-1 AC 5). Overlapping actions with different roles are refused with a "merge the roles" message. |
| Row filters inside the role (a doctor sees only their patients) | That is `value_match` or an advanced policy. Separate feature. |
| Removing or renaming a role from the builder | Additive only. Removal is blocked by the platform while a user or policy uses the role. |
| Changing tables that already exist beyond what `set_rls_mode` and `add_policy` already allow | No new rule; existing tables use the same operations. |
| Closing the demotion window | An end-user token carries the role for `auth.TokenTTL` (1 hour). A demoted user keeps access until the token expires. Documented, not changed. |
| Free-form advanced policies written by the agent | The structured template stays the unit of approval, like the rest of the builder. |

---

## Assumptions & Open Questions

| # | Decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | What `role_access` generates | One policy per action in `actions` (default `select`, `insert`, `update`, `delete`), each `roles` = the given roles, with the always-true clause `owner_id IS NOT NULL`, named `tpl_role_access_<action>`. | The exact shape verified on 2026-11-16 on the local stack. It reuses the clause trick of `open_read` (`policy` mode always has `owner_id`). | Verified for the shape; the template name and the default of `actions` are my proposal. |
| 2 | How the role gets into the app's role list | A new plan operation `add_role` (`{role}`), additive and idempotent, admin only, validated like `PUT /apps/{id}/roles`. | Explicit operations are what the user confirms; a hidden side effect of `add_policy` on another table would not show on the card. | **No.** Alternative: `add_policy` with `role_access` registers its roles itself. Recommended default stays explicit. |
| 3 | Who is the first holder of the role | The administrator assigns it by hand in the app-users screen after the person registers. | No invite flow exists (`enduser-roles-config`, out of scope: admin-created users). | Fact. |
| 4 | Does a policy need its role in the list | No. `CreateTablePolicyForUser` checks only the role format. The list feeds the dashboard `Select` (role assignment, policy roles). | Read from `handler.go` (`UpdateAppEnduserRoles`) and `policy.go`. **[verify]** by a test that creates a policy with an unlisted role. | Not tested. |
| 5 | Plan size | `add_role` counts toward the 30-operation limit. | Parent spec assumption 4. | Inherited. |
| 6 | Brief and plan must agree | A table the brief declares `owner` or `role` is refused if the backend plan declares it `public`. `signed_in` with `public` is allowed (same meaning in Orbit). | The 2026-11-16 mismatch (brief `signed_in`, plan `public`, prose "only staff") was the proximate cause. | **No.** |
| 7 | The self-promotion check | An end user cannot change their own role. `PATCH /auth/me` accepts only `name`, `phone`, `avatar_url`. | Read from `auth/handler.go`. **[verify]** with a test, because the whole feature rests on it. | Not tested. |
| 8 | Collisions with policies that already exist | Detected at apply time (the provisioner rejects a duplicate policy name), reported like any failed operation. | `AppTableRow` carries no policies, so `Validate` cannot see them. | Fact. |
| 9 | Role name the agent picks | From the brief (for example `staff`); format `^[a-z][a-z0-9_]{0,62}$`; `member` is always valid and never needs `add_role`. | Platform rule. | Fact. |

---

## User Stories

### P1: The template ⭐ MVP

**US-1**: As a builder user (and as an Orbit admin using the dashboard or the MCP server), I want a policy template that gives some roles full access to a table so that a shared table can be limited to a team.

**Acceptance Criteria**

1. RAC-01 The template `role_access` SHALL take `roles` (required, non-empty, each matching `^[a-z][a-z0-9_]{0,62}$`) and `actions` (optional subset of `select`, `insert`, `update`, `delete`; omitted means all four; duplicates and unknown values are rejected).
2. RAC-02 For each action it SHALL produce one policy named `tpl_role_access_<action>` with the given roles and the single clause `owner_id IS NOT NULL`, and nothing else; the table must be in an access mode that has the owner column (`policy`), as for `open_read`.
3. RAC-03 The Go package `internal/policytemplates`, the TypeScript mirror `internal/dashboard/ui/src/lib/policyTemplates.ts` and the MCP tool `orbit_create_policy_from_template` (and `orbit_list_policy_templates`) SHALL expose the same template id, the same inputs and the same generated policies.
4. RAC-04 WHEN a plan contains two `role_access` operations for the same table whose `actions` overlap THEN validation SHALL refuse the plan with a message that names the table and the action and says to merge the roles into one operation.
5. RAC-05 Two `role_access` operations on one table with disjoint actions SHALL be accepted.
6. RAC-06 The builder SHALL keep the admin-only rule of `add_policy` for `role_access`, and the edit chat SHALL keep rejecting the builder-only operations.

### P1: The role

**US-2**: As a builder user, I want the plan to register the role it uses so that the administrator can assign it without leaving the dashboard.

**Acceptance Criteria**

1. RAC-07 The builder plan SHALL accept an operation `add_role` with a single `role` (format as RAC-01). Applying it SHALL add the role to the app's role list if absent and do nothing if present.
2. RAC-08 `add_role` SHALL require the admin role on the app, go through the same validation, RBAC and audit as `PUT /apps/{id}/roles` (audit origin `ai_builder`), and SHALL never remove or rename a role.
3. RAC-09 WHEN a plan uses `role_access` with a role that is neither `member`, nor already in the app's role list, nor added by an earlier `add_role` of the same plan THEN validation SHALL refuse it with a model-actionable message.
4. RAC-10 WHEN applying a plan stops after `add_role` and before `add_policy` THEN the outcome SHALL report it like any partial plan (the existing applied/failed/untouched lists); a role in the list with no policy is harmless.

### P1: The agent says and does the same thing

**US-3**: As a user who asked for "only the team", I want the plan and the backend to say the same thing so that I am not told one thing and given another.

**Acceptance Criteria**

1. RAC-11 WHEN the approved brief declares a table `owner` or `role` and the backend plan declares it `public` THEN `propose_backend_plan` SHALL refuse the plan, naming the table and the two declarations. `signed_in` with `public` SHALL stay allowed.
2. RAC-12 A brief data entry with `access: role` SHALL be carried out by `access: policy`, `set_rls_mode` `policy`, `add_role` and `add_policy` with `role_access` in the backend plan; the prompt SHALL say so, replacing the interim paragraph that says the builder cannot create that policy yet.
3. RAC-13 The interim rule stays: the agent never says that sign-up is closed, that accounts are created by an administrator, or that only the team can enter; it says that accounts wait for an administrator to assign the role.

### P2: The people on the other side

**US-4**: As a clinic employee who just signed up, I want to see that my account is waiting so that I do not think the app is broken.

**Acceptance Criteria**

1. RAC-14 WHEN a brief uses `role` access THEN the generated app SHALL offer a sign-up form (name, e-mail, password) next to sign-in, and after sign-in SHALL show an "account waiting for approval" screen for an account whose role is not one the app grants access to, instead of empty lists or 403 errors.
2. RAC-15 The built-in skill `orbit-crud-screens` (or a new built-in skill) SHALL describe this pattern. **[verify]** that the SDK exposes the signed-in user's role (`/auth/me` or the token claim) before writing the AC as final.

**US-5**: As the user confirming the plan, I want the card to say which role gets access.

**Acceptance Criteria**

1. RAC-16 The backend plan card and the single-operation card SHALL render `role_access` as "Role `staff` can read, add, change and delete" (actions as chosen) and `add_role` as "Add the role `staff`", in `en` and `pt-BR`, with the access line of a table declared `policy` naming the roles.
2. RAC-17 The card SHALL say that an administrator must assign the role to each person in the app-users screen; this line SHALL be present whenever a plan contains `role_access`.

### P3: Docs and check

**US-6**: As an operator, I want the limits written down.

**Acceptance Criteria**

1. RAC-18 `docs/docs/ai-frontend-builder.md` SHALL document the template, `add_role`, the manual role assignment, the 1-hour demotion window and the fact that sign-up stays open.
2. RAC-19 `CHANGELOG.md` `[Unreleased]` and the translated READMEs (if the feature tables change) SHALL be updated in the same change.
3. RAC-20 A local run with a real model SHALL redo the clinic CRM prompt and check, with an unprivileged self-registered account, that every table with data about people returns nothing and refuses writes. The result goes in `tasks.md`.

---

## Edge Cases

- A plan names the role `member` in `role_access`: accepted by validation (it is valid) but the card SHALL warn that every self-registered account has this role. The agent is told not to do it for data about people.
- The administrator demotes a user: access ends when their token expires (up to 1 hour); documented, not fixed.
- A role in `role_access` is later removed from the list: the platform blocks the removal while a policy uses it.
- The same plan creates the table, `add_role`, `set_rls_mode` and the four policies: order is enforced by the existing rule that `add_policy` needs the access mode earlier in the plan; `add_role` has no ordering requirement except before use (RAC-09).
- An existing policy named `tpl_role_access_select` on the table: apply fails on that operation with the existing partial-plan outcome.

## Success Criteria

- [ ] Repeating the 2026-11-16 session with the same prompt: the plan card shows the access line with a role, the unprivileged account created by the test reads nothing and writes nothing, and after the administrator assigns the role the same account can use the app.
- [ ] The three template surfaces generate identical policies for the same input (RAC-03 test).
- [ ] No new way for an end user to obtain a role without an administrator (assumption 7 test).
