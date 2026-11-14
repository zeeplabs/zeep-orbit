# AI Builder Economy Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** If the skill cannot be activated, stop and tell the user.

One commit per finished task with a green gate (authorized by the maintainer for the builder work); push only with the maintainer's OK; never add Claude/Anthropic attribution; never force push. **E01 adds DDL to `provisioner.go` (higher-risk, AGENTS §8): do not start it before the maintainer approves the spec.** E06 and E07 and E09 spend real tokens: say the expected cost and get an OK before each run, and never read or print provider keys.

---

**Spec**: `.specs/features/ai-builder-economy/spec.md`
**Design**: `.specs/features/ai-builder-economy/design.md`
**Status**: Draft (2026-11-14), not approved. Open for the maintainer: A1 (same provider only) and A4 (acceptance thresholds). Nothing implemented.

---

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 30m ./...` (the `dashboard` package takes about 10 minutes locally; `DASHBOARD_BOOTSTRAP_SECRET` and `TEST_DATABASE_URL` must be set) |
| Frontend | UI tasks | `cd internal/dashboard/ui && node_modules/.bin/tsc -b && npm run build` plus i18n JSON validation for `en.json` and `pt-BR.json` (do not use `npx tsc`: it pulls a newer TypeScript that rejects `baseUrl`) |

## Dependency Graph

```
E01 setting + DDL ──► E02 runtime ──► E06 eval harness ──► E07 eval run + decision ──┐
        │                  │                                                         │
        └──► E05 frontend ◄┴── E03 report by purpose                                 ├──► E08 docs ──► E09 local check
                                E04 estimate split ──────────────────────────────────┘
