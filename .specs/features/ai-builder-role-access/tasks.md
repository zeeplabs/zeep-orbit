# AI Builder Role Access Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** If the skill cannot be activated, stop and tell the user.

One commit per finished task with a green gate (authorized by the maintainer for the builder work); push only with the maintainer's OK; never add Claude/Anthropic attribution; never force push. **Do not start any task before the maintainer approves this spec** (open items: assumptions 2 and 6, the parity option in the design). R01 changes the shared template package and R03 the MCP tool: higher-risk surfaces (AGENTS §8), explain the diff before applying.

---

**Spec**: `.specs/features/ai-builder-role-access/spec.md`
**Design**: `.specs/features/ai-builder-role-access/design.md`
**Status**: Draft. No task started. Branch of the interim text fix: `fix/builder-open-signup-wording` (commits `2377306`, `ad5cabc`, not pushed).

---

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 30m ./...` (`DASHBOARD_BOOTSTRAP_SECRET` and `TEST_DATABASE_URL` set; the `dashboard` package takes about 10 minutes) |
| Frontend | UI tasks | `cd internal/dashboard/ui && node_modules/.bin/tsc -b && npm run build` plus JSON validation of `en.json` and `pt-BR.json` (do not use `npx tsc`) |

## Dependency Graph

```
R00 [verify] ─► R01 template (Go) ─► R02 TS mirror ─┐
                     │                              ├─► R07 cards + i18n ─► R09 docs ─► R10 real run
                     ├─► R03 MCP ───────────────────┤
                     └─► R04 add_role ─► R05 validation ─► R06 prompt + skill ─┘
                                                      R08 sign-up screen (skill) ─┘
