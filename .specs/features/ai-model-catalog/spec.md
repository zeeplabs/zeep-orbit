# AI Model Catalog Specification

> Status: **Draft for review (2026-11-13).** Changes decisions of `.specs/features/ai-builder-costs/` (its Goal 2, Out of Scope rows "Automatic price updates or a bundled price list" and "Currency conversion", and Assumption 2). Read that spec first. Orbit is moving to a SaaS offer operated by ZeepLabs, with self-hosting kept for later; the catalog is updated only by Orbit releases.

## Problem Statement

Today a superadmin types the model of each AI provider and types a price for every model in the **Model prices** card, in a currency the installation chooses. For `openai` and `anthropic` this is work every customer repeats with the same public data, and it fails in silent ways: a typo in a model ID, a missing price ("no price set" calls do not count toward the cost budgets), an old price, or a price typed in the wrong currency. The cost budgets protect money only as well as those numbers are right.

Commit `0ae2140` added a curated list of models and suggested list prices in the frontend (`internal/dashboard/ui/src/lib/aiModelCatalog.ts`), but the superadmin can still type any model and any price, and the backend does not know the list.

## Goals

- [ ] For `openai` and `anthropic`, the model is chosen from a catalog shipped with Orbit, and its price comes from that catalog. Nobody types or edits either.
- [ ] Costs and money budgets of the builder are always in USD, the currency both providers publish in.
- [ ] A model whose price depends on the prompt size (Claude Haiku 5.5 above 100,000 tokens) is priced correctly per call.
- [ ] `openai_compatible` keeps working as today: free model, operator-entered price.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Fetching the catalog from the network | Decided 2026-11-13: release only. The SaaS is deployed by ZeepLabs, so a release reaches it. Self-hosted staleness is accepted for now. |
| Price overrides or discounts per installation (negotiated contracts) | Decided 2026-11-13: prices are the providers' list prices and are not editable. |
| Currency conversion or a local display currency | Decided 2026-11-13: USD only. |
| Catalog for `openai_compatible` | Any server and any model (Ollama, vLLM, Azure, gateways); there is no public price to ship. |
| Region multipliers (Anthropic US-only inference 1.1x), batch discounts, OpenAI long context above 272K | Orbit does not use US-only inference or batches, and builder prompts are compacted well below 272K (`DefaultContextWindowTokens = 128000`). |
| Billing customers of the SaaS | A separate product decision. This spec is cost accounting of model calls. |
| Cost of "Build with AI" / "Edit with AI" drawers | Still not in the ledger (ai-builder-costs ABC-21). They only get the model validation of AMC-04. |

---

## Assumptions & Open Questions

| # | Decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | Where the catalog lives | Go package embedded in the binary (`internal/agent/catalog`), served to the dashboard by an API. The frontend list from `0ae2140` is removed. | One source of truth: the server prices calls and validates models, so it must own the list. | No |
| 2 | Update path | Orbit release only. | User decision, 2026-11-13. | **Yes** |
| 3 | Currency | USD, fixed. `builder_settings.cost_currency` stops being editable and is forced to `USD`. | User decision, 2026-11-13. | **Yes** |
| 4 | Installation that had another currency | On upgrade, `cost_currency` becomes `USD` and `cost_currency_changed_at` is set, which already excludes older turns from estimates. Money caps keep their numbers and the Costs card shows a one-time notice asking the superadmin to review them. Costs already stored are not converted. | Resetting caps to 0 would remove protection; keeping the numbers is looser (BRL 50 becomes USD 50) but not none, and the notice makes it visible. [Incerto] whether any installation runs with a currency other than USD. | No |
| 5 | Saved model that is not in the catalog (`o3-mini`, a typo, a model removed by a release) | The provider row keeps it. The builder reports itself unavailable with reason `model_not_in_catalog` until a superadmin picks a catalog model. The AI chat drawers keep working (they record no cost). | A priced, budgeted feature must not run a model it cannot price. Same pattern as `budget_not_configured`. | No |
| 6 | Removing a model from the catalog | A release does not remove a model the provider still serves; it marks it `deprecated` (still selectable for a saved row, shown with a label, not offered to new saves). | Avoids an upgrade that switches the builder off by surprise (decision 5). | No |
| 7 | Price changes | Each catalog price has an `effective_from` date; the price used for a call is the last one with `effective_from <= call time`. | Lets a release carry a known future change (a promotion ending, an announced increase). | No |
| 8 | Existing `ai_model_prices` rows for `openai` and `anthropic` | Ignored for pricing from the upgrade on and no longer shown; not deleted. | No DDL beyond what is needed; history already holds its cost. Deleting can be a later cleanup. | No |
| 9 | Prompt-size tiers | A catalog price may list tiers by prompt tokens (input + cached input + cache write of the call); the whole call is billed at the tier its prompt falls in. | How Anthropic bills Claude Haiku 5.5 (pricing page, 2026-11-13). | No |

