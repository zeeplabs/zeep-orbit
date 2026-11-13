# AI Builder Costs Design

**Spec**: `.specs/features/ai-builder-costs/spec.md`
**Status**: Draft for review (2026-11-13). Items marked **[verify]** were not read in the code or in provider documentation yet and must be checked in the task that touches them.

---

## 1. What exists and is reused (read from the code)

| Piece | Where | Reuse |
| --- | --- | --- |
| Ledger | `zeep_system.ai_usage_ledger` (`provisioner.go`), `store.RecordUsage` (`internal/agent/store/usage.go`) | Extended with columns; same transaction with the session counters. |
| Token caps | `store.BudgetChecker.Check`, errors wrapping `ErrBudgetExceeded` | Extended with money caps and the warning. |
| Usage call sites | `loop.go` (`chat`) and `compaction.go` (`summarize`) | Pass the new token classes and the purpose. |
| Provider contract | `internal/agent/provider/provider.go` (`Usage{InputTokens, OutputTokens}`), `openai.go`, `contract_test.go` | `Usage` gains two fields; the contract table gains the Anthropic adapter. |
| Provider settings | `ai_providers_store.go` (`{model, api_key_encrypted, enabled, base_url}` per provider kind), `ai_provider_handlers.go` | New kind `anthropic`. |
| Builder settings | `builder_settings` single row, partial-update store (`builder_settings_store.go`) | New columns with the same merge-on-absent-key rule. |
| Session view | `builder_common.go` (`tokens_in`, `tokens_out`) and `api.ts` | Gains cost fields. |
| Audit | existing audit helper used by builder settings | Price and currency changes. |

## 2. Data model (DDL, higher-risk, AGENTS.md §8: show the diff and get confirmation before applying)

All statements are idempotent and go in the builder block of `provisioner.go`.

```sql
-- Price per provider kind and model. Prices are per million tokens, in the
-- installation currency (builder_settings.cost_currency).
CREATE TABLE IF NOT EXISTS zeep_system.ai_model_prices (
    provider             TEXT          NOT NULL,
    model                TEXT          NOT NULL,
    input_per_mtok       NUMERIC(18,6) NOT NULL CHECK (input_per_mtok >= 0),
    cached_input_per_mtok NUMERIC(18,6)         CHECK (cached_input_per_mtok >= 0),
    cache_write_per_mtok NUMERIC(18,6)          CHECK (cache_write_per_mtok >= 0),
    output_per_mtok      NUMERIC(18,6) NOT NULL CHECK (output_per_mtok >= 0),
    updated_by           UUID,
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model)
);

ALTER TABLE zeep_system.ai_usage_ledger
    ADD COLUMN IF NOT EXISTS cached_input_tokens INT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_write_tokens  INT    NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS purpose             TEXT   NOT NULL DEFAULT 'chat',
    ADD COLUMN IF NOT EXISTS cost_micros         BIGINT,          -- NULL = no price at record time
    ADD COLUMN IF NOT EXISTS usage_reported      BOOLEAN NOT NULL DEFAULT true;

CREATE INDEX IF NOT EXISTS ai_usage_ledger_session_idx
    ON zeep_system.ai_usage_ledger (session_id, created_at) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS ai_usage_ledger_created_idx
    ON zeep_system.ai_usage_ledger (created_at);

ALTER TABLE zeep_system.builder_sessions
    ADD COLUMN IF NOT EXISTS cost_micros     BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS unpriced_calls  INT    NOT NULL DEFAULT 0;

ALTER TABLE zeep_system.builder_settings
    ADD COLUMN IF NOT EXISTS cost_currency                TEXT   NOT NULL DEFAULT 'USD',
    ADD COLUMN IF NOT EXISTS cost_currency_changed_at     TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS session_cost_cap_micros      BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS user_monthly_cost_cap_micros BIGINT NOT NULL DEFAULT 0;
```

Notes:

- Existing ledger rows read as `purpose = 'chat'`, zero cache tokens, `cost_micros = NULL`. No backfill.
- `builder_sessions.cost_micros` and `unpriced_calls` are counters updated in the `RecordUsage` transaction, like `tokens_in/out`, so the session header needs no aggregate query. Existing sessions start at 0 cost with `unpriced_calls = 0`, which under-reports calls made before the upgrade. The session view therefore reports `cost_complete = false` when the session has ledger rows older than the feature. **Simpler alternative to decide in C01:** compute session totals from the ledger on read (one indexed query) and add no counters. Preferred if the query is cheap with the new index; it removes the divergence case entirely.
- `provider` in the price table is the provider **kind** (`openai`, `openai_compatible`, `anthropic`), the same string the ledger stores.
- No FK from the ledger to prices, by intent: deleting a price must not touch history.

## 3. Token classes and the cost formula (ABC-01 to ABC-05)

`provider.Usage` becomes:

```go
type Usage struct {
    InputTokens       int `json:"input_tokens"`        // billed at the full input rate
    CachedInputTokens int `json:"cached_input_tokens"` // read from the provider's prompt cache
    CacheWriteTokens  int `json:"cache_write_tokens"`  // written to the provider's prompt cache
    OutputTokens      int `json:"output_tokens"`
    Reported          bool `json:"reported"`           // false when the provider sent no usage
}
```

Adapter mapping, **[verify] against each provider's current API reference before coding**:

- OpenAI Chat Completions: `usage.prompt_tokens` includes cached tokens; cached count is in `usage.prompt_tokens_details.cached_tokens`. So `InputTokens = prompt_tokens - cached_tokens`, `CachedInputTokens = cached_tokens`, `CacheWriteTokens = 0`. OpenAI-compatible servers that omit the details object give `CachedInputTokens = 0`.
- Anthropic Messages: `usage.input_tokens` excludes cache activity; `cache_read_input_tokens` and `cache_creation_input_tokens` are separate. Map directly.

`Usage.Total()` (new helper) returns the sum of the four classes and replaces the hand-written `in + out` wherever caps or the compaction heuristic use it.

Cost, in micros, computed in Go with `math/big` (or integer arithmetic on prices scaled by 1e6), half-up rounding once per ledger row:

```
cost_micros = round( input*P_in + cached*P_cached + write*P_write + output*P_out )   -- P_* per token, in micros
P_cached defaults to P_in when NULL; P_write defaults to P_in when NULL
```

Prices are `NUMERIC(18,6)` per million tokens, so a price in micros per million tokens is an exact integer and `tokens * price_micros_per_mtok / 1_000_000` is integer arithmetic with one rounding. The function is pure (`internal/agent/store/cost.go`) and table-tested, including the rounding boundary and the int64 range (tokens up to 2e9 times a price up to 1e12 micros per million stays inside `big.Int`, then is checked to fit int64).

`RecordUsage` reads the price row inside its transaction (`SELECT ... FROM ai_model_prices WHERE provider = $1 AND model = $2`), computes the cost, inserts the ledger row, and updates the session counters. A missing row gives `cost_micros = NULL` and `unpriced_calls + 1`.

## 4. Budgets (ABC-19, ABC-20)

`BudgetChecker.Check` already reads both token caps and the session and month totals. It gains:

- money caps from `builder_settings`; `0` means no money cap;
- session cost and the user's month cost (`SUM(cost_micros)` over the ledger for the month, same index as the token sum);
- two new errors wrapping `ErrBudgetExceeded`: `ErrSessionCostCapReached`, `ErrMonthlyCostCapReached`.

Warning: `Check` returns, besides the error, a small value listing caps that are at or above 80%. The loop appends a `budget_warning` event with `{cap: "session_tokens" | "month_tokens" | "session_cost" | "month_cost", used, limit}` only if the session has no earlier `budget_warning` event for that cap. **[verify]** how the loop can test "event already exists" cheaply (store method by type and payload key, or a per-run in-memory set plus a store check at turn start).

The event payload carries numbers only. `budget_warning` must be added to the event whitelist served by the events API (`builderEventFields`, see the note in `ai-builder-backend/tasks.md` B06).

## 5. Anthropic adapter (ABC-15 to ABC-18)

New file `internal/agent/provider/anthropic.go`, same shape as `openai.go` (stdlib HTTP, no SDK dependency, response size guard, retry on 429 with `Retry-After`).

Mapping from the neutral types, **[verify] every field against the current Messages API reference (use the `claude-api` skill) before coding**:

| Neutral | Anthropic Messages |
| --- | --- |
| `ChatRequest.System` | top-level `system` |
| `RoleUser` text | `user` message with a text block |
| `RoleAssistant` with `ToolCalls` | `assistant` message with text block (if any) plus one `tool_use` block per call (`id`, `name`, `input`) |
| `RoleTool` | `user` message with a `tool_result` block (`tool_use_id`, content). Consecutive tool messages are merged into one user message, because roles must alternate. |
| `ToolSpec` | `tools[]` with `name`, `description`, `input_schema` |
| `MaxOutputTokens` | `max_tokens` (required by the API; when 0, a documented adapter default is sent) |
| `stop_reason` | `end_turn` → `stop`, `tool_use` → `tool_calls`, `max_tokens` → `length`; anything else passed through |

Details that need care:

- `ToolCall.Arguments` may hold a JSON string when a model emitted invalid JSON (see the comment in `provider.go`). `tool_use.input` must be an object, so the adapter sends `{}` plus the original text in a text block in that case. Covered by a contract test.
- Error mapping: 401/403 → `ErrAuth`; 429 → `*ErrRateLimited`; the context-too-long condition → `ErrContextTooLong` (**[verify]** the exact status and error type); others → `*ErrProvider` with a truncated body, never logged with headers.
- Base URL is fixed to the official endpoint. A custom base URL for this kind is not offered in v1 (gateways are reachable through `openai_compatible`).
- Version header pinned to a constant; the model name comes from settings, never hardcoded.