```

R02 and R03 can run in parallel after R01. R04 does not depend on R02/R03.

---

## Tasks

### R00 — Verify the assumptions the design rests on
- **What**: tests or reads for the items marked **[verify]**: (a) a policy can be created with a role that is not in `enduser_roles_config`; (b) `PATCH /auth/me` cannot change `role` (and neither can any app REST route reach `_auth_users`); (c) the guard that keeps the edit chat from accepting builder-only operations; (d) how `UpdateAppEnduserRoles` handles two concurrent PUTs; (e) whether the dashboard package can read the current brief (store method or SQL); (f) the SDK call that returns the signed-in user's role.
- **Where**: `internal/auth`, `internal/dashboard`, `internal/agent/store`, the template SDK in `orbit-template`.
- **Done when**: each item is answered with a file and line or a test, and the spec's assumption table is updated (4, 7 and the design's open points). (b) is a regression test that stays.
- **Gate**: Quick for the touched package.

### R01 — Template `role_access` in `internal/policytemplates`  `[P with R04]`
- **What**: `TemplateIDRoleAccess`, `List()` entry, `BuildRoleAccessPolicies`, `Resolve` case (roles required, actions default and validation), golden fixture `testdata/role_access.json`.
- **Where**: `internal/policytemplates/templates.go`, `resolve.go`, tests.
- **Depends on**: R00 (a).
- **Done when**: tests for: default actions; a subset; duplicate and unknown actions rejected; empty roles rejected; names `tpl_role_access_<action>`; clause is `owner_id IS NOT NULL`; the golden fixture matches.
- **Gate**: Quick.

### R02 — TypeScript mirror and dashboard picker
- **What**: id, definition, builder function and picker entry in `policyTemplates.ts`; i18n strings for the template name and description.
- **Where**: `internal/dashboard/ui/src/lib/policyTemplates.ts`, the policies UI, `en.json`, `pt-BR.json`.
- **Depends on**: R01.
- **Done when**: the Go test that compares ids and flags with the TS file passes; UAT in the dashboard creates the four policies on a table in `policy` mode and the manual run of 2026-11-16 (member denied, staff allowed) repeats.
- **Gate**: Frontend.

### R03 — MCP tool uses the template
- **What**: add `role_access` to `orbit_create_policy_from_template` and `orbit_list_policy_templates`; replace the MCP copy of the template switch with `policytemplates.Resolve` if the diff stays small.
- **Where**: `internal/mcpserver/tools.go`, tests.
- **Depends on**: R01.
- **Done when**: the MCP tool and the builder resolve the same input to the same policies (a shared test table); existing MCP template tests unchanged.
- **Gate**: Quick, then Full before the commit (the MCP package shares the test database).

### R04 — Operation `add_role`
- **What**: extract `UpdateAppEnduserRolesForUser` from the handler (row lock, same audit), keep the handler a wrapper, add the `add_role` kind to the plan and the single-change parser, the apply switch and the edit-chat guard.
- **Where**: `internal/dashboard/handler.go`, `internal/dashboard/ai/client.go`, `builder_tools.go`, `builder_plan.go`, tests.
- **Depends on**: R00 (c), (d).
- **Done when**: tests for: adds a role; idempotent on an existing one; invalid format; needs admin (an editor is refused); never removes; audit row with origin `ai_builder`; the existing roles endpoint tests unchanged; a concurrent double add keeps both roles.
- **Gate**: Full.

### R05 — Plan validation
- **What**: `Validate` takes the role list; RAC-04 (overlap), RAC-09 (unknown role), `role_access` counts as a policy; RAC-11 as a separate check against the current brief in `proposeBackendPlan`.
- **Where**: `internal/dashboard/builder_plan.go`, `builder_tools.go`, tests.
- **Depends on**: R01, R04, R00 (e).
- **Done when**: table-driven tests for each rule including the accepted cases (disjoint actions, `member`, `signed_in` + `public`, no brief); messages are English and contain no Go error; the 30-operation limit counts `add_role`.
- **Gate**: Full.

### R06 — Prompt and skill
- **What**: replace the interim paragraph in `core.md` (RAC-12), list the template in the `add_policy` schema line and in the `propose_backend_plan` description, update `TestBuildSystemPrompt_CoreStatesThatSignUpIsOpen` to the new text.
- **Where**: `internal/agent/prompts/core.md`, `internal/dashboard/builder_tools.go`, `prompt_test.go`.
- **Depends on**: R05.
- **Done when**: tests pin the new instruction and keep the sign-up rules; prompt size stays within the limit the existing tests assert.
- **Gate**: Quick.

### R07 — Cards and text
- **What**: `summarizePlan` and operation sentences for `role_access` and `add_role`, the administrator line (RAC-17), access line with roles, `en` and `pt-BR`.
- **Where**: `builderModel.ts`, `ProposalCard.tsx`, `en.json`, `pt-BR.json`.
- **Depends on**: R04, R05.
- **Done when**: the card of a plan with the role and the four policies reads as in RAC-16 in headless Chromium on the local stack (scripted model), reject and confirm work; no row values on the card.
- **Gate**: Frontend.

### R08 — Sign-up and "waiting for approval" pattern
- **What**: built-in skill text (and, if the template needs it, the template's login page) for RAC-14 and RAC-15.
- **Where**: `internal/agent/skills/builtin/orbit-crud-screens/` or a new skill, `builtin.lock`, the lint and index tests.
- **Depends on**: R00 (f).
- **Done when**: the skill lints and is listed locked and loadable (`ShippedBuiltinsAreListedLockedAndLoadable`); the pattern is checked in R10.
- **Gate**: Quick.

### R09 — Docs
- **What**: RAC-18 and RAC-19; update the parent specs where they say the builder cannot restrict by role; update `ai-builder-backend/tasks.md` status.
- **Where**: `docs/docs/ai-frontend-builder.md`, `CHANGELOG.md`, translated READMEs only if needed.
- **Depends on**: R01 to R08.
- **Done when**: the doc states the template, `add_role`, manual assignment, the one-hour window, open sign-up; the interim CHANGELOG entry is amended, not duplicated.
- **Gate**: none beyond review.

### R10 — Real run
- **What**: repeat the clinic CRM prompt of 2026-11-16 with a real model on the local stack; with an unprivileged self-registered account check every table with data about people (reads empty, writes refused), then assign the role in the dashboard and check the app works; record the plan card text, the cost and the divergences.
- **Where**: scratch driver, `tasks.md`.
- **Depends on**: R06, R07, R08 (the stack image must be rebuilt first).
- **Done when**: the table below is filled; if the agent still proposes `public` for patients, that is a finding, not a pass.
- **Gate**: none (manual).

| Check | Result |
| --- | --- |
| Plan card shows role access | _pending_ |
| Self-registered account reads nothing, writes refused | _pending_ |
| After role assignment the account works | _pending_ |
| Cost of the run | _pending_ |