---

## User Stories

### US-1: Pick a model from the catalog (P1) ⭐ MVP

**User Story**: As a superadmin, I want to pick the OpenAI or Anthropic model from a list Orbit knows, so that I cannot save a model ID Orbit cannot price.

**Acceptance Criteria**:

1. WHEN a superadmin opens the **AI** tab for `openai` or `anthropic` THEN the model field SHALL be a select with the catalog models of that provider that are not deprecated, and no free-text option.
2. WHEN `PUT /dashboard/api/ai-providers/{openai|anthropic}` carries a model that is not in the catalog THEN the server SHALL answer 400 with `model is not in the catalog` (English, AGENTS.md §4) and store nothing.
3. WHEN the saved model is not in the catalog THEN the AI tab SHALL show it as the current value with a notice that it must be replaced, and the builder SHALL report `model_not_in_catalog` (Assumption 5).
4. WHEN the saved model is deprecated THEN the select SHALL show it with a "deprecated" label and the save SHALL still be accepted while it stays selected.
5. WHEN the provider is `openai_compatible` THEN the model field SHALL stay free text, as today.

**Independent Test**: Save `gpt-4.1` for `openai` (accepted); send `o3-mini` by API (400); put `o3-mini` in the row by SQL and see the builder unavailable with `model_not_in_catalog`.

---

### US-2: Calls of catalog models are priced by the catalog (P1) ⭐ MVP

**User Story**: As a superadmin, I want every builder call of an OpenAI or Anthropic model to have a cost without entering prices, so that the reports and the money budgets are right from day one.

**Acceptance Criteria**:

1. WHEN a builder call of an `openai` or `anthropic` catalog model is recorded THEN its cost SHALL be computed from the catalog price valid at the call time and stored on the ledger row, as today.
2. WHEN the catalog price of a model has tiers THEN the call SHALL be billed at the tier its prompt tokens fall in (Assumption 9).
3. WHEN a call is of an `openai_compatible` model THEN its cost SHALL come from `ai_model_prices`, as today, and SHALL be "no price set" when there is no row.
4. WHEN `ai_model_prices` has a row for an `openai` or `anthropic` model THEN it SHALL NOT be used (Assumption 8).
5. WHEN a release changes a catalog price THEN calls recorded before it SHALL keep their stored cost.

**Independent Test**: Run one turn on `openai`/`gpt-4.1` with no price row and see a cost in the receipt equal to the catalog price times the tokens; run a Haiku 5.5 call with a prompt over 100,000 tokens (unit test) and see the higher tier.

---

### US-3: Prices and models are visible, not editable (P1) ⭐ MVP

**User Story**: As a superadmin, I want to see the model list and the price of each model, so that I know what the builder will cost without being able to change it.

**Acceptance Criteria**:

1. WHEN a superadmin opens the **Model prices** card THEN it SHALL list the catalog models of `openai` and `anthropic` with their current prices (input, cached input, cache write, output, and tiers when present), read-only, with the date the catalog was reviewed.
2. WHEN the card is open THEN the add, edit and delete actions SHALL exist only for `openai_compatible` rows.
3. WHEN the unpriced-models notice lists models THEN it SHALL list only `openai_compatible` models and saved models that are not in the catalog.
4. WHEN `PUT` or `DELETE /dashboard/api/builder/prices` targets `openai` or `anthropic` THEN the server SHALL answer 400 with `prices of catalog providers cannot be changed`.
5. WHEN any authenticated dashboard user calls `GET /dashboard/api/builder/catalog` THEN the server SHALL return the catalog (providers, models, deprecated flag, prices with `effective_from` and tiers, reviewed date, currency `USD`). It holds no secret, so the superadmin-only rule is not needed for reading it. [Provável]

**Independent Test**: Open the card as superadmin, see the catalog rows without edit buttons and a working add for `openai_compatible`; `PUT` a price for `openai` by API and get 400.

---

### US-4: Costs are in USD (P1) ⭐ MVP