Settings: `ai_providers` accepts the kind `anthropic` (same store and handlers; the provider-kind validation list gains one value). `builder_settings.provider` accepts it. The settings UI adds the kind to the existing provider selector. `builder_runtime.go` builds the adapter for the selected kind. **[verify]** where the kind list is validated (`ai_provider_handlers.go`, `builder_settings.go`).

Resume across providers (ABC-17): the stored history is neutral, so nothing is migrated. One real risk: tool call IDs produced by one provider are replayed to the other. Both accept opaque strings; the contract test replays an OpenAI-shaped history through the Anthropic adapter and the reverse. **[verify]** ID format constraints of each API.

## 6. API (ABC-11 to ABC-14, ABC-22, ABC-23)

All under the existing builder routes, same auth middleware. Error strings in English.

| Method and path | Who | Returns |
| --- | --- | --- |
| `GET /api/dashboard/builder/sessions/{id}/usage` | session owner or superadmin (same access check as the session) | session totals plus one row per turn: calls, tokens by class, `cost_micros` (nullable), `unpriced_calls`, duration, model(s), files changed, builds run |
| `GET /api/dashboard/builder/usage?from&to&group_by=day\|user\|model\|session` | superadmin: all users; others: own usage only (the `user` grouping is refused for non-superadmins) | grouped totals, currency, compaction share |
| `GET /api/dashboard/builder/usage.csv?from&to` | same rule | one line per ledger row: timestamp, user id, session id, turn id, provider, model, purpose, token classes, cost |
| `GET /api/dashboard/builder/prices` | superadmin | price rows plus `unpriced_models` (models seen in the ledger in the last 30 days with no price) |
| `PUT /api/dashboard/builder/prices/{provider}/{model}` | superadmin | upsert; all four price keys always sent by the UI (null clears the optional ones) |
| `DELETE /api/dashboard/builder/prices/{provider}/{model}` | superadmin | 204 |

The session view (`builder_common.go`) gains `cost_micros`, `unpriced_calls`, `currency`, and the cap usage when caps apply. The turn receipt takes files changed and builds from the turn's events (`file_changed`, `build_*`), which already exist.

Date ranges are capped (for example 366 days) and the CSV is streamed with a row limit, to keep one request from scanning the whole ledger. The model name in a path segment is URL-escaped; models containing `/` (common on OpenAI-compatible gateways) must round-trip, so the handler reads the raw path. **[verify]** chi wildcard behavior, or move the key to the body/query.

## 7. UI

- **Chat** (`ChatPanel.tsx`, `builderModel.ts`): a compact line under each finished turn, "12.4k tokens · $0.08", expandable into the receipt. When some calls have no price: "$0.08 + unpriced calls". When none has: "12.4k tokens · no price set".
- **Session header** (`SessionActions.tsx` or the page header): session total and a thin cap bar when a cap applies; the `budget_warning` event renders as a timeline notice.
- **Settings → AI Builder** (`AIBuilderSettings.tsx`; split into a section file as the skills section did): provider kind `anthropic`; currency; money caps; price table editor with the "models without a price" hint.
- **Usage page**: new section under the builder settings area, with range picker, grouping selector, table, totals and CSV download. Charts are left for later; a table is enough for v1.
- Money formatting with `Intl.NumberFormat` and the installation currency; micros converted in one helper. All strings in `en.json` and `pt-BR.json`. Every mutation has `onError` with a toast. Usage hooks are shared, not fetched ad hoc per component.

## 8. Security and compliance

- API keys: same encryption and handling as the existing provider keys; never in logs, events, audit or error bodies. The Anthropic adapter truncates upstream error bodies and never logs request headers.
- Usage rows hold identifiers and counts, no prompt or reply text. The CSV has the same columns.
- Per-user usage is visible only to superadmins. A non-superadmin asking for another user's usage gets the same 404 as for a session they cannot see.
- Prices are not secrets, so audit entries carry old and new values.
- No network call is added other than the provider API itself. Prices are never fetched.

## 9. Testing strategy

| Layer | Tests |
| --- | --- |
| Cost function | unit, table-driven: each class, optional prices falling back to input, zero prices, rounding boundary, overflow guard |
| Adapters | unit with `httptest`: usage mapping with and without cache fields, error mapping, message mapping (merged tool results, invalid tool arguments); the shared contract table runs both adapters; cross-provider replay |
| Store | integration (real Postgres): `RecordUsage` with and without a price, counters equal to ledger sums, price edit does not change old rows, money caps, warning once |
| Handlers | integration: RBAC for each route, grouping rules, CSV columns, model names with `/` |
| Real endpoints | opt-in test (`contract_real_test.go` pattern) with keys from the environment; run by the owner, never in CI by default |
| Frontend | gate only (`tsc -b`, `npm run build`, i18n JSON); manual check on the local stack |

Go tests that touch the database run with `-p 1 -parallel 4`, as AGENTS.md requires.
