# AI Builder Economy Design

**Spec**: `.specs/features/ai-builder-economy/spec.md`
**Status**: Draft (2026-11-14). **One DDL change** (§2, higher-risk), which needs the maintainer's approval before it is written.

Items marked **[verify]** were not confirmed in the code or against a real call and must be checked in the task that names them.

---

## 1. Overview

Four changes, none of which touches the agent loop's control flow:

```
builder_settings.planning_model ──► prepareDeps ──► agent.Deps.PlanningProvider
                                                          │
                       run.chat: planMode && PlanningProvider != nil ──► that provider
                                                          │
                              ledger row = provider/model actually used (priced by the catalog)

usage report ──► group_by=purpose          estimate ──► split by turns.milestone
planning eval (opt-in build tag) ──► same 12 briefs, two models, printed lines
```

Reuses, unchanged: the model catalog and `callCost` (`internal/agent/catalog`, `store.callCost`), the ledger `purpose` column (no CHECK, `PurposePlanning` exists), `resolveProvider` in `builder_runtime.go`, `catalogModelChoosable`, the `milestone` column of `builder_turns`, the report query builder in `builder_usage.go`.

## 2. Setting and DDL  `[higher-risk: DDL]`

One column, added the way `planning_mode` was (idempotent, in `provisioner.go` next to the other `builder_settings` changes):

```sql
ALTER TABLE zeep_system.builder_settings
  ADD COLUMN IF NOT EXISTS planning_model TEXT NOT NULL DEFAULT ''
```

- `''` means "same as the build model" (AEC-01). No data `UPDATE`.
- `BuilderSettings` gains `PlanningModel string \`json:"planning_model"\``; `UpdateBuilderSettings` input gains `PlanningModel *string` (merge-on-absent, like the others). The frontend must send the key when clearing it (AGENTS §4, partial updates).
- Validation in `builder_settings.go`, after the provider is known (the new one when it changes in the same request, else the saved one):
  - empty: always valid;
  - `openai` / `anthropic`: `catalog.Lookup(provider, model)` must succeed and the model must be choosable, i.e. `catalogModelChoosable(provider, model, saved)` with `saved` = the current `planning_model` (AEC-03). English error: `planning_model must be a model of the selected provider's catalog`;
  - `openai_compatible`: any non-empty string up to the same length limit as the provider row's model (the catalog does not cover it);
  - provider changed and the saved `planning_model` is not valid for the new provider: **clear it in the same update** (AEC-04). Clearing is safer than refusing, because the operator did not touch that field. The response returns the cleared value so the UI shows it.
- Audit: the existing settings audit entry gains the field (same entry, no new event type). **[verify]** the audit payload builder in `builder_settings.go`.

## 3. Runtime

`agent.Deps` gains:

```go
// PlanningProvider answers the calls of a plan-mode turn. Nil means the
// turn's Provider answers them.
PlanningProvider provider.Provider
```

- `run.chat` (`internal/agent/loop.go`, where `r.deps.Provider.Chat` is called, and where `Provider`/`Model` of the usage record are read, lines ~395 and ~426) picks `p := r.deps.Provider; if r.planMode && r.deps.PlanningProvider != nil { p = r.deps.PlanningProvider }` and uses `p` for the call, for the error log and for the usage record (AEC-02). A helper `func (r *run) modelProvider() provider.Provider` keeps the three uses in step.
- Compaction keeps `r.deps.Provider`: it summarizes history that the build model will read, and the summary quality matters more than its cost. No compaction call exists in the ledger today. (Out of scope in the spec.)
- `prepareDeps` (`builder_runtime.go`): after `base.Provider = rt.resolveProvider(...)`, if `s.PlanningModel != "" && s.PlanningModel != <build model>`, `base.PlanningProvider = rt.resolvePlanningProvider(ctx, s.Provider, s.PlanningModel)`.
- `resolvePlanningProvider` is `resolveProvider` with the model replaced: it calls `ResolveAIProviderRuntime`, sets `prt.Model = model`, checks `catalogModelUsable`, and builds the same adapter. **Any failure returns nil and logs a warning with the provider and model** (AEC-05, A5); the turn then runs on the build model. To avoid copying the adapter-construction block, extract it from `resolveProvider` into `buildProvider(prt *AIProviderRuntime) (provider.Provider, error)` and call it from both.
- The key is the provider row's key. The planning model is not a second credential.
- Per turn, read fresh with the other settings (`prepareDeps` already does this), so a resumed turn takes the current setting (edge case in the spec).
- A plan-mode turn that is approved and goes on to build in the same turn changes `r.planMode` at that point (**[verify]** where, in `planning_loop.go`), so the build calls use `Provider` with no further code.