**User Story**: As a superadmin, I want costs and money budgets in USD, so that they match what the providers bill.

**Acceptance Criteria**:

1. WHEN the Costs card is shown THEN the currency SHALL be displayed as USD and SHALL NOT be editable.
2. WHEN `PUT /dashboard/api/builder/settings` carries a `cost_currency` other than `USD` THEN the server SHALL answer 400 with `cost_currency must be USD`.
3. WHEN Orbit starts on an installation whose `cost_currency` is not `USD` THEN it SHALL set it to `USD`, set `cost_currency_changed_at`, keep the money cap numbers, and the Costs card SHALL show a notice to review the caps until a superadmin saves the card (Assumption 4).
4. WHEN costs are shown anywhere (chat receipt, usage report, CSV, plan estimate) THEN they SHALL be formatted as USD.

**Independent Test**: Set `cost_currency = 'BRL'` by SQL, restart, see `USD`, the notice, and the caps unchanged.

---

### US-5: Keep the catalog current (P2)

**User Story**: As a ZeepLabs maintainer, I want a documented, tested way to update the catalog, so that a release can change models and prices safely.

**Acceptance Criteria**:

1. WHEN the catalog is changed THEN a test SHALL fail on a duplicated model, a price with no `effective_from`, a negative or malformed price, tiers out of order, or a reviewed date older than the newest `effective_from`.
2. WHEN a maintainer updates the catalog THEN `RELEASE.md` SHALL list the step (check both providers' pricing pages, update the file, bump the reviewed date, add a CHANGELOG line).

**Independent Test**: Break the catalog file on purpose and see the test fail.

---

## Edge Cases

- WHEN the provider answers with a snapshot ID different from the configured model THEN pricing SHALL still use the configured model, which is what the ledger records today (`Provider.Model()`).
- WHEN a catalog model has no cached-input price (OpenAI "pro" models, not in the first catalog) THEN cached input SHALL be billed at the input price, as today.
- WHEN a call crosses a tier boundary because of cache writes THEN the tier SHALL be decided by the sum of input, cached input and cache write, matching the provider's rule.
- WHEN the catalog has a price with `effective_from` in the future THEN calls before that date SHALL use the previous price, and the card SHALL show the upcoming price and its date.
- WHEN the cost of a call does not fit the column THEN it SHALL be stored as "no price set", as today (`callCost`).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AMC-01 | US-1: catalog package with models, deprecated flag, prices, `effective_from`, tiers, reviewed date | Design | Pending |
| AMC-02 | US-3: `GET /dashboard/api/builder/catalog` | Design | Pending |
| AMC-03 | US-1: AI tab select from the API catalog, no free text for `openai`/`anthropic`; remove `lib/aiModelCatalog.ts` | Design | Pending |
| AMC-04 | US-1: `PUT /ai-providers/{openai,anthropic}` rejects non-catalog models | Design | Pending |
| AMC-05 | US-1: builder availability reason `model_not_in_catalog` | Design | Pending |
| AMC-06 | US-2: `callCost` uses the catalog for `openai`/`anthropic`, `ai_model_prices` for `openai_compatible` | Design | Pending |
| AMC-07 | US-2: tiered pricing in `CostMicros` | Design | Pending |
| AMC-08 | US-3: Model prices card read-only for catalog providers | Design | Pending |
| AMC-09 | US-3: prices API rejects writes for catalog providers; unpriced list limited | Design | Pending |
| AMC-10 | US-4: currency fixed to USD (API, UI, startup normalization, notice) | Design | Pending |
| AMC-11 | US-5: catalog validation test | Design | Pending |
| AMC-12 | US-5: `RELEASE.md` catalog step | Design | Pending |
| AMC-13 | All: docs (`ai-frontend-builder.md`), CHANGELOG, README feature row if needed | Design | Pending |

**Coverage:** 13 total, 0 mapped to tasks.

---

## Success Criteria

- [ ] A new installation with `openai` or `anthropic` configured shows a cost on the first builder turn with no price typed.
- [ ] No API path accepts a model or a price for `openai`/`anthropic` that is not in the catalog.
- [ ] Every money amount in the builder is in USD.
- [ ] Claude Haiku 5.5 calls above 100,000 prompt tokens are billed at the higher tier (unit test).
- [ ] Go gate (`go build`, `go test -p 1 -parallel 4 ./...`, `go vet`, `gofmt`) and frontend gate (`tsc -b`, `npm run build`) pass.
