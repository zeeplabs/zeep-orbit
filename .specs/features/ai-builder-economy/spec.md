# AI Builder Economy Specification

> Status: **Draft for review (2026-11-14).** Child of `.specs/features/ai-frontend-builder/`. Read `ai-builder-costs/spec.md` (measurement), `ai-model-catalog/spec.md` (prices) and `ai-builder-planning/spec.md` (planning mode) first. Those three left the token-saving work to "a separate, later spec `ai-builder-economy`"; this is that spec, **cut down to what the ledger shows is worth doing**. Prompt caching on the Anthropic adapter, which was on the original list, shipped on 2026-11-14 (`f44ac21`) and is not repeated here.

## Problem Statement

The builder records what every call costs, but nothing yet lowers it. A measurement of the local ledger on 2026-11-14 (Anthropic `claude-sonnet-5-5`, 99 calls, US$ 1.553, small to-do apps, so an order of magnitude and not a forecast) shows where the money goes:

| Part of the spend | US$ | Share |
| --- | --- | --- |
| Chat (build) calls | 1.168 | 75% |
| Planning calls (`purpose = planning`) | 0.385 | **25%** |
| Compaction calls | 0 | 0% (no compaction happened) |

Inside the chat calls: cache writes 35%, output 32%, fresh input 24%, cache reads 8%. Planning is a quarter of the spend, and it is the part where a cheap model is most likely good enough: it asks questions and writes a plan, it does not write code. Today it runs on the same model as the build. Besides that, the usage report cannot answer "how much of the spend is planning?" without reading the database, and the plan estimate treats the first build of an app and a later milestone as the same thing even though the first costs more.

## Goals

- [ ] An operator can choose a cheaper model for planning turns, inside the provider already selected for the builder. Left unset, nothing changes.
- [ ] The decision to turn it on rests on a repeatable comparison of the planning model against the build model, not on a guess.
- [ ] The usage report shows spend split by purpose (planning, chat, compaction).
- [ ] The plan estimate separates the first milestone of an app from the later ones when the history allows it.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Compaction tuning | The ledger has zero compaction calls, so there is nothing to tune and no evidence of a problem. Revisit when real sessions compact. |
| Replaying the model's thinking blocks across steps | A quality question, not a cost one. It needs an opaque per-provider field on the neutral `Message` and an append-only history (`ai-builder-costs/design.md` §5). |
| Diff edits, schema-driven generation | Output is 32% of the chat spend, but `edit_file` already exists and there is no measurement that a new technique saves anything. Needs its own eval first. |
| A cheaper model for the build turns | Build quality decides whether the app works. Not without a much larger eval. |
| A planning model from a different provider | One provider per installation today (`builder_settings.provider`). Crossing providers means two keys, two failure modes and two budgets. See Assumption A1. |
| Per-user or per-session choice of the planning model | One setting per installation, like the build model. |
| Economy modes ("cheap", "balanced", "best") | A bundle of settings with no data behind its presets. |
| Changing token caps or cost caps | Unchanged. The caps count the calls of both models together. |

---

## Assumptions & Open Questions

| # | Topic | Assumption | Why | Needs OK |
| --- | --- | --- | --- | --- |
| A1 | Which provider the planning model belongs to | **Same provider as the build model.** The setting holds a model id of the provider chosen in `builder_settings.provider`. | One key and one set of failure modes. The saving is in the model price, not in the vendor. | **Yes** (the owner has not answered) |
| A2 | Default | Empty, meaning "the same model as the build". Installing this feature changes no call. | Safe merge. The operator opts in after the eval. | No |
| A3 | Which model | Not fixed by this spec. The catalog offers `claude-haiku-5-5` for Anthropic; for OpenAI the operator picks a smaller catalog model. The eval (US-2) decides if a model is acceptable. | Quality is measured, not assumed. | No |
| A4 | Acceptance bar | The planning model passes when, on the same briefs, its structural pass rate is not lower than the build model's and a blind read of the plans by the owner finds no worse result in at least 10 of 12 briefs. | A starting bar, to be set by the owner before the run. | **Yes** (thresholds) |
| A5 | Failure of the planning model | If its provider cannot be built (key or model gone), the planning turn runs on the build model and the cause is logged. The turn does not fail because of a tier. | A cost optimization must not become a new way to stop the builder. | No |
| A6 | Prompt cache when the model changes inside a turn | A turn that plans on one model and then builds on another starts the build calls with a fresh cache. The tools list already differs between plan and build mode, so the planning cache was never reusable by the build. **[verify in E09]** with the ledger. | Avoid promising a saving that a cache miss eats. | No |
| A7 | Estimate split | "First milestone" is a build turn whose `milestone` is 1; "later" is `milestone > 1`. With fewer than 10 turns in a group the old single distribution is used for that group. | Same minimum sample as today. No new DDL. | No |

---

## User Stories

### US-1: Choose the planning model (P1) ⭐ MVP

**User Story**: As a superadmin, I want plan-mode turns to run on a cheaper model of the same provider so that the quarter of the spend that goes to planning drops.

**Why P1**: It is the one lever with a measured size (up to about 24% of the total on the sample).

**Acceptance Criteria**:

1. WHEN `planning_model` is empty THEN every call of every turn SHALL use the provider row's model, exactly as today.
2. WHEN `planning_model` names a model THEN calls of a plan-mode turn SHALL use it and calls of a build-mode turn SHALL use the provider row's model. The ledger row records the model actually used, so each call is priced by the catalog.
3. WHEN the provider is `openai` or `anthropic` THEN the setting SHALL accept only a catalog model of that provider (same rule as the build model, including "a deprecated model the setting already holds stays valid"). The error is in English.
4. WHEN the provider of the setting is changed THEN a `planning_model` that does not belong to the new provider SHALL be refused or cleared, never kept silently.
5. WHEN the planning provider cannot be built THEN the planning turn SHALL fall back to the build model and log the cause (A5).
6. WHEN a plan is approved and the same turn goes on to build THEN the build calls SHALL use the build model.

