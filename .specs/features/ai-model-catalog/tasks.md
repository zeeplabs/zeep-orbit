# AI Model Catalog Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** If the skill cannot be activated, stop and tell the user.

One commit per finished task with a green gate (authorized by the maintainer for the builder work); push only with the maintainer's OK; never add Claude/Anthropic attribution; never force push. **M03 adds DDL and a data `UPDATE` to `provisioner.go` (higher-risk, AGENTS.md §8): show the diff and get confirmation before applying.** Tests run locally or on ZeepLabs-controlled infra, never on Starbem infra. Provider API keys are never written to the repository, a spec, a log or a memory note.

---

**Spec**: `.specs/features/ai-model-catalog/spec.md`
**Design**: `.specs/features/ai-model-catalog/design.md`
**Status**: Spec approved by the maintainer on 2026-11-13, DDL of M03 included. Done: M01–M08 on `feat/ai-frontend-builder`.

---

## Gate Check Commands

| Gate | When | Command |
| --- | --- | --- |
| Quick | Unit-only task | `go build ./... && go vet ./... && go test ./<task pkg>/...` |
| Full | Task touching DB or dashboard | `go build ./... && go vet ./... && gofmt -l cmd internal && go test -p 1 -parallel 4 -timeout 25m ./...` |
| Frontend | UI tasks | `cd internal/dashboard/ui && npx tsc -b && npm run build` plus i18n JSON validation for `en.json` and `pt-BR.json` |

## Dependency Graph

```
M01 catalog package ──► M02 pricing in callCost ──┐
        │                                          ├──► M06 frontend ──► M07 docs ──► M08 local check
        ├──► M04 catalog API + model validation ───┤
        │                                          │
M03 DDL + USD (confirm DDL) ──► M05 prices API ────┘
```

---

## Tasks

### M01 — Catalog package
- **What**: `internal/agent/catalog` with the types and functions of design §2 and the first data set (models of `0ae2140` plus Claude Haiku 5.5 with two tiers). Header comment: how to update, deprecate instead of removing.
- **Done when**: `Covers`, `Models`, `Lookup`, `PriceAt`, `TierFor` work; the validation test (external `catalog_test` package) fails on duplicated IDs, unknown provider, empty `Prices`, versions out of order, tiers out of order or with a last tier that is not unbounded, a price `store.ParsePrice` rejects, and a `ReviewedAt` older than the newest `EffectiveFrom`.
- **Requirements**: AMC-01, AMC-11. **Tests**: unit. **Gate**: Quick (`./internal/agent/...`).
- **Status**: Done 2026-11-13. Quick gate: 443 tests in 8 packages pass.

### M02 — Pricing from the catalog
- **What**: `store.callCost` per design §3; existing store tests that seed `openai` price rows move to `openai_compatible`; new cases.
- **Done when**: an `openai`/`gpt-4.1` call with no price row gets the catalog cost; an `openai` model outside the catalog gets NULL; an `ai_model_prices` row for `openai` is ignored; Haiku 5.5 at 100,000 and 100,001 prompt tokens uses each tier; `openai_compatible` is unchanged.
- **Requirements**: AMC-06, AMC-07. **Tests**: unit + integration. **Gate**: Full.
- **Status**: Done 2026-11-13. Full gate: 29 packages pass.

### M03 — USD only  `[higher-risk: confirm DDL first]`
- **What**: DDL and `UPDATE` of design §4; settings validation (`cost_currency must be USD`); `cost_caps_review_pending` in `BuilderSettings` and cleared by a body with a cost cap.
- **Done when**: DDL diff shown and confirmed; provisioning twice is a no-op the second time; a `BRL` row becomes `USD` with the flag and caps unchanged; `PUT` with `BRL` is 400, with `USD` is 200; saving a cap clears the flag.
- **Requirements**: AMC-10 (backend). **Tests**: integration. **Gate**: Full.
- **Status**: Done 2026-11-13. DDL confirmed by the maintainer. Provisioning twice tested (`TestProvision_MovesOtherCurrencyToUSDAndFlagsCaps`). Full gate: 29 packages pass.

