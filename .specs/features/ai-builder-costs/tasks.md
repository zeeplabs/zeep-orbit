# AI Builder Costs Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. If the skill cannot be activated, stop and tell the user.

Commits only when the user asks (AGENTS.md §2); one commit per task when authorized; never add Claude/Anthropic attribution; never force push. **C01 adds DDL to `provisioner.go` (higher-risk, AGENTS.md §8): show the DDL diff and get confirmation before applying.** Tests and measurements run locally or on ZeepLabs-controlled infra, never on Starbem infra. Provider API keys are typed by the owner in the settings screen or exported in their own shell; they are never written to the repository, to a spec, to a log or to a memory note.

---

**Spec**: `.specs/features/ai-builder-costs/spec.md`
**Design**: `.specs/features/ai-builder-costs/design.md`
**Parent**: `.specs/features/ai-frontend-builder/`
**Status**: Approved by the maintainer on 2026-11-13, DDL of C01 included. In execution on `feat/ai-frontend-builder`. Done: C01–C09. Pending: C10–C13.

---

## Test Coverage Matrix

| Code Layer | Required Test Type | Location Pattern | Run Command | Parallel-Safe |
| --- | --- | --- | --- | --- |
| Cost function, adapters (`httptest`), loop with fakes | unit | `*_test.go` next to code | `go test ./internal/agent/...` | Yes |
| Stores and handlers (real Postgres) | integration | `internal/agent/store/*_test.go`, `internal/dashboard/builder_*_test.go` | `go test -p 1 -parallel 4 -timeout 25m ./...` | No (shared DB) |
| DDL in `provisioner.go` | none | exercised by C01/C04 tests | build gate | — |
| Real provider endpoints | opt-in | `internal/agent/provider/contract_real_test.go` | run by the owner with keys in the environment | — |
| Frontend | none (no framework) | `src/**` | frontend gate | — |

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 25m ./...` |
| Frontend | UI tasks | `cd internal/dashboard/ui && npx tsc -b && npm run build` plus i18n JSON validation for `en.json` and `pt-BR.json` |

---

## Dependency Graph

```
C01 DDL ──► C03 price store + API ──► C04 cost in RecordUsage ──► C05 money caps + warning
   │                 │                         │                          │
   │                 └──► C09 price/currency UI│                          │
C02 Usage classes ───────────────────────────► ┘                          │
   │                                           └──► C08 usage API ──► C10 chat + header UI ──► C11 usage page
   └──► C06 Anthropic adapter ──► C07 provider kind wiring + settings UI