**Independent Test**: Set `planning_model` to a cheaper catalog model, send a vague request, approve the plan; the ledger shows `purpose = planning` rows on the cheap model and `purpose = chat` rows on the build model.

---

### US-2: Know the cheap model is good enough (P1) ⭐ MVP

**User Story**: As the owner, I want a repeatable comparison of the planning model against the build model so that I enable it with evidence.

**Why P1**: A cheaper planner that asks bad questions or writes weak plans costs more in rework than it saves.

**Acceptance Criteria**:

1. WHEN the comparison runs THEN it SHALL send the same fixed list of 12 briefs (vague, complete, pt-BR and English, with and without an existing backend) to each model through the real plan-mode turn, and record per brief: whether the turn ended in `ask_user` or `propose_build_plan` (not plain text, not a failure), whether the plan parses and respects the milestone and question bounds, calls, tokens and cost.
2. WHEN it ends THEN it SHALL print one machine-readable line per model and brief, and the totals, ready to paste into `design.md`.
3. The comparison SHALL be opt-in (build tag and environment keys, like `providercontract`), never part of the normal `go test`, because it spends real tokens.
4. The blind read of the plans (A4) SHALL use plan text with the model name removed.

**Independent Test**: Run it for two models; the output has 24 lines and the totals.

---

### US-3: See planning apart from the rest (P1) ⭐ MVP

**User Story**: As a superadmin, I want the usage report grouped by purpose so that I can see what planning costs against building.

**Why P1**: Without it the benefit of US-1 cannot be seen except by querying the database.

**Acceptance Criteria**:

1. WHEN `GET /dashboard/api/builder/usage?group_by=purpose` is called THEN the response SHALL have one row per purpose present in the range (`chat`, `planning`, `compaction`) with the same columns as the other groupings, ordered by cost.
2. WHEN a user is not a superadmin THEN the report SHALL show only their own rows, as the other groupings do.
3. WHEN `group_by` is not one of the known values THEN the 400 message SHALL list `purpose` too.
4. The usage page SHALL offer the grouping, with the purpose names translated in `en` and `pt-BR`.

**Independent Test**: With planning and chat rows in the ledger, the grouping returns two rows whose costs add up to the totals.

---

### US-4: A better estimate for the first build (P2)

**User Story**: As a user, I want the plan estimate to count the first milestone apart from the later ones so that a plan of several milestones is not under-estimated.

**Why P2**: The estimate already works; this refines it. Measured first-build cost is higher than later ones in the design notes of `ai-builder-planning` (design §9), but with the data of one installation the split may not have enough turns.

**Acceptance Criteria**:

1. WHEN there are at least 10 completed build turns with `milestone = 1` AND at least 10 with `milestone > 1` on the configured provider and model THEN the plan range SHALL be the first-milestone range plus (number of milestones - 1) times the later range.
2. WHEN either group has fewer than 10 turns THEN the response SHALL be what it is today (one range times the number of milestones).
3. The response SHALL say which of the two it used, so the UI can word it (`basis`: `split` or `single`).
4. The cost range stays nil when the priced sample is under 10, as today.

**Independent Test**: Seed 10 turns of each group with different costs; the plan range uses both. Seed 10 and 3; it falls back.

---

## Edge Cases

- WHEN the setting names a model that later leaves the catalog THEN the planning turn SHALL fall back to the build model (A5) and the status page SHALL NOT report `model_not_in_catalog` for it; only the build model gates the builder.
- WHEN the planning model is the same id as the build model THEN nothing differs, and the setting stays valid.
- WHEN a planning turn is resumed after a restart THEN it SHALL pick the model again from the current setting, not from the first call.
- WHEN a plan-mode turn is rejected and revised THEN the revision SHALL run on the planning model.
- WHEN the planning model has a smaller context window than the history THEN the existing context-too-long path applies. [verify] the window comes from the catalog or from `context_window_tokens`, and that a planning turn never uses a window larger than the planning model supports.
- WHEN a model is priced by prompt size (Haiku 5.5 above 100,000 tokens) THEN the ledger cost follows the catalog tiers as it does today.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AEC-01 | US-1: empty setting changes nothing | Design | Pending |
| AEC-02 | US-1: plan-mode calls on the planning model, build calls on the build model | Design | Pending |
| AEC-03 | US-1: catalog validation of the setting | Design | Pending |
| AEC-04 | US-1: setting cleared or refused when the provider changes | Design | Pending |
| AEC-05 | US-1: fallback to the build model, with a log | Design | Pending |
| AEC-06 | US-2: opt-in comparison over 12 fixed briefs | Design | Pending |
| AEC-07 | US-2: machine-readable output, blind plan text | Design | Pending |
| AEC-08 | US-3: `group_by=purpose` in the report | Design | Pending |
| AEC-09 | US-3: usage page grouping and translations | Design | Pending |
| AEC-10 | US-4: split estimate with fallback and `basis` | Design | Pending |

**Coverage:** 10 total, 10 mapped to tasks (see `tasks.md`).

---

## Success Criteria

- [ ] With `planning_model` empty, the ledger of a full turn is the same as before this feature (same models, same purposes).
- [ ] With a cheaper model set, planning calls of the local check cost at least 80% less per call than on the build model, and the plan card is as good in the owner's blind read (A4).
- [ ] The report answers "what share of the spend is planning?" in one request.
- [ ] No test of the normal run needs a key or spends tokens.
