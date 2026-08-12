# Policy Templates & Help Drawer Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/policy-templates/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate commands) and §5 (i18n rule); no frontend testing standard documented in `CONTRIBUTING.md`/README beyond the tools actually configured. This repo's frontend ships **only** Playwright e2e (`internal/dashboard/ui/e2e/*.spec.ts`, real backend + real Postgres, sequential multi-stage test per feature — sampled `webhooks.spec.ts`, `enduser-roles.spec.ts`) — no unit test runner (no Vitest/Jest config, no `*.test.ts` file exists anywhere under `internal/dashboard/ui/src`). That absence is the real floor for this repo: this feature does not introduce a new unit framework, it follows the existing e2e-only convention.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Frontend domain logic (`policyTemplates.ts` builders + `TEMPLATE_DEFINITIONS`) | e2e (exercised through the real UI, no unit runner available in this repo) | Every template (PTPL-01,02,04,05,06,07) triggered at least once through the browser; generated policy asserted via the resulting `pg_policies`/policy list in the UI; partial-failure + retry-skip edge case (P2 AC2/AC3) has its own stage | `internal/dashboard/ui/e2e/policy-templates.spec.ts` | `npm run test:e2e` (from `internal/dashboard/ui`, real backend running) |
| Frontend UI (`PolicyTemplatePicker`, `PolicyHelpContent`, `RoleChipPicker`, `TablePolicies.tsx` mode toggle) | e2e | Happy path per template + "Ajuda" drawer open/close preserving in-progress draft (P3 AC1/AC3) + mode toggle to/from "Modo avançado" | same file as above | same |
| `hasOwnerColumn` helper (pure refactor extraction) | none | Behavior-preserving extraction of existing inline logic — already implicitly covered by whatever e2e exercises RLS mode switching in `TableCard.tsx`; no new behavior to assert | `internal/dashboard/ui/src/lib/rls.ts` | build gate only |
| Backend (`internal/**`) | none | Spec Out of Scope — no backend file touched by this feature | - | build gate only, as a regression check |
| i18n JSON (`en.json`, `pt-BR.json`) | none | `AGENTS.md`: validated by JSON parse + consumed by `tsc`/`vite build` (unused-key or malformed-JSON would fail the build) | `src/locales/en.json`, `src/locales/pt-BR.json` | `python3 -c "import json; json.load(open('src/locales/en.json')); json.load(open('src/locales/pt-BR.json'))"` |

**Coverage Expectation rationale**: this repo's strong floor for TS logic is "exercised through e2e" (no unit layer exists for any existing pure `lib/` module either — sampled `src/lib/api.ts` has no companion unit test). Applying a stricter unit-test standard here than the rest of the codebase already uses would be inconsistent, not more rigorous.

## Gate Check Commands

