# AI Model Catalog Design

**Spec**: `.specs/features/ai-model-catalog/spec.md` (approved 2026-11-13, all defaults of its Assumptions table confirmed)

---

## 1. Overview

```
internal/agent/catalog        (new, no dependencies)
   models, prices (decimal strings), effective_from, tiers, deprecated
        │
        ├──► internal/agent/store.callCost      price of a ledger row (catalog providers)
        │       └─ ai_model_prices               price of openai_compatible rows (unchanged)
        │
        └──► internal/dashboard
               GET  /api/builder/catalog         read-only catalog for the UI
               PUT  /api/ai-providers/{p}        rejects non-catalog models (openai, anthropic)
               availability()                    reason model_not_in_catalog
               BuilderRuntime.resolveProvider    refuses a non-catalog model (unavailable provider)
               /api/builder/prices               writes only for openai_compatible
               builder settings                  cost_currency fixed to USD + review flag
```

The frontend reads the catalog from the API; `internal/dashboard/ui/src/lib/aiModelCatalog.ts` (commit `0ae2140`) is deleted.

## 2. Catalog package — `internal/agent/catalog`

No imports from Orbit packages, so `store` and `dashboard` can both import it.

```go
package catalog

const Currency = "USD"

// ReviewedAt is the day the models and prices were checked on the providers' pages.
var ReviewedAt = time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)

type Tier struct {
    // MaxPromptTokens is the largest prompt (input + cached input + cache write)
    // billed at this tier; 0 on the last tier means no limit.
    MaxPromptTokens int
    Input, Output   string // decimal, per million tokens
    CachedInput     string // "" = billed at Input
    CacheWrite      string // "" = billed at Input
}

type PriceVersion struct {
    EffectiveFrom time.Time // UTC
    Tiers         []Tier    // ascending MaxPromptTokens, last one 0
}

type Model struct {
    Provider   string // "openai" | "anthropic" (the ledger's provider kind)
    ID         string
    Deprecated bool
    Prices     []PriceVersion // ascending EffectiveFrom
}

func Covers(provider string) bool                   // openai, anthropic
func Models(provider string) []Model                // copy, catalog order
func Lookup(provider, id string) (Model, bool)
func (m Model) PriceAt(t time.Time) (PriceVersion, bool)    // last EffectiveFrom <= t
func (v PriceVersion) TierFor(promptTokens int) Tier
```

The API returns every price version, so the UI can show an upcoming price without a helper here.

- Prices stay decimal strings so the data reads like the providers' pages. They are parsed with `store.ParsePrice`; the validation test (`catalog_test`, external package) imports `store`, which is allowed because it is test-only.
- First data set: the models and prices of commit `0ae2140`, plus Claude Haiku 5.5 with two tiers (≤100,000: 0.10 / 0.125 / 0.01 / 0.50; above: 0.50 / 0.625 / 0.05 / 2.50), all with `EffectiveFrom` = 2026-11-13.
- Anthropic cache write = the 5-minute write price (the adapter requests no 1-hour cache).

## 3. Pricing — `store.callCost`

```go
func callCost(ctx, tx, r UsageRecord) (*int64, error) {
    if catalog.Covers(r.Provider) {
        m, ok := catalog.Lookup(r.Provider, r.Model)
        if !ok { return nil, nil }              // "no price set"
        v, ok := m.PriceAt(time.Now().UTC())
        if !ok { return nil, nil }
        p, err := priceFromTier(v.TierFor(r.InputTokens + r.CachedInputTokens + r.CacheWriteTokens))
        if err != nil { return nil, nil }       // malformed data is caught by the catalog test
        return costOrNil(p, r)
    }
    // ai_model_prices query, unchanged
}
```

`time.Now()` instead of the row's `created_at`: the row is inserted in the same transaction, within milliseconds. `CostMicros` is unchanged; tiers only choose which `Price` is passed to it.

## 4. Currency — DDL and settings  `[higher-risk: DDL]`

Additions to `internal/dashboard/provisioner.go`, idempotent:

```sql
ALTER TABLE zeep_system.builder_settings
  ADD COLUMN IF NOT EXISTS cost_caps_review_pending BOOLEAN NOT NULL DEFAULT false;

-- Catalog prices are in USD (ai-model-catalog). An installation that billed in
-- another currency is moved to USD once; the cap numbers are kept and flagged
-- for review. Stored costs are not converted.
UPDATE zeep_system.builder_settings
   SET cost_currency = 'USD', cost_currency_changed_at = now(), cost_caps_review_pending = true
 WHERE cost_currency <> 'USD';
```

