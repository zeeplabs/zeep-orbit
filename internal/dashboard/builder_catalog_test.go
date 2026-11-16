package dashboard

// builder_catalog_test.go — the model catalog in the dashboard
// (ai-model-catalog M04, AMC-02/04/05). Runs against a real Postgres
// (TEST_DATABASE_URL).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
)

func TestBuilderCatalog_ReturnsCatalogToAnyUser(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := httptest.NewRecorder()
	h.GetBuilderCatalog(w, httptest.NewRequest(http.MethodGet, "/dashboard/api/builder/catalog", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no user: got %d, want 401", w.Code)
	}

	got := decodeJSON[builderCatalogResponse](t, callBuilder(t, h.GetBuilderCatalog, super, http.MethodGet, "/x", ""))
	if got.Currency != "USD" || got.ReviewedAt.IsZero() || len(got.Providers) != 2 {
		t.Fatalf("catalog header = %s / %s / %d providers", got.Currency, got.ReviewedAt, len(got.Providers))
	}
	find := func(provider, id string) *builderCatalogModel {
		for _, p := range got.Providers {
			if p.Provider != provider {
				continue
			}
			for i := range p.Models {
				if p.Models[i].ID == id {
					return &p.Models[i]
				}
			}
		}
		return nil
	}

	gpt := find("openai", "gpt-4.1")
	if gpt == nil || len(gpt.Prices) != 1 || len(gpt.Prices[0].Tiers) != 1 {
		t.Fatalf("gpt-4.1 = %+v", gpt)
	}
	tier := gpt.Prices[0].Tiers[0]
	if tier.Input != "2.00" || tier.Output != "8.00" || tier.CachedInput == nil || *tier.CachedInput != "0.50" || tier.CacheWrite != nil {
		t.Errorf("gpt-4.1 tier = %+v, want 2.00 / 0.50 cached / no cache write / 8.00", tier)
	}

	haiku := find("anthropic", "claude-haiku-5-5")
	if haiku == nil || len(haiku.Prices[0].Tiers) != 2 {
		t.Fatalf("claude-haiku-5-5 = %+v, want two tiers", haiku)
	}
	if lo, hi := haiku.Prices[0].Tiers[0], haiku.Prices[0].Tiers[1]; lo.MaxPromptTokens != 100_000 || lo.Input != "0.10" || hi.MaxPromptTokens != 0 || hi.Input != "0.50" {
		t.Errorf("haiku tiers = %+v / %+v", lo, hi)
	}
	if find("openai", "o3-mini") != nil {
		t.Error("o3-mini must not be in the catalog")
	}
}

func TestUpsertAIProvider_CatalogProvidersAcceptOnlyCatalogModels(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	for _, c := range []struct{ provider, body string }{
		{"openai", `{"model":"o3-mini","api_key":"sk-x","enabled":true}`},
		{"openai", `{"model":"claude-sonnet-5-5","api_key":"sk-x","enabled":true}`},
		{"anthropic", `{"model":"gpt-4.1","api_key":"sk-ant-x","enabled":true}`},
		{"anthropic", `{"model":"claude-made-up","api_key":"sk-ant-x","enabled":true}`},
	} {
		w := putOpenAICompatible(t, h, super, c.provider, c.body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errModelNotInCatalog) {
			t.Errorf("%s %s: got %d %s, want 400 %q", c.provider, c.body, w.Code, w.Body.String(), errModelNotInCatalog)
		}
	}
	if cur, err := GetAIProvider(context.Background(), pool, "openai"); err != nil || cur.Model != "" {
		t.Errorf("a refused model must not be stored: %+v / %v", cur, err)
	}

	// Surrounding spaces are trimmed before the lookup and the save.
	if w := putOpenAICompatible(t, h, super, "openai", `{"model":" gpt-4.1 ","api_key":"sk-x","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("catalog model: got %d %s", w.Code, w.Body.String())
	}
	if cur, _ := GetAIProvider(context.Background(), pool, "openai"); cur.Model != "gpt-4.1" {
		t.Errorf("stored model = %q, want gpt-4.1", cur.Model)
	}
	// openai_compatible keeps a free model.
	if w := putOpenAICompatible(t, h, super, aiProviderOpenAICompatible,
		`{"model":"llama3.1:70b","base_url":"https://llm.example.com/v1","enabled":true}`); w.Code != http.StatusOK {
		t.Errorf("openai_compatible free model: got %d %s", w.Code, w.Body.String())
	}
}

func TestCatalogModelRules(t *testing.T) {
	if !catalogModelUsable(aiProviderOpenAICompatible, "anything") {
		t.Error("openai_compatible runs any model")
	}
	if !catalogModelUsable("openai", "gpt-4.1") || catalogModelUsable("openai", "o3-mini") {
		t.Error("openai runs catalog models only")
	}
	if !catalogModelChoosable("anthropic", "claude-opus-5-5", "") || catalogModelChoosable("anthropic", "o3-mini", "o3-mini") {
		t.Error("a saved model outside the catalog cannot be saved again")
	}
}

// A provider row left on a model outside the catalog (saved before the
// catalog, or set by hand) makes the builder unavailable with its own reason.
func TestBuilderStatus_ModelNotInCatalog(t *testing.T) {
	e := newBHEnv(t)
	ctx := context.Background()
	if _, err := UpsertAIProvider(ctx, e.pool, aiProviderAnthropic,
		&aiProviderUpsertInput{Model: "claude-retired-model", APIKey: "sk-ant-local", Enabled: true}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	prov := aiProviderAnthropic
	e.setSettings(t, BuilderSettingsInput{Provider: &prov})

	got := decodeJSON[BuilderStatus](t, e.do(t, e.h.Status, e.owner, http.MethodGet, "/x", ""))
	if got.Available || got.Reason != BuilderReasonModelNotInCatalog || !got.ProviderConfigured {
		t.Errorf("want unavailable/model_not_in_catalog with the provider configured, got %+v", got)
	}

	if _, err := UpsertAIProvider(ctx, e.pool, aiProviderAnthropic,
		&aiProviderUpsertInput{Model: "claude-sonnet-5-5", Enabled: true}); err != nil {
		t.Fatalf("switch model: %v", err)
	}
	got = decodeJSON[BuilderStatus](t, e.do(t, e.h.Status, e.owner, http.MethodGet, "/x", ""))
	if !got.Available {
		t.Errorf("a catalog model makes the builder available again, got %+v", got)
	}
}

// A turn that starts while the row holds a model outside the catalog gets the
// unavailable provider (the turn ends provider_unavailable), not an adapter.
func TestBuilderRuntime_RefusesModelNotInCatalog(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	ctx := context.Background()
	if _, err := UpsertAIProvider(ctx, rt.pool, aiProviderAnthropic,
		&aiProviderUpsertInput{Model: "claude-retired-model", APIKey: "sk-ant-local", Enabled: true}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	super := mustCreateSuperadmin(t, rt.pool)
	prov := aiProviderAnthropic
	if _, err := UpsertBuilderSettings(ctx, rt.pool, super.ID, BuilderSettingsInput{Provider: &prov}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	d := rt.prepareDeps(ctx, agent.Deps{Provider: unavailableProvider{}})
	if _, ok := d.Provider.(unavailableProvider); !ok {
		t.Fatalf("provider = %s/%s, want the unavailable provider", d.Provider.Name(), d.Provider.Model())
	}
}