> Generated from codebase - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a Foundation task with no user-visible behavior change (pure refactor/module addition) | `cd internal/dashboard/ui && npx tsc -b` |
| Full | After a task that adds/changes UI behavior reachable through the browser | `cd internal/dashboard/ui && npx tsc -b && npm run build` — plus, once the feature is wired end-to-end (T9), `npm run test:e2e -- policy-templates.spec.ts` against a running `zeep` binary (`DATABASE_URL`/`DASHBOARD_BOOTSTRAP_SECRET` set per README's configuration table) |
| Build | Phase completion / final gate | Full gate, plus `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json')); json.load(open('internal/dashboard/ui/src/locales/pt-BR.json'))"`, plus `go build ./... && go vet ./...` from repo root as a backend-regression sanity check (no backend file is expected to change) |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation

Pure refactors and pure logic — no new user-visible behavior yet.

T1 has no dependency. T2 has no dependency (independent of T1). T3 depends on T2 (reuses its builders). T4 has no dependency (independent extraction, same file family as T2/T3 but unrelated code).

### Phase 2: Core UI

New components that consume Phase 1's logic.

T5 has no dependency. T6 depends on T2 and T4. T7 depends on T3 and T6.

### Phase 3: Integration & Verification

Wires everything into the existing screen and closes the deferred e2e coverage from Phase 1/2 (compilation-dependency merge-forward, see Test Co-location Validation below).

T8 depends on T5, T6, and T7. T9 depends on T8.

---

## Task Breakdown

### T1: Extract `hasOwnerColumn` helper, replace inline duplicates in `TableCard.tsx`

**What**: New exported function `hasOwnerColumn(rls: string): boolean` returning `rls === "owner" || rls === "enabled" || rls === "policy"`; replace the two inline expressions in `TableCard.tsx` (`autoColumnsFor`'s condition at line 84 and `isPolicyRLS`'s comparison is a *different* check and stays — only the `autoColumnsFor` condition and any other place computing "does this rls value have an owner column" are replaced) with a call to the new helper.
**Where**: `internal/dashboard/ui/src/lib/rls.ts` (new file), `internal/dashboard/ui/src/components/TableCard.tsx` (modify line ~84)
**Depends on**: None
**Reuses**: Mirrors `internal/config/rls.go`'s `HasOwnerColumn` — same three-value set, same semantics, kept in sync per the risk flagged in `design.md`.
**Requirement**: Design decision (`hasOwnerColumn` centralization) — supports gating for PTPL-01, PTPL-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `hasOwnerColumn(rls)` exported from `internal/dashboard/ui/src/lib/rls.ts`, matching `internal/config/rls.go`'s three-value semantics
- [x] `TableCard.tsx`'s inline "owner/enabled/policy" check for auto-columns now calls `hasOwnerColumn`, no behavior change (`autoColumnsFor` output identical for all 4 `rls` values: `""`, `"owner"`, `"enabled"`, `"policy"`)
- [x] Gate check passes: `cd internal/dashboard/ui && npx tsc -b`

**Status**: ✅ Complete

**Tests**: none (behavior-preserving refactor of already-untested inline logic — see Test Coverage Matrix)
**Gate**: quick

**Commit**: `refactor(dashboard-ui): extract hasOwnerColumn helper from TableCard`

---

### T2: `policyTemplates.ts` — single-action template builders

**What**: New pure module exporting `generatedPolicyName(templateId, action)`, and the single-action builders `buildOwnerOnlyPolicies(actions, roles)` (PTPL-01), `buildOpenReadPolicy(roles)` (PTPL-02), `buildReadOnlyPolicy(roles)` (PTPL-04, reuses `buildOpenReadPolicy`'s shape), `buildValueMatchPolicy(column, value, roles)` (PTPL-05); plus a `TEMPLATE_DEFINITIONS` array with entries for `owner_only`, `open_read`, `read_only`, `value_match`, and the non-actionable `blocked_by_default` (PTPL-07, `kind: "info"`, no builder).
**Where**: `internal/dashboard/ui/src/lib/policyTemplates.ts` (new file)
**Depends on**: None
**Reuses**: `PolicyDef`/`PolicyClause` types from `internal/dashboard/ui/src/lib/api.ts:206-219`.
**Requirement**: PTPL-01, PTPL-02, PTPL-04, PTPL-05, PTPL-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `buildOwnerOnlyPolicies(["select","update"], ["member"])` returns one `PolicyDef` per action, each with `clauses: [{column: "owner_id", operator: "=", value_source: "claim", value: "sub"}]` and `roles: ["member"]`
- [x] `buildOpenReadPolicy(["member"])` returns a single `PolicyDef` with `action: "select"` and `clauses: [{column: "owner_id", operator: "IS NOT NULL"}]` (no `value`/`value_source`, matching the unary-operator shape `internal/provisioner/policy.go` expects)
- [x] `buildValueMatchPolicy("status", "published", ["member"])` returns `action: "select"`, `clauses: [{column: "status", operator: "=", value_source: "literal", value: "published"}]`
- [x] `TEMPLATE_DEFINITIONS` has exactly 5 entries (`owner_only`, `open_read`, `read_only`, `value_match`, `blocked_by_default`), each with `requiresOwnerColumn` set per the spec Assumptions table
- [x] Gate check passes: `cd internal/dashboard/ui && npx tsc -b`

**Status**: ✅ Complete

**Tests**: none — merge-forward: this pure logic can't be driven through the browser until `PolicyTemplatePicker` wires it in; e2e coverage for every builder above lands in T9's `policy-templates.spec.ts` (Resolving Compilation Dependencies, `tasks.md` reference)
**Gate**: quick

**Commit**: `feat(dashboard-ui): add single-action policy template builders`

---

### T3: `policyTemplates.ts` — composite template builder

**What**: Add `buildOpenReadOwnerWritePolicies(readRoles)` (PTPL-06), returning an array of 3 `PolicyDef`s built by reusing T2's `buildOpenReadPolicy(readRoles)` for `select` and `buildOwnerOnlyPolicies(["update","delete"], readRoles)` for the write actions — no new clause shape duplicated. Add the `open_read_owner_write` entry to `TEMPLATE_DEFINITIONS` with `kind: "composite"`.
**Where**: `internal/dashboard/ui/src/lib/policyTemplates.ts` (modify)
**Depends on**: T2
**Reuses**: `buildOpenReadPolicy`, `buildOwnerOnlyPolicies` (both from T2) — no duplicated clause-shape logic.
**Requirement**: PTPL-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `buildOpenReadOwnerWritePolicies(["member"])` returns exactly 3 `PolicyDef`s: one `select` (open-read shape), one `update` and one `delete` (both owner-only shape), all with `roles: ["member"]`
- [x] `TEMPLATE_DEFINITIONS` now has 6 entries, `open_read_owner_write` marked `kind: "composite"`
- [x] Gate check passes: `cd internal/dashboard/ui && npx tsc -b`

**Status**: ✅ Complete

**Tests**: none — merge-forward to T9 (same rationale as T2; composite flow additionally needs T7's retry/partial-failure UI before it is meaningfully testable through the browser)
**Gate**: quick

**Commit**: `feat(dashboard-ui): add composite open-read/owner-write policy template builder`

---

### T4: Extract `RoleChipPicker` component

**What**: New component `<RoleChipPicker availableRoles selected onToggle label />`, extracted 1:1 from `TablePolicies.tsx`'s existing `chipRoles`/`toggleRole` block (lines 77-83, 211-236) — same orphaned-role behavior (`ROLECFG-16`), no visual or behavioral change. Update `TablePolicies.tsx`'s advanced form to render `<RoleChipPicker>` instead of its inline JSX, computing `chipRoles` the same way but now inside the shared component.
**Where**: `internal/dashboard/ui/src/components/RoleChipPicker.tsx` (new file), `internal/dashboard/ui/src/components/TablePolicies.tsx` (modify, replace lines 211-236)
**Depends on**: None
**Reuses**: Exact logic already in `TablePolicies.tsx` — extraction, not reimplementation.
**Requirement**: Design decision (shared role picker) — supports PTPL-01, PTPL-02, PTPL-06 role selection in T6/T7

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `RoleChipPicker` renders identically to the block it replaces (same chip styling classes, same orphaned-role-stays-visible behavior)
- [x] Advanced form in `TablePolicies.tsx` uses `RoleChipPicker`, existing advanced-form policy creation/edit still works with no behavior change
- [x] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Status**: ✅ Complete

**Tests**: none — behavior-preserving extraction; the advanced form's existing e2e coverage (if any touches policies) is unaffected; new coverage for its reuse in template mode lands in T9
**Gate**: full

**Commit**: `refactor(dashboard-ui): extract RoleChipPicker from TablePolicies advanced form`

---

### T5: `PolicyHelpContent` component + tutorial i18n content

**What**: New component `<PolicyHelpContent />` rendering the tutorial required by spec P3: intro explaining Column/Operator/ValueSource/Logic in plain language, and ≥3 complete advanced-clause examples, each using only real allowlisted operators (`=`, `!=`, `IN`, `NOT IN`, `>`, `<`, `>=`, `<=`, `IS NULL`, `IS NOT NULL`) and real claims (`role`, `sub`, `email`) — no `LIKE`, `now()`, or invented claim in any example. All text via `t("tablePolicies.help.*")`.
**Where**: `internal/dashboard/ui/src/components/PolicyHelpContent.tsx` (new file), `internal/dashboard/ui/src/locales/en.json`, `internal/dashboard/ui/src/locales/pt-BR.json` (add `tablePolicies.help.*` keys, both files, same change)
**Depends on**: None
**Reuses**: Nothing — new static content.
**Requirement**: PTPL-08 (drawer de ajuda, P3)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] Component renders ≥3 example blocks, each showing column/operator/claim-or-literal/logic in plain text (not a live form)
- [x] Every operator and claim referenced in the examples is a member of the real allowlists (`internal/provisioner/policy.go:21-32,40-45`) — manually cross-checked against the file: example 1 uses `=`/claim `email`; example 2 uses `IN`/`>=` with literals; example 3 uses `IS NULL`/`=` with claim `sub` — no `LIKE`, no SQL function, no invented claim
- [x] All strings present in both `en.json` and `pt-BR.json` under `tablePolicies.help.*`
- [x] `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json')); json.load(open('internal/dashboard/ui/src/locales/pt-BR.json'))"` passes
- [x] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Status**: ✅ Complete

**Tests**: none — merge-forward to T9 (drawer open/close-preserves-draft behavior needs T8's wiring to be reachable through the browser)
**Gate**: full

**Commit**: `feat(dashboard-ui): add policy help drawer content`

---

### T6: `PolicyTemplatePicker` — single-action templates

**What**: New component rendering the 5 single-action/info templates from `TEMPLATE_DEFINITIONS` (`owner_only`, `open_read`, `read_only`, `value_match`, `blocked_by_default`) as a list. Each actionable template collects its minimal input (action checkboxes for `owner_only`; role selection via `RoleChipPicker` for `owner_only`/`open_read`/`read_only`; column dropdown + literal value input for `value_match`) and, on apply, calls `useCreateTablePolicy`'s `mutateAsync` once per generated `PolicyDef` (sequentially, awaiting each). `blocked_by_default` renders as static explanatory text, never calls the mutation. Templates requiring `owner_id` are hidden when `hasOwnerColumn(rls)` is false (prop `rls` passed in).
**Where**: `internal/dashboard/ui/src/components/PolicyTemplatePicker.tsx` (new file)
**Depends on**: T2, T4
**Reuses**: `policyTemplates.ts` builders (T2), `RoleChipPicker` (T4), `useCreateTablePolicy` (`api.ts:243`), `hasOwnerColumn` (T1).
**Requirement**: PTPL-01, PTPL-02, PTPL-04, PTPL-05, PTPL-07 (spec P1, AC1-9 except AC1/AC7 which also require T7/T8)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] All 5 templates render; `owner_only` hidden when `hasOwnerColumn(rls)` is false
- [ ] Applying `owner_only` for 2 selected actions calls `createPolicy.mutateAsync` twice with the `PolicyDef`s from `buildOwnerOnlyPolicies`
- [ ] Applying `open_read`/`read_only`/`value_match` calls it once each with the matching builder's output
- [ ] `blocked_by_default` never triggers a mutation call
- [ ] Failure on any call surfaces via the existing `toast.error` (`useCreateTablePolicy`'s `onError`, `api.ts:243-260`) — no unhandled promise rejection, no stuck loading state
- [ ] All new strings (template names/descriptions/inputs) present in `en.json` and `pt-BR.json`
- [ ] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none — merge-forward to T9 (needs to be mounted inside `TablePolicies.tsx`, T8, to be reachable through a real browser session)
**Gate**: full

**Commit**: `feat(dashboard-ui): add PolicyTemplatePicker with single-action templates`

---

### T7: `PolicyTemplatePicker` — composite template with partial-failure handling

**What**: Add the `open_read_owner_write` (PTPL-06) template to `PolicyTemplatePicker`: on apply, checks `existingPolicies` (passed in via prop, sourced from `useTablePolicies`) for a policy already matching each of the 3 generated actions' `generatedPolicyName`, skips any that already exist, and calls `createPolicy.mutateAsync` sequentially for the rest — stopping at the first failure (spec P2 AC2) and rendering a per-action status list (created / failed-with-reason / skipped-already-exists / pending). The apply button (and the mode/template switch) is disabled while the sequence is in flight (design Risk mitigation).
**Where**: `internal/dashboard/ui/src/components/PolicyTemplatePicker.tsx` (modify)
**Depends on**: T3, T6
**Reuses**: `buildOpenReadOwnerWritePolicies` (T3), the same `createPolicy` call already wired in T6.
**Requirement**: PTPL-06 (spec P2, AC1-3)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Applying the composite template with no existing policies creates all 3 (select, update, delete) via 3 sequential calls
- [ ] If the 2nd call fails, the 3rd is never attempted; UI shows "created: select" / "failed: update (<message>)" / "pending: delete"
- [ ] Re-applying after that partial failure skips `select` (already exists per `existingPolicies`) and retries only `update`/`delete`
- [ ] Apply button and template/mode switching are disabled for the duration of the sequential calls
- [ ] All new strings present in `en.json` and `pt-BR.json`
- [ ] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none — merge-forward to T9 (partial-failure/retry scenario needs a real backend round-trip through the wired screen to be meaningfully verified, not a mock)
**Gate**: full

**Commit**: `feat(dashboard-ui): add composite policy template with partial-failure handling`

---

### T8: Wire template mode, "Modo avançado" toggle, and help drawer into `TablePolicies.tsx`

**What**: Add a `mode: "templates" | "advanced"` state to `TablePoliciesTab`, defaulting to `"templates"`. Render `PolicyTemplatePicker` (T6/T7) when `mode === "templates"`, the existing advanced form (lines 188-377, now using `RoleChipPicker` per T4) when `mode === "advanced"` — no behavior change to the advanced form itself. Add a header row with an "Ajuda" button (opens `FormDrawer` wrapping `PolicyHelpContent` from T5) and a "Modo avançado" toggle, both always visible. `TablePoliciesTabProps` gains a required `rls: string` field, passed through to `PolicyTemplatePicker`.
**Where**: `internal/dashboard/ui/src/components/TablePolicies.tsx` (modify)
**Depends on**: T5, T6, T7
**Reuses**: `FormDrawer` (`components/patterns/FormDrawer.tsx`), `PolicyTemplatePicker`, `PolicyHelpContent`.
**Requirement**: PTPL-01 through PTPL-08 (integration point for the whole feature; spec P1 AC1, P3 AC1/AC3)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Opening "create policy" on a table shows the template picker by default
- [ ] "Modo avançado" switches to the existing technical form and back, without losing already-selected roles when the fields translate directly (spec Edge Cases)
- [ ] "Ajuda" opens the drawer over whichever mode/form is active; closing it preserves that mode's in-progress state
- [ ] `TablePoliciesTabProps.rls` is required and consumed
- [ ] Gate check passes: `cd internal/dashboard/ui && npx tsc -b && npm run build`

**Tests**: none — merge-forward to T9 (this task completes the wiring; T9 supplies the `rls` value from `TableCard` and adds the e2e suite that exercises everything end-to-end)
**Gate**: full

**Commit**: `feat(dashboard-ui): wire policy template picker, advanced mode toggle, and help drawer`

---

### T9: Pass `rls` from `TableCard`, add end-to-end Playwright coverage

**What**: Pass `rls={table.rls}` into `<TablePoliciesTab>` at `TableCard.tsx:415`. Add `internal/dashboard/ui/e2e/policy-templates.spec.ts` (Playwright, following the `webhooks.spec.ts` convention: `beforeAll` bootstrap+login, one sequential `test()` with stages commented to their spec ACs) covering: `owner_only` template (multi-action), `open_read`, `read_only`, `value_match`, `blocked_by_default` (asserts no policy created), the composite template's happy path AND its partial-failure-then-retry stage (force a failure, e.g. by pre-creating a colliding policy name before applying the template), the "Ajuda" drawer open/close preserving an in-progress draft, and the "Modo avançado" toggle round-trip.
**Where**: `internal/dashboard/ui/src/components/TableCard.tsx` (modify line 415), `internal/dashboard/ui/e2e/policy-templates.spec.ts` (new file)
**Depends on**: T8
**Reuses**: `bootstrapOrSkip`/`login` helpers (`e2e/helpers.ts`), same multi-stage single-`test()` pattern as `webhooks.spec.ts`.
**Requirement**: PTPL-01 through PTPL-08 (closes all merge-forward test debt from T2, T3, T6, T7, T8)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `table.rls` flows into `TablePoliciesTab` at the real call site
- [ ] `policy-templates.spec.ts` exercises every template (PTPL-01,02,04,05,06,07) and the help drawer, each stage commented with the AC it covers
- [ ] Partial-failure stage genuinely forces the 2nd/3rd composite call to fail and asserts the resulting status list and successful retry-skip
- [ ] Gate check passes (Full): `cd internal/dashboard/ui && npx tsc -b && npm run build`, then `npm run test:e2e -- policy-templates.spec.ts` against a running `zeep` binary with `DATABASE_URL`/`DASHBOARD_BOOTSTRAP_SECRET` set
- [ ] Build gate passes: `python3 -c "import json; json.load(open('internal/dashboard/ui/src/locales/en.json')); json.load(open('internal/dashboard/ui/src/locales/pt-BR.json'))"`, `go build ./... && go vet ./...` from repo root (no backend regression)

**Tests**: e2e
**Gate**: full

**Commit**: `test(dashboard-ui): add end-to-end coverage for policy templates and help drawer`

---

## Phase Execution Map

Phases run in sequence; tasks within a phase run in order.

Phase 1 (Foundation): T1, then T2, then T3 (T3 depends on T2), then T4 (T4 depends on nothing in this phase, runs after T1-T3 only because tasks in a phase execute in listed order, not because of a real dependency).

Phase 2 (Core UI): T5, then T6 (depends on T2 and T4 from Phase 1), then T7 (depends on T3 from Phase 1 and T6 from this phase).

Phase 3 (Integration & Verification): T8 (depends on T5, T6, T7), then T9 (depends on T8).

**How phase-based execution works:** at Execute, the agent counts total tasks (9 here) and packs phases into task-budgeted batches (~7 tasks per worker, whole phases only — the cut lands on a phase boundary, never mid-phase). 9 tasks exceeds the single-batch threshold (~8), so batch sub-agents will be offered before Execute starts: Batch 1 = Phase 1 + Phase 2 (7 tasks: T1-T7), Batch 2 = Phase 3 (2 tasks: T8-T9). Batches run sequentially; a worker executes all its tasks in order, then reports before the next batch starts.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Extract `hasOwnerColumn` helper | 1 function + 1 call-site update | ✅ Granular |
| T2: Single-action template builders | 1 new module (cohesive set of pure functions + 1 data array) | ✅ Granular |
| T3: Composite template builder | 1 function added to existing module from T2 | ✅ Granular |
| T4: Extract `RoleChipPicker` | 1 component | ✅ Granular |
| T5: `PolicyHelpContent` + i18n | 1 component + its i18n keys (same change, per `AGENTS.md` §5) | ✅ Granular |
| T6: `PolicyTemplatePicker` (single templates) | 1 component | ✅ Granular |
| T7: Composite template + partial-failure UI | 1 component modification, 1 cohesive behavior (composite apply flow) | ✅ Granular |
| T8: Wire mode/toggle/drawer into `TablePolicies.tsx` | 1 file modification, 1 cohesive concern (integration wiring) | ✅ Granular |
| T9: `rls` prop + e2e suite | 1-line prop change + 1 new test file, bundled because the e2e suite is the earliest point the prop change becomes observable/testable (compilation-dependency merge-forward) | ✅ Granular (justified bundling, not scope creep) |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | None | None | ✅ Match |
| T3 | T2 | T2 | ✅ Match |
| T4 | None | None | ✅ Match |
| T5 | None | None | ✅ Match |
| T6 | T2, T4 | T2, T4 | ✅ Match |
| T7 | T3, T6 | T3, T6 | ✅ Match |
| T8 | T5, T6, T7 | T5, T6, T7 | ✅ Match |
| T9 | T8 | T8 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `hasOwnerColumn` | `hasOwnerColumn` helper (pure refactor) | none | none | ✅ OK |
| T2: single-action builders | Frontend domain logic (`policyTemplates.ts`) | e2e | none (merge-forward → T9) | ✅ OK (compilation-dependency, justified) |
| T3: composite builder | Frontend domain logic (`policyTemplates.ts`) | e2e | none (merge-forward → T9) | ✅ OK (compilation-dependency, justified) |
| T4: `RoleChipPicker` | Frontend UI | e2e | none (behavior-preserving extraction; new-context coverage merge-forward → T9) | ✅ OK |
| T5: `PolicyHelpContent` | Frontend UI | e2e | none (merge-forward → T9) | ✅ OK (compilation-dependency, justified) |
| T6: `PolicyTemplatePicker` (single) | Frontend UI + domain logic consumer | e2e | none (merge-forward → T9) | ✅ OK (compilation-dependency, justified) |
| T7: composite + partial-failure | Frontend UI + domain logic consumer | e2e | none (merge-forward → T9) | ✅ OK (compilation-dependency, justified) |
| T8: wiring into `TablePolicies.tsx` | Frontend UI (integration) | e2e | none (merge-forward → T9) | ✅ OK (compilation-dependency, justified — screen only becomes end-to-end reachable once `rls` flows in via T9) |
| T9: `rls` prop + e2e suite | Frontend UI (final wiring) + the e2e suite itself | e2e | e2e | ✅ OK |

**Merge-forward justification (applies to T2, T3, T5, T6, T7, T8):** none of these layers can be driven through a real browser session until the full chain — template logic → picker UI → mode/drawer wiring → real `rls` prop — is connected, which only happens at T9. Splitting a "write tests" task out at T9 would be the deferred-testing anti-pattern; instead T9 *is* the earliest point this code is reachable at all, so bundling the e2e suite there is the correct resolution per the "Resolving compilation dependencies" rule, not a violation of it.

---

## Task Verification Standards

Every task above follows `Done when` + `Tests` + `Gate`, each entry specific and binary pass/fail, referencing the exact commands from **Gate Check Commands**.
