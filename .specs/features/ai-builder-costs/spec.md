# AI Builder Costs Specification

> Status: **Draft for review (2026-11-13).** Child of `.specs/features/ai-frontend-builder/`. Read its `spec.md` first. This feature adds money to the usage accounting that today only counts tokens, adds the Anthropic API as a provider, and shows usage and cost per reply, per task, per session, per user and per month. Token-saving techniques (prompt layout for caching, model tiers, schema-driven generation, cost estimates before a run) are a separate, later spec (`ai-builder-economy`); this one only makes the spend measurable and visible.

> **Superseded in part (2026-11-13)** by `.specs/features/ai-model-catalog/`: OpenAI and Anthropic prices now ship with Orbit (Goal 2 and the Out of Scope row on a bundled price list no longer hold), and costs are always in USD (Assumption 2 and the currency conversion row).

## Problem Statement

The builder records input and output tokens per LLM call (`zeep_system.ai_usage_ledger`) and enforces two token caps (per session, per user per month). Nobody can see what a task cost: there is no price data, no cost column, no usage screen, and the chat shows no usage at all. Cached input tokens, which providers bill at a different rate, are not recorded, so even a correct price table would give a wrong cost. The only provider kinds are `openai` and `openai_compatible`; the Anthropic API needs its own adapter.

An operator who self-hosts Orbit pays the provider directly. To trust the builder with a team, they need to answer "what did this app cost to build", "who spent what this month" and "which model is cheaper for our work" from data in their own installation.

## Goals

- [ ] Every LLM call records input, cached input, cache-write and output tokens, and the cost at the price valid at that moment.
- [ ] The operator maintains a price table per provider and model. Orbit ships no hardcoded prices and never fetches prices from the network.
- [ ] A user sees tokens and cost on every agent reply, a receipt per task (turn), and the running total of the session.
- [ ] A superadmin sees usage and cost by user, model, session and month, and can export it.
- [ ] Budgets can be set in money as well as in tokens, with a warning before the cap is reached.
- [ ] The Anthropic API works as a builder provider, with the same loop, tools and error handling as OpenAI.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Token-saving techniques (cache-friendly prompt layout, model tiers, diff edits, schema-driven generation, pre-run cost estimate, economy modes) | Separate spec `ai-builder-economy`. It depends on the measurements this spec adds. |
| Automatic price updates or a bundled price list | Prices change and differ by contract. A wrong built-in price is worse than an empty table. The operator types them. |
| Currency conversion | One currency per installation, chosen by the operator. Prices are typed in that currency. |
| Billing, invoices, charging end users, Stripe | Orbit is self-hosted; the operator pays the provider. This is reporting, not billing. |
| Cost of "Build with AI" / "Edit with AI" drawers (`ai_build_sessions`) | They do not write to the ledger today. Wiring them is P3 (ABC-21). |
| Retroactive cost for ledger rows written before this feature | No price snapshot exists for them. They keep `cost_micros = NULL` and show as "no price". |
| Streaming replies | Belongs to the experience spec. The Anthropic adapter is written non-streaming, like the OpenAI one. |
| Using a Claude subscription instead of an API key | Decided against on 2026-11-13 (provider terms). API keys only. |

---

## Assumptions & Open Questions

| # | Decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- | --- |
| 1 | Unit of stored cost | Integer **micros** (1 unit of currency = 1 000 000 micros) in `BIGINT`. Prices are `NUMERIC(18,6)` per million tokens. | No floating point in money. A single call of a few tokens at a low price still rounds to a non-zero integer in most cases; the rounding rule is fixed (half up) and tested. | No |
| 2 | Currency | One ISO 4217 code per installation in `builder_settings.cost_currency`, default `USD`. Changing it does not convert stored costs; the UI warns about that. | Conversion needs rates and a date policy. Providers bill in USD; an operator who thinks in BRL can type converted prices and set `BRL`. | No |
| 3 | Missing price | Cost is `NULL`, shown as "no price set" with a link to the price table. It is never shown as zero and never blocks the call. A money cap ignores calls with no price; the settings screen warns when a money cap is set and the active model has no price. | Blocking on a missing price would break working installations on upgrade. Zero would be a lie. | No |
| 4 | Token caps | Kept and still mandatory (`0 = blocked`, as today). Money caps are additional and optional (`0 = no money cap`). | Token caps work without a price table and protect an operator who never configures prices. | No |
| 5 | What "input tokens" means in the ledger | `input_tokens` = input billed at the full rate (not read from cache, not written to cache). `cached_input_tokens` = read from cache. `cache_write_tokens` = written to cache. Session counters (`tokens_in`) and token caps count the sum of the three. | OpenAI reports cached tokens inside `prompt_tokens`; Anthropic reports them outside `input_tokens`. Normalizing at the adapter keeps one cost formula. **To verify against each provider's current API reference during C02 and C06.** | No |
| 6 | Who sees what | A user sees their own sessions' usage. A superadmin sees everyone's. Usage rows never contain prompt or reply text. | Same visibility as sessions today. Per-user spend is personal data about an employee's activity; it is limited to the superadmin. | No |
| 7 | Deleted sessions | The ledger has no FK to sessions (by design, AFB-14). Reports show such rows as "deleted session" and still count them. | Deleting a session must not erase spend. | No |
| 8 | Anthropic adapter and prompt caching | The adapter reads cache usage fields but did not add cache breakpoints. **Superseded 2026-11-14:** the loop's calls now ask for the request-level breakpoint (see design.md, Risks); the rest of `ai-builder-economy` (model tiers, compaction tuning) is still open. | Keeps this spec about measurement. | No |
| 9 | Price changes | A price row has no history. The cost is fixed on the ledger row when the call is recorded, so later edits do not change past costs. The price table shows `updated_at` and `updated_by`, and every change is audited. | A price history table adds little once the cost is snapshotted. | No |
| 10 | Warning threshold | One `budget_warning` event per session and cap kind when usage crosses 80% of a cap. Fixed at 80% in v1. | Simple, and enough to avoid a surprise stop. | No |

---

## User Stories

### P1: Record what a call really cost ⭐ MVP

**US-1**: As an operator, I want every LLM call recorded with its real token classes and cost so that reports are correct.

**Acceptance Criteria**

1. ABC-01 The provider layer SHALL report, per call, input tokens billed at the full rate, cached input tokens, cache-write tokens and output tokens. An adapter that receives no cache information SHALL report zero for the cache fields.
2. ABC-02 WHEN a call is recorded THEN the system SHALL compute its cost from the price row of that provider and model and store it on the ledger row in the same transaction as the token counters. The cost SHALL NOT change when the price is edited later.
3. ABC-03 WHEN no price row exists for the provider and model THEN the cost SHALL be stored as `NULL`, the call SHALL proceed, and reports SHALL show "no price set" for it, never zero.
4. ABC-04 Each ledger row SHALL record what the call was for (`chat` or `compaction`), so summaries can show how much was spent on history compaction.
5. ABC-05 Session counters and the existing token caps SHALL keep counting all input tokens (full rate, cached and cache-write) plus output tokens, so no existing cap becomes looser.

### P1: Price table ⭐ MVP

**US-2**: As a superadmin, I want to set the price of each model so that Orbit can turn tokens into money.

**Acceptance Criteria**

1. ABC-06 A superadmin SHALL be able to list, create, update and delete price rows keyed by provider kind and model, with four prices per million tokens: input, cached input, cache write, output. Cached input and cache write are optional; WHEN absent THEN they SHALL be billed at the input price.
2. ABC-07 Prices SHALL be validated: not negative, at most 6 decimal places, model name not empty. Errors are returned in English.
3. ABC-08 The installation SHALL have one cost currency (ISO 4217 code), editable by a superadmin. The UI SHALL warn that changing it does not convert stored costs.
4. ABC-09 Every price or currency change SHALL be written to the audit log with the actor and the old and new values. Prices are not secrets.
5. ABC-10 The price screen SHALL list the models that appear in the ledger in the last 30 days and have no price, so the operator sees what is missing.

### P1: See usage where the work happens ⭐ MVP

**US-3**: As a builder user, I want to see what each reply and each task cost so that I can decide how to ask for the next change.

**Acceptance Criteria**