**Prompt cache.** The cache prefix is tools → system → messages. Plan-mode and build-mode already send different tool lists, so the planning prefix was never reusable by the build. Changing the model at the mode switch should therefore cost nothing extra (A6). **[verify in E09]** by comparing `cache_write_tokens` of the first build call of a turn with and without a planning model.

**Context window.** `ContextWindowTokens` is one installation setting used by compaction (`compaction.go:71`). A planning model with a smaller window than that setting could receive a history that does not fit; the existing `ErrContextTooLong` → compaction path handles it at the cost of one call. **[verify]** that the catalog states the context window of Haiku 5.5, and refuse a `planning_model` whose window is below `context_window_tokens` if it does.

## 4. Usage report by purpose

`builderUsageGroups` in `builder_usage.go` gets:

```go
"purpose": {
    cols:  `l.purpose, NULL::text, false, NULL::text, NULL::text`,
    group: `l.purpose`,
    order: `SUM(l.cost_micros) DESC NULLS LAST, count(*) DESC, l.purpose`,
},
```

- The 400 message of `UsageReport` lists `'purpose'` (AEC-08).
- Visibility is unchanged: the filter `builderUsageWhere` already restricts a non-superadmin to their rows. `purpose` is not personal data.
- The CSV export already has a `purpose` column; no change.
- Frontend: `BuilderUsagePage.tsx` adds the grouping to its selector. Labels for `chat`, `planning`, `compaction` come from `t()` keys in `en.json` and `pt-BR.json` (the key from the API is the purpose id; unknown ids fall back to the raw value). TypeScript type of `group_by` in `lib/api.ts` gets the new member.

## 5. Estimate split

`store.EstimateBuildTurn` today returns one `BuildEstimate` over completed build turns of the configured provider and model, excluding planning calls. Change:

- `EstimateBuildTurn` runs the same `per_turn` CTE with `t.milestone` in the select and groups the final aggregate with `FILTER (WHERE t.milestone = 1)` and `FILTER (WHERE t.milestone > 1)` for the counts and the two percentiles, in the same single query. A turn with `milestone IS NULL` (a build with no brief) counts in the single distribution only.
- `BuildEstimate` gains `First` and `Later *EstimateRange` (tokens low/high, cost low/high, sample and priced counts), each filled only when its group has `>= EstimateMinSample` turns (A7). The existing fields stay as the single distribution, so callers that read them keep working.
- `PlanEstimate` in `builder_planning.go`: when both groups are filled, `plan = first + (milestones - 1) * later`, and `basis = "split"`; otherwise it is what it is today and `basis = "single"`. `milestone` (the range of the next milestone) uses `First` when the brief has no done milestone and `Later` otherwise, when filled. `BuilderPlanEstimate` gains `Basis string \`json:"basis"\``. Cost ranges stay nil when the priced sample of a group is under 10.
- Percentiles of a sum are not the sum of percentiles; the split plan range is therefore a rough band, which the existing UI already words as "estimated". No claim of a confidence interval.
- Frontend: the estimate text does not change; `basis` is exposed for a later wording and is not shown in v1.

Known limit: on the 2026-11-14 local ledger every real build turn is `milestone = 1`, so the split cannot be exercised with real data until milestone-2 turns accumulate. The tests seed both groups.

## 6. Planning eval (opt-in)