C12 docs + CHANGELOG (last)        C13 real-endpoint run (owner, needs keys)
```

---

## Tasks

### C01 — DDL  `[higher-risk: confirm DDL first]`
- **What**: `ai_model_prices`; ledger columns (`cached_input_tokens`, `cache_write_tokens`, `purpose`, `cost_micros`, `usage_reported`) and indexes; `builder_settings` currency and money caps. Decide here between session counters and reading totals from the ledger (design §2 note): measure the indexed query on a seeded ledger and pick; record the choice in the design.
- **Where**: `internal/dashboard/provisioner.go`.
- **Done when**: DDL diff shown and confirmed; idempotent re-run on a database that already has the old tables; old rows read with the defaults; **[verify]** every `SELECT` on the altered tables uses explicit column lists.
- **Requirements**: ABC-02, ABC-03, ABC-04, ABC-06, ABC-08, ABC-19. **Tests**: integration (provision twice). **Gate**: Full.
- **Status**: Done 2026-11-13. DDL confirmed by the maintainer. Applied twice on a local database that already had the old tables (15 existing ledger rows read back with the defaults and `cost_micros` NULL). Totals are read from the ledger, no session counters (measurement in design §2). No `SELECT *` on the altered tables. Full gate result recorded in the commit.

### C02 — Token classes in the provider layer  [P]
- **What**: extend `provider.Usage` (design §3) with `Total()`; OpenAI adapter maps cached tokens and sets `Reported`; replace hand-written `in + out` sums in the loop, compaction and store with the helper; **[verify]** the OpenAI usage field names in the current API reference.
- **Where**: `internal/agent/provider/provider.go`, `openai.go`, `internal/agent/loop.go`, `compaction.go`.
- **Done when**: unit tests for usage with details, without details, and with no usage object; existing contract tests green; cap behavior unchanged (a test asserts cached tokens count toward the token cap).
- **Requirements**: ABC-01, ABC-05. **Tests**: unit. **Gate**: Quick.
- **Status**: Done 2026-11-13. `Usage` has the four classes, `Reported`, `TotalInput()` and `Total()`. The OpenAI adapter reads `usage.prompt_tokens_details.cached_tokens` (field name confirmed in OpenAI's prompt caching guide) and subtracts it from `prompt_tokens`; that `prompt_tokens` includes the cached part is consistent with the documented example payload but is still to be checked against a real call in C13. The loop and compaction record `TotalInput()` so the token caps are unchanged until C04 stores the classes. Full gate green.

### C03 — Price store, cost function and price API
- **What**: pure cost function with half-up rounding (design §3); price CRUD store; handlers and routes for `GET/PUT/DELETE prices`; currency in builder settings (partial update, validated as a 3-letter code); audit for price and currency changes; `unpriced_models`.
- **Where**: new `internal/agent/store/cost.go`, new `internal/dashboard/builder_prices.go` (+ store), `builder_settings*.go`, route registration.
- **Done when**: table tests for the cost function (each class, fallbacks, zero price, rounding boundary, overflow guard); integration tests for RBAC (superadmin only), validation errors in English, audit rows with old and new values, model names containing `/`.
- **Requirements**: ABC-06 to ABC-10. **Depends**: C01. **Tests**: unit + integration. **Gate**: Full.
- **Status**: Done 2026-11-13. `internal/agent/store/cost.go` (`ParsePrice`, `FormatPrice`, `CostMicros`, integer arithmetic with one half-up rounding per call), `internal/dashboard/builder_prices.go` (store, handlers, audit, unpriced models), routes `GET/PUT/DELETE /api/builder/prices`. The settings store also gained the money cap fields here (same SQL statement as the currency), so C05 only has to enforce them. The price key is in the body/query, not the path (design §6). Price rows are accepted only for the provider kinds the builder knows; `anthropic` joins in C07.

### C04 — Cost on every recorded call
- **What**: `UsageRecord` carries the token classes, `Purpose` and `Reported`; `RecordUsage` looks up the price in its transaction, stores `cost_micros` or `NULL`, updates session totals (or nothing, per C01's choice); loop passes `chat`, compaction passes `compaction`.
- **Where**: `internal/agent/store/usage.go`, `loop.go`, `compaction.go`, fakes.
- **Done when**: integration tests: priced call, unpriced call, zero-price call (cost 0, not NULL), price edited after the call leaves the row unchanged, session total equals the ledger sum; loop tests with fakes still green.
- **Requirements**: ABC-02, ABC-03, ABC-04. **Depends**: C02, C03. **Tests**: integration + unit. **Gate**: Full.
- **Status**: Done 2026-11-13. `UsageRecord` carries the three input classes, `Purpose` (`chat`, `compaction`; validated in Go) and `Reported`. `RecordUsage` reads the price of `(provider, model)` in its transaction and stores `cost_micros`: NULL with no price row, 0 with a zero price. `input_tokens` in the ledger is now only the full-rate input; `builder_sessions.tokens_in` and the monthly token cap sum all three input classes, so the token caps behave as before. A cost that does not fit the column is stored as NULL instead of failing a call whose tokens are already spent. Full gate green (27 packages).

### C05 — Money caps and the 80% warning
- **What**: settings fields with partial-update semantics; `BudgetChecker` money caps and new errors; warning result and the `budget_warning` event, once per session and cap; event whitelisted in the events API.
- **Where**: `internal/agent/store/usage.go`, `loop.go`, `limits.go` (event constant), `builder_settings*.go`, `builder_common.go`.
- **Done when**: tests: money cap 0 means no cap; cap reached ends the turn `budget_exceeded` with a distinguishable reason; unpriced calls do not count; warning emitted once at 80% for each cap kind and not again after a resume; token-cap tests unchanged.
- **Requirements**: ABC-19, ABC-20. **Depends**: C04. **Tests**: unit (loop) + integration. **Gate**: Full.
- **Status**: Done 2026-11-13. `BudgetChecker.Check` returns `([]BudgetWarning, error)`: money caps (`0` = none) with `ErrSessionCostCapReached` / `ErrMonthlyCostCapReached`; unpriced calls have no cost and do not count. `store.BudgetReason` names the cap, and the `budget_exceeded` event now carries `{reason}` (`session_tokens`, `month_tokens`, `session_cost`, `month_cost`, `not_configured`). Warning at 80% (integer comparison): the once-per-session-and-cap rule lives in the store (`AppendBudgetWarning`, insert guarded by `NOT EXISTS` on the session's events), and the loop keeps a per-run set so the store is asked once per cap and turn; this resolves the design §4 **[verify]**. `budget_warning` is whitelisted with `cap`, `used`, `limit` only. The settings fields were already done in C03. Full gate green (27 packages).

### C06 — Anthropic adapter  [P]
- **What**: `anthropic.go` per design §5. Before coding, read the current Messages API reference through the `claude-api` skill and resolve every **[verify]** in design §5; record what was confirmed.
- **Where**: `internal/agent/provider/anthropic.go`, `anthropic_test.go`, `contract_test.go`.
- **Done when**: `httptest` tests for request mapping (system, merged tool results, invalid tool arguments, `max_tokens` default), response mapping (text, tool calls, stop reasons, usage with and without cache fields), error mapping (auth, rate limit with `Retry-After`, context too long, generic), response size guard; the shared contract table passes for the new adapter; cross-provider history replay both ways; no key or header in any error string (asserted).
- **Requirements**: ABC-16, ABC-17, ABC-18. **Depends**: C02. **Tests**: unit. **Gate**: Quick.
- **Status**: Done 2026-11-13. `internal/agent/provider/anthropic.go` (stdlib HTTP, no SDK), `anthropic_test.go`, and a Messages-format fake in `contract_test.go` that enforces role alternation and tool_use/tool_result pairing; the shared contract table passes for the adapter and `CONTRACT_PROVIDERS` accepts `anthropic:<model>`. What was confirmed and what stays to check with a real key is in design §5 ("Resolved in C06"). Not confirmed: the wording of the prompt-too-long error and the effect of the `max_tokens` floor; both go to C13. Quick gate green.

### C07 — Provider kind `anthropic` in settings and runtime
- **What**: accept the kind in `ai_providers` and `builder_settings.provider`; runtime builds the adapter; settings UI lists it; i18n.
- **Where**: `ai_provider_handlers.go`, `builder_settings.go`, `builder_runtime.go`, `AIBuilderSettings.tsx`, `api.ts`, locales.
- **Done when**: integration tests for saving and selecting the kind and for an unknown kind (400); the base-URL field is not offered for this kind; frontend gate; mutation has `onError`.
- **Requirements**: ABC-15. **Depends**: C06. **Tests**: integration. **Gate**: Full + Frontend.
- **Status**: Done 2026-11-13. `anthropic` is accepted by the provider API (no base URL; key required at runtime), by `builder_settings.provider` and by the price table; `BuilderRuntime.resolveProvider` builds the Anthropic adapter with its own HTTP client (fixed official endpoint, 10 minute timeout). UI: an **Anthropic** tab in Integrations → AI (free-text model, no base URL field) in place of the disabled Claude placeholder, and the kind in the AI Builder provider selector. The legacy `claude` name stays as an unimplemented placeholder in the API. Checked in a browser on the local stack (form renders, no base URL field, no console or API errors); nothing was saved there. CHANGELOG entry added. Full and frontend gates green.

### C08 — Usage API
- **What**: session usage with per-turn receipts; grouped report; CSV (design §6); cost fields on the session view.
- **Where**: new `internal/dashboard/builder_usage.go` (+ store queries), `builder_common.go`, routes.
- **Done when**: integration tests: totals equal ledger sums; receipts include files changed and builds from events; a non-owner gets 404; a non-superadmin cannot group by user or read others' rows; range cap enforced; CSV columns fixed and free of text content; deleted-session rows still counted.
- **Requirements**: ABC-11 to ABC-14, ABC-22, ABC-23. **Depends**: C04. **Tests**: integration. **Gate**: Full.
- **Status**: Done 2026-11-13. `internal/dashboard/builder_usage.go`: `GET /api/builder/sessions/{id}/usage` (totals, caps in use, one receipt per turn, calls outside a turn listed apart so totals always equal the sum), `GET /api/builder/usage` (grouped by day, user, model or session) and `GET /api/builder/usage.csv`. The session list and detail views carry a `usage` block (currency, calls, cost, unpriced calls; the detail also has the caps). What differs from the first draft of design §6 is recorded there ("Decided in C08"). Full gate green.

### C09 — Price table, currency and caps in settings UI
- **What**: price editor (add, edit, delete, optional fields cleared with explicit null), unpriced-models hint, currency field with the no-conversion warning, money cap fields with a warning when the active model has no price.
- **Where**: new section file under `src/pages/settings/`, `api.ts`, locales.
- **Done when**: frontend gate; i18n validated; all keys always sent on update; manual check on the local stack.
- **Requirements**: ABC-06 to ABC-10, ABC-19. **Depends**: C03, C05. **Gate**: Frontend.
- **Status**: Done 2026-11-13. `src/pages/settings/BuilderCostsSection.tsx` adds two cards to Integrations → AI Builder: **Costs** (currency with the no-conversion warning, the two money budgets typed as decimals and sent as micros, a warning when a budget is set and the model in use has no price) and **Model prices** (table, add/edit/delete, the "used with no price" list with a shortcut to price each model). `src/lib/money.ts` holds the shared money helpers (string to micros without floats, `Intl.NumberFormat` display). The three cost keys and the four price keys are always sent. Checked in a browser on the local stack: validation errors, save, the unpriced-model warning appearing and clearing, add, edit (key fields locked) and delete. Frontend gate green.

### C10 — Cost in the chat and session header
- **What**: per-turn usage line and expandable receipt; session total and cap bar; `budget_warning` notice; the three display states of ABC-13; shared money formatter.
- **Where**: `src/components/patterns/builder/*`, `builderModel.ts`, `api.ts`, locales.
- **Done when**: frontend gate; manual check on the local stack with a priced model, an unpriced model and a mixed session.
- **Requirements**: ABC-11 to ABC-14, ABC-20. **Depends**: C08. **Gate**: Frontend.

### C11 — Usage page
- **What**: range picker, grouping selector, table, totals, CSV download; superadmin and own-usage variants.
- **Where**: new page or settings section, `api.ts`, locales, navigation entry.
- **Done when**: frontend gate; manual check as superadmin and as a regular user.
- **Requirements**: ABC-22, ABC-23. **Depends**: C08. **Gate**: Frontend.

### C12 — Docs and CHANGELOG
- **What**: `docs/docs/ai-frontend-builder.md` (providers, price table, currency, caps, usage page, what "no price set" means); `CHANGELOG.md` `[Unreleased]`; README feature table row and its three translations if the usage page is listed as a feature; update this file's status.
- **Requirements**: all. **Depends**: C01 to C11. **Gate**: docs build + i18n JSON check.

### C13 — Real-endpoint run (owner)
- **What**: run the opt-in contract test against OpenAI and Anthropic with the owner's keys; compare one call's recorded tokens and cost with the provider's usage page; record the result in the design notes. Closes parent task T02 as well.
- **Requirements**: Success Criteria. **Depends**: C07. **Tests**: manual.

---

## Risks to watch

| Risk | Mitigation |
| --- | --- |
| Token class semantics differ per provider and the cost is silently wrong | Normalize in the adapter, verify against the API references in C02/C06, and check one real call against the provider's own usage page in C13. |
| Operator trusts a cost computed from a stale or mistyped price | Prices are visible with `updated_at`; changes are audited; "no price set" is never shown as zero. The docs state that Orbit's number is an estimate from the operator's own price table, not an invoice. |
| Session counters diverge from the ledger | Same transaction as today, or no counters at all (C01 decision). A test asserts equality. |
| Per-user spend exposed to colleagues | Grouping by user and other users' rows are superadmin-only; covered by tests. |
| Anthropic message rules (alternating roles, tool result placement) break on resumed histories | Mapping tests on real stored-history shapes, cross-provider replay test. |
| Money cap set while the model has no price gives a false sense of protection | Settings warning (C09) and docs; token caps stay mandatory. |