1. ABC-11 Each finished turn in the chat SHALL show its total tokens and cost. Expanding it SHALL show the receipt: number of model calls, tokens by class, cost, duration, files changed, builds run and the model used.
2. ABC-12 The session header SHALL show the session's total tokens and cost, and, when a cap applies, how much of it is used.
3. ABC-13 WHEN part of a turn or session has no price THEN the UI SHALL show the cost it knows followed by a marker that some calls have no price; it SHALL NOT present a partial sum as the full cost.
4. ABC-14 The usage shown to a user SHALL come from the API, computed from the ledger; the frontend SHALL NOT compute costs from prices.

### P1: Anthropic API provider ⭐ MVP

**US-4**: As a superadmin, I want to use the Anthropic API as the builder provider.

**Acceptance Criteria**

1. ABC-15 The provider kind `anthropic` SHALL be configurable like the others (model, API key encrypted at rest, enabled) and selectable as the builder provider.
2. ABC-16 The adapter SHALL implement the same contract as the OpenAI adapter: system prompt, multi-turn history with tool calls and tool results, tool definitions, output token cap, and the typed errors (`ErrAuth`, `ErrContextTooLong`, `*ErrRateLimited`, `*ErrProvider`). It SHALL pass the shared provider contract tests.
3. ABC-17 A conversation persisted while one provider was active SHALL resume after the operator switches to the other provider, because the stored history is provider-neutral.
4. ABC-18 The API key SHALL never appear in logs, events, errors returned to the client or the audit log.

### P2: Money budgets and reports

**US-5**: As a superadmin, I want caps in money and a usage report.

**Acceptance Criteria**

1. ABC-19 A superadmin SHALL be able to set a session cost cap and a per-user monthly cost cap (`0` = no money cap). WHEN recorded cost reaches a cap THEN the next call SHALL be refused with the same turn outcome as the token caps (`budget_exceeded`), with a reason that tells the caps apart.
2. ABC-20 WHEN usage crosses 80% of any cap (token or money) THEN the system SHALL append one `budget_warning` event for that session and cap kind, and the UI SHALL show it.
3. ABC-22 A superadmin SHALL see a usage report for a date range, grouped by day, user, model or session, with tokens by class, cost, and the share spent on compaction. A user SHALL see the same report limited to their own usage.
4. ABC-23 The report SHALL be exportable as CSV. The export SHALL contain no prompt or reply text.

### P3

1. ABC-21 Record usage of the "Build with AI" and "Edit with AI" drawers in the same ledger with their own `feature` value.
2. ABC-24 Per-app (backend app) totals across sessions on the app details page.

---

## Edge Cases

- A provider returns no usage object (some OpenAI-compatible servers): tokens are recorded as zero and the row is flagged so reports can say "usage not reported by the provider". **[verify]** what the adapter does today in that case.
- A price is edited between two calls of the same turn: each call keeps the price it was recorded with.
- Currency changed with existing costs: old rows keep their numbers; the report header states the current currency and the date of the last currency change.
- The monthly money cap is reached in the middle of a turn: the call that crosses it is the last one, as with token caps.
- Local models (Ollama through `openai_compatible`) with price zero: a price row with zeros gives a real cost of 0, which is different from "no price".
- Very small calls: cost below one micro rounds half up; the sum of rounded rows is what reports show (no re-computation from token totals), so the report always equals the sum of the receipts.

---

## Requirement Traceability

| Requirement | Story | Tasks |
| --- | --- | --- |
| ABC-01, ABC-05 | US-1 | C02 |
| ABC-02, ABC-03, ABC-04 | US-1 | C01, C04 |
| ABC-06 to ABC-10 | US-2 | C01, C03, C09 |
| ABC-11 to ABC-14 | US-3 | C08, C10 |
| ABC-15 to ABC-18 | US-4 | C06, C07 |
| ABC-19, ABC-20 | US-5 | C05, C10 |
| ABC-22, ABC-23 | US-5 | C08, C11 |
| ABC-21, ABC-24 | P3 | not planned |

---

## Success Criteria

- For a session built with a priced model, the session total equals the sum of its turn receipts, and equals the sum of its ledger rows, to the micro.
- A cost computed by hand from one provider invoice line (tokens by class times the configured prices) matches the ledger row for that call.
- Switching the builder provider between OpenAI and Anthropic in the middle of a session works, and both parts of the session show their own model and cost.
- Upgrading an installation with existing ledger rows and no price table keeps the builder working, with costs shown as "no price set".