A Go test file `internal/agent/planning_eval_test.go` with `//go:build planningeval`, in the style of `provider/contract_real_test.go`:

- Env: `PLANEVAL_PROVIDERS` (`<name>:<model>` entries, same grammar as `CONTRACT_PROVIDERS`), `PLANEVAL_KEY_<NAME>` for keys. Empty list skips. Keys are never printed.
- It builds the real provider for each entry and runs, for each of 12 briefs in `internal/agent/testdata/planeval/briefs.json`, one plan-mode turn through the agent's own entry point against an in-memory store and a fake sandbox (the planning turn needs no workspace; **[verify]** that the loop can be driven with the fake sandbox and the planning tool set only). Questions are answered by a fixed script (first option of each question) so the two models see the same answers.
- Per run it prints `PLANEVAL_RESULT provider=<p> model=<m> brief=<id> outcome=question|plan|text|error rounds=<n> milestones=<n> calls=<n> tokens=<n> cost_micros=<n> plan_ok=true|false note="..."`, and a totals line per model.
- `plan_ok` is `planning.ParsePlan` success plus the bounds of the planning package (`MaxPlanRounds`, milestone count). `outcome=text` is the failure the loop's directive tries to prevent.
- It writes the plan text of each run, with the model name removed and the order shuffled by a fixed seed, to `PLANEVAL_OUT` (a directory outside the repo) for the blind read (A4). The key from run to file name is printed once, separately.
- The 12 briefs: 4 vague, 4 complete, 2 with an existing backend schema, 2 in English; none contains real personal data (made-up domains only).
- Cost: bounded by a per-run call cap and a total cost cap read from the catalog before the run; it stops and prints what it has when the cap is reached.

Why not extend the parent's T42 eval set: it does not exist yet, and this one is small and specific to the planning step. If T42 later covers it, this file is replaced.

## 7. Tests

| Area | Test |
| --- | --- |
| Setting | empty is valid; catalog model valid; model of another provider refused; provider change clears an invalid `planning_model`; deprecated saved model stays valid; omitted key leaves it unchanged; explicit `""` clears it |
| Runtime | with `PlanningProvider` set, plan-mode calls use it and build-mode calls do not; ledger rows carry the model used; nil `PlanningProvider` changes nothing; a failing `resolvePlanningProvider` yields the build model and a warning; the turn that goes from plan to build switches back |
| Report | `group_by=purpose` returns one row per purpose, totals match; non-superadmin sees only their rows; 400 message lists `purpose` |
| Estimate | both groups filled → split range and `basis=split`; one group short → `basis=single` and the old numbers; `milestone IS NULL` turns only in the single group; cost nil under the priced minimum |
| Frontend | `tsc -b`, build, i18n JSON valid; grouping option present |
| Eval | `go vet -tags planningeval ./internal/agent/` compiles it; it is not run in CI |

## 8. Risks

| Risk | Mitigation |
| --- | --- |
| A cheap planner asks worse questions or writes weaker plans, and the user pays in rework | Off by default (A2); the eval and the blind read gate it (US-2, A4); the setting is reversible at once |
| The saving is smaller than the sample suggests (apps of other sizes, a different mix of planning and chat) | The by-purpose report (US-3) shows the real share on each installation; the 25% figure is from one small sample and is stated as such |
| Haiku 5.5 may not cache short prefixes, so planning calls pay full input | Its input price is 20x lower, so a full-price planning call is still far below a cached Sonnet call. **[verify in E09]** with the ledger |
| Prompt-size price tiers (Haiku above 100,000 tokens costs 5x) hit a long planning history | Planning histories are short; the ledger follows the catalog tiers either way (spec edge case) |
| Two models in one turn make a bug harder to read in the ledger | Each row already records provider and model; the report groups by model and by purpose |
| A smaller context window on the planning model | §3 context window item |
| DDL on `builder_settings` | One idempotent `ADD COLUMN IF NOT EXISTS` with a default; same pattern as `planning_mode`; reviewed as higher-risk |