- The `UPDATE` matches no row after the first run, so concurrent replicas and restarts are safe.
- `PUT /api/builder/settings`: `cost_currency` other than `USD` → 400 `cost_currency must be USD`; `USD` is still accepted (old clients). A body that carries `session_cost_cap_micros` or `user_monthly_cost_cap_micros` clears `cost_caps_review_pending` (the Costs card always sends both).
- `BuilderSettings` gains `cost_caps_review_pending`. Every reader of `builder_settings` uses the explicit `builderSettingsCols` list, so adding the column is safe.

## 5. Dashboard API

| Endpoint | Change |
| --- | --- |
| `GET /dashboard/api/builder/catalog` (new, `RequireAuth`) | `{currency, reviewed_at, providers: [{provider, models: [{id, deprecated, prices: [{effective_from, tiers: [{max_prompt_tokens, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok, output_per_mtok}]}]}]}]}`. Decimals in the canonical `store.FormatPrice` form; empty optional prices are `null`. No secret in it. |
| `PUT /dashboard/api/ai-providers/{openai,anthropic}` | Model must be in the catalog. A deprecated model is accepted only when it equals the saved one. Error: 400 `model is not in the catalog`. `openai_compatible` unchanged. |
| `GET /dashboard/api/builder/status` | New reason `model_not_in_catalog`, checked right after `provider_not_configured`. Message endpoints already answer 503 through `requireAvailable`. |
| Builder worker | `resolveProvider` returns `unavailableProvider` for a catalog provider whose model is not in the catalog (a turn queued before the model changed). |
| `GET /dashboard/api/builder/prices` | Returns only `openai_compatible` rows. `unpriced_models`: ledger models with no price, meaning `openai_compatible` models with no row plus catalog-provider models that are not in the catalog. |
| `PUT`/`DELETE /dashboard/api/builder/prices` | `openai` or `anthropic` → 400 `prices of catalog providers cannot be changed`. |

## 6. Frontend

- `api.ts`: `BuilderCatalog` types, `builderKeys.catalog`, `useBuilderCatalog()` (`staleTime: Infinity`: the catalog only changes with a release).
- **AI tab** (`GitHubIntegrationPage.tsx`): for `openai`/`anthropic`, a select with the non-deprecated models, plus the saved model when it is deprecated (labelled) or not in the catalog (labelled, with a notice to replace it). No custom input. `openai_compatible` keeps the text input.
- **Costs card**: the currency input becomes the read-only text "USD"; the change-of-currency warning goes away; when `cost_caps_review_pending` is set, a warning notice asks to review the caps (saving the card clears it).
- **Model prices card**: a read-only table of the catalog (provider, model, input, cached input, cache write, output; tiers shown as extra lines with "up to N tokens" / "above N tokens"; a future price shows "from {date}"), with the reviewed date; below it the existing editable table and form, restricted to `openai_compatible` (no provider select). The "fill in the official price" button is removed.
- **AI Builder status card**: text for `model_not_in_catalog`.
- i18n: new keys in `en.json` and `pt-BR.json`; keys that lose their use are removed.

## 7. Tests that change

- `internal/dashboard/builder_prices*_test.go` and `internal/agent/store/usage*_test.go` that seed `openai` price rows: move them to `openai_compatible`, and add catalog cases (cost from the catalog with no row, a non-catalog `openai` model → NULL, a Haiku 5.5 tier boundary).
- `internal/dashboard/ai_provider*_test.go` that save arbitrary OpenAI models (`gpt-4o-mini` etc.): use catalog IDs; add the 400 case and the deprecated case.
- Builder settings tests that set `BRL`: expect 400; add the provision test for the review flag.

## 8. Risks

- **Stale catalog on self-hosted installs** — accepted (spec, Assumption 2). `ReviewedAt` is shown in the UI.
- **A release removes a model by mistake** — a test cannot see the previous release's list, so the rule (deprecate, never remove, while the provider serves the model) lives in `RELEASE.md` and in the catalog file's header comment, and is checked in review.
- **Existing installs with a non-catalog OpenAI model** (for example `o3-mini` saved from the old presets) — the builder becomes unavailable until a superadmin picks a model; the status card says why. The AI chat drawers are unaffected.