### M04 — Catalog API and model validation
- **What**: `GET /dashboard/api/builder/catalog`; model validation on `PUT /ai-providers/{openai,anthropic}`; availability reason `model_not_in_catalog`; `resolveProvider` refusal. Existing provider tests move to catalog IDs.
- **Done when**: the catalog JSON matches design §5; a non-catalog model is 400; a deprecated saved model can be re-saved; status reports `model_not_in_catalog` for a row set by SQL; a queued turn with such a model ends `provider_unavailable`.
- **Requirements**: AMC-02, AMC-04, AMC-05. **Tests**: integration. **Gate**: Full.
- **Status**: Done 2026-11-13. Full gate: 29 packages pass. The deprecated-model path (re-saving a deprecated saved model) has no test yet: the first catalog has no deprecated model, and the catalog data is not injectable; `catalogModelChoosable` covers it in code.

### M05 — Prices API for openai_compatible only
- **What**: `GET` filters to `openai_compatible` rows; `unpriced_models` per design §5; `PUT`/`DELETE` reject catalog providers.
- **Done when**: the handler tests cover each case; existing price tests use `openai_compatible`.
- **Requirements**: AMC-09. **Tests**: integration. **Gate**: Full.
- **Status**: Done 2026-11-13. Full gate: 29 packages pass.

### M06 — Frontend
- **What**: design §6; delete `src/lib/aiModelCatalog.ts`.
- **Done when**: frontend gate passes; checked in the browser (AI tab for both providers, a non-catalog saved model, the prices card, the Costs card with the review notice) in en and pt-BR.
- **Requirements**: AMC-03, AMC-08, AMC-10 (UI). **Tests**: none (no framework); browser check. **Gate**: Frontend.
- **Status**: Done 2026-11-13. Frontend gate passes. Checked on the local stack in en and pt-BR: AI tab lists for both providers (no free text), a saved `o3-mini` shows the notice and blocks the save, the catalog table with the Haiku 5.5 tiers, the OpenAI-compatible section, and the Costs card with the review notice (set by SQL, then restored). Fixed during the check: catalog dates rendered one day early west of UTC (now formatted in UTC).

### M07 — Docs
- **What**: `docs/docs/ai-frontend-builder.md` (provider models, Model prices, currency, `model_not_in_catalog`, API); CHANGELOG (rewrite the `0ae2140` entry into this feature, with the behavior changes: USD only, no custom model for OpenAI/Anthropic, builder unavailable on a non-catalog model); `RELEASE.md` catalog step; README rows if a feature row mentions model prices (and the three translations).
- **Done when**: Docusaurus build passes; no doc still says Orbit ships no prices.
- **Requirements**: AMC-12, AMC-13. **Tests**: none. **Gate**: docs build.
- **Status**: Done 2026-11-13. Docusaurus build passes. Also aligned the earlier Unreleased cost-accounting entry of the CHANGELOG and marked the ai-builder-costs spec as superseded in part.

### M08 — Local check
- **What**: on the local stack, the screens of M06 and one builder turn on `openai_compatible` (mock) to confirm nothing regressed; catalog pricing is covered by M02 tests (a real OpenAI/Anthropic turn needs the owner's key and is optional).
- **Done when**: results recorded here.
- **Requirements**: all. **Gate**: —.
- **Status**: Done 2026-11-13. Local stack: `GET /builder/status` available, `GET /builder/catalog` 200 (USD, openai 13 models, anthropic 9). One planning turn on `openai_compatible`/`mock-model` reached its plan card; its call cost 3000 micros, matching the operator row (500×3 + 1000×0.30 + 80×15). Screens: see M06. A real OpenAI/Anthropic turn was not run (needs the owner's key; catalog pricing is covered by the M02 tests).