```

E03 and E04 do not depend on E01 and can run first.

---

## Tasks

### E03 — Usage report by purpose  `[P]`
- **What**: `group_by=purpose` in `UsageReport`; 400 message lists it.
- **Where**: `internal/dashboard/builder_usage.go`, `builder_usage_test.go`.
- **Requirements**: AEC-08.
- **Done when**: rows per purpose add up to the totals; a non-superadmin sees only their rows; the 400 message names `purpose`.
- **Tests**: unit/integration in the dashboard package (existing usage test helpers). **Gate**: Full.
- **Status**: Pending.

### E04 — Estimate split first vs later  `[P]`
- **What**: `EstimateBuildTurn` computes the `milestone = 1` and `milestone > 1` groups in the same query; `PlanEstimate` uses them when both have 10 turns and returns `basis`.
- **Where**: `internal/agent/store/usage.go`, `internal/agent/store/estimate_test.go`, `internal/dashboard/builder_planning.go`, `builder_brief_test.go` (or the file that tests the estimate).
- **Requirements**: AEC-10.
- **Done when**: both groups filled → split range and `basis=split`; one short → old numbers and `basis=single`; `milestone IS NULL` turns only in the single group; cost nil under the priced minimum. Existing estimate tests still pass unchanged.
- **Tests**: store (seeded turns) and handler. **Gate**: Full.
- **Status**: Pending.

### E01 — `planning_model` setting and DDL  `[higher-risk: DDL]`
- **What**: the column (design §2), the field on the settings struct, the update input, the validation and the clearing on provider change, the audit field.
- **Where**: `internal/dashboard/provisioner.go`, `builder_settings_store.go`, `builder_settings.go`, `builder_catalog.go` (reuse `catalogModelChoosable`), tests beside them.
- **Requirements**: AEC-01, AEC-03, AEC-04.
- **Done when**: the setting table in design §7 passes; an existing installation with no column migrates with the default `''`; the idempotent DDL runs twice without error. **[verify]** the audit payload and the catalog's context-window data (design §3).
- **Tests**: integration (settings handler). **Gate**: Full.
- **Status**: Pending, needs the maintainer's approval of the DDL.

### E02 — Runtime: planning calls on the planning model
- **What**: `Deps.PlanningProvider`; `modelProvider()` in `run`; `buildProvider` extracted from `resolveProvider`; `resolvePlanningProvider` with the fallback and the warning; `prepareDeps` wiring.
- **Where**: `internal/agent/loop.go`, `internal/agent/planning_loop.go` (**[verify]** where `planMode` flips on approval), `internal/dashboard/builder_runtime.go`, tests in `internal/agent` and `internal/dashboard`.
- **Depends on**: E01.
- **Requirements**: AEC-02, AEC-05, and AEC-01 for the nil case.
- **Done when**: the runtime table in design §7 passes, including the ledger model of each row and the switch back to the build model after approval; `Deps` validation does not require `PlanningProvider`.
- **Tests**: unit with two fake providers recording their requests; integration for the fallback. **Gate**: Full.
- **Status**: Pending.

### E05 — Frontend: planning model select and purpose grouping
- **What**: a "Planning model" select in the AI Builder settings tab (options: "Same as the build model" and the catalog models of the selected provider; sends `planning_model` explicitly, `""` to clear); the purpose grouping and labels on the usage page; types in `lib/api.ts`.
- **Where**: `internal/dashboard/ui/src/pages/settings/` (the AI Builder tab), `pages/BuilderUsagePage.tsx`, `lib/api.ts`, `locales/en.json`, `locales/pt-BR.json`.
- **Depends on**: E01, E03.
- **Requirements**: AEC-09 and the UI of AEC-03/AEC-04.
- **Done when**: both locales have every string; changing the provider refreshes the options and shows a cleared planning model; the mutation has `onError` toast; the grouping option appears.
- **Tests**: `tsc -b`, build, i18n JSON; a Playwright case only if the existing settings tab has one to extend. **Gate**: Frontend.
- **Status**: Pending.

### E06 — Planning eval harness (opt-in)
- **What**: the `planningeval` test file, the 12 briefs, the answer script, the output lines, the shuffled and anonymized plan files, the cost cap (design §6).
- **Where**: `internal/agent/planning_eval_test.go`, `internal/agent/testdata/planeval/briefs.json`.
- **Depends on**: E02 (the loop must accept a planning provider; the harness may also build the provider directly if that is simpler, **[verify]**).
- **Requirements**: AEC-06, AEC-07.
- **Done when**: `go vet -tags planningeval ./internal/agent/` passes; with `PLANEVAL_PROVIDERS` empty the test skips; a dry run against a fake provider prints 12 lines and a totals line and writes 12 anonymized files.
- **Tests**: the dry run is a normal test (no tag) over the printing and anonymizing helpers. **Gate**: Quick.
- **Status**: Pending.

### E07 — Run the eval and decide  (spends real tokens)
- **What**: run the harness for the build model and the candidate planning model on the same 12 briefs; the maintainer reads the anonymized plans; record the result and a go / no-go in `design.md` Risks and here.
- **Depends on**: E06, and the maintainer setting the acceptance thresholds (A4).
- **Expected cost**: about 12 plan turns per model; at the sample's US$ 0.016 per planning call and 3 to 5 calls per turn, roughly US$ 0.6 to 0.9 for Sonnet and under US$ 0.05 for Haiku. **Ask before running.**
- **Done when**: the result lines are pasted in `design.md`; the decision is written; if no-go, E09 is skipped and the setting stays empty.
- **Status**: Pending.

### E08 — Docs and CHANGELOG
- **What**: `docs/docs/ai-frontend-builder.md` (the planning model setting, the report grouping, the estimate), `CHANGELOG.md` under `[Unreleased]`, the parent specs' "Out of Scope"/risk lines that point to this feature (`ai-builder-costs` spec row 8, `ai-builder-planning` spec decision 6 and design §9 limit), README only if a user-facing table lists settings. English only in docs.
- **Depends on**: E05, E07.
- **Done when**: the docs state that the default changes nothing, the A1 limit (same provider) and how to check the share of planning in the report; the parent lines are marked as done by this feature.
- **Gate**: none beyond `gofmt`/JSON checks; Quick if code was touched.
- **Status**: Pending.

### E09 — Local check with a planning model set  (spends real tokens)
- **What**: in the local stack, set the planning model, run a vague request to a plan, approve it, and let the build run; read the ledger by purpose and by model; confirm design A6 (cache at the mode switch) and the Haiku caching [verify]; reset the setting if the owner wants.
- **Depends on**: E02, E05, E07 (go).
- **Expected cost**: about US$ 0.15 to 0.4 for one turn on Sonnet build plus Haiku planning. **Ask before running.**
- **Done when**: planning rows show the cheap model and at least 80% lower cost per call (spec success criterion); the first build call's `cache_write_tokens` is recorded with and without the setting; the numbers go in the "Real turns" notes of this file.
- **Status**: Pending.

---

## Requirement coverage

| Requirement | Task |
| --- | --- |
| AEC-01 | E01, E02 |
| AEC-02 | E02 |
| AEC-03 | E01, E05 |
| AEC-04 | E01, E05 |
| AEC-05 | E02 |
| AEC-06 | E06, E07 |
| AEC-07 | E06 |
| AEC-08 | E03 |
| AEC-09 | E05 |
| AEC-10 | E04 |

**Coverage:** 10 of 10 requirements mapped.
