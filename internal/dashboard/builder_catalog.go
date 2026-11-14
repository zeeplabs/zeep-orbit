package dashboard

// builder_catalog.go — the model catalog in the dashboard (ai-model-catalog
// M04, AMC-02/04/05): the read-only catalog endpoint and the rules that keep
// the openai and anthropic provider rows on catalog models.

import (
	"net/http"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/catalog"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// Error of PUT /api/ai-providers/{openai,anthropic} for a model that cannot
// be chosen.
const errModelNotInCatalog = "model is not in the catalog"

// catalogModelUsable reports whether provider can run model: always for a
// provider outside the catalog, and only for a catalog model (deprecated
// included) otherwise.
func catalogModelUsable(provider, model string) bool {
	if !catalog.Covers(provider) {
		return true
	}
	_, ok := catalog.Lookup(provider, model)
	return ok
}

// catalogModelChoosable reports whether model may be saved for a catalog
// provider whose row currently holds saved: a catalog model that is not
// deprecated, or the deprecated model the row already uses.
func catalogModelChoosable(provider, model, saved string) bool {
	m, ok := catalog.Lookup(provider, model)
	if !ok {
		return false
	}
	return !m.Deprecated || model == saved
}

// builderCatalogTier and the types below are the JSON of GET
// /api/builder/catalog. Prices are canonical decimal strings per million
// tokens (store.FormatPrice); an optional price that the provider does not
// bill separately is null. max_prompt_tokens 0 means no limit.
type builderCatalogTier struct {
	MaxPromptTokens int     `json:"max_prompt_tokens"`
	Input           string  `json:"input_per_mtok"`
	CachedInput     *string `json:"cached_input_per_mtok"`
	CacheWrite      *string `json:"cache_write_per_mtok"`
	Output          string  `json:"output_per_mtok"`
}

type builderCatalogPrice struct {
	EffectiveFrom time.Time            `json:"effective_from"`
	Tiers         []builderCatalogTier `json:"tiers"`
}

type builderCatalogModel struct {
	ID         string                `json:"id"`
	Deprecated bool                  `json:"deprecated"`
	Prices     []builderCatalogPrice `json:"prices"`
}

type builderCatalogProvider struct {
	Provider string                `json:"provider"`
	Models   []builderCatalogModel `json:"models"`
}

type builderCatalogResponse struct {
	Currency   string                   `json:"currency"`
	ReviewedAt time.Time                `json:"reviewed_at"`
	Providers  []builderCatalogProvider `json:"providers"`
}

// canonicalCatalogPrice renders a catalog price in the canonical form of the
// price API. The catalog test guarantees it parses.
func canonicalCatalogPrice(v string) string {
	micros, err := store.ParsePrice(v)
	if err != nil {
		return v
	}
	return store.FormatPrice(micros)
}

func buildCatalogResponse() builderCatalogResponse {
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		c := canonicalCatalogPrice(v)
		return &c
	}
	out := builderCatalogResponse{Currency: catalog.Currency, ReviewedAt: catalog.ReviewedAt}
	for _, provider := range []string{catalog.ProviderOpenAI, catalog.ProviderAnthropic} {
		p := builderCatalogProvider{Provider: provider, Models: []builderCatalogModel{}}
		for _, m := range catalog.Models(provider) {
			jm := builderCatalogModel{ID: m.ID, Deprecated: m.Deprecated, Prices: []builderCatalogPrice{}}
			for _, v := range m.Prices {
				jv := builderCatalogPrice{EffectiveFrom: v.EffectiveFrom, Tiers: []builderCatalogTier{}}
				for _, t := range v.Tiers {
					jv.Tiers = append(jv.Tiers, builderCatalogTier{
						MaxPromptTokens: t.MaxPromptTokens,
						Input:           canonicalCatalogPrice(t.Input),
						CachedInput:     optional(t.CachedInput),
						CacheWrite:      optional(t.CacheWrite),
						Output:          canonicalCatalogPrice(t.Output),
					})
				}
				jm.Prices = append(jm.Prices, jv)
			}
			p.Models = append(p.Models, jm)
		}
		out.Providers = append(out.Providers, p)
	}
	return out
}

// GetBuilderCatalog handles GET /dashboard/api/builder/catalog for any
// authenticated user: the catalog holds public list prices and no secret.
func (h *Handler) GetBuilderCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFromContext(r.Context()); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, buildCatalogResponse())
}
