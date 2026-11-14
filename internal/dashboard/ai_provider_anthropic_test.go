package dashboard

// ai_provider_anthropic_test.go — the `anthropic` provider kind in the
// provider API, the builder settings and the builder runtime
// (ai-builder-costs C07, ABC-15). Runs against a real Postgres
// (TEST_DATABASE_URL).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/agent"
)

const anProvider = "anthropic"

// The kind is saved like openai: a model and a write-only key, no base URL.
func TestAnthropicProvider_SaveAndGetNeverReturnsKey(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := putOpenAICompatible(t, h, super, anProvider, `{"model":"claude-sonnet-5-5","api_key":"sk-ant-secret-123","base_url":"","enabled":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sk-ant-secret-123") {
		t.Fatal("PUT response must never echo the key")
	}
	gw, got := getAIProviderHTTP(t, h, super, anProvider)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", gw.Code)
	}
	if !got.HasKey || got.Model != "claude-sonnet-5-5" || !got.Enabled || got.Provider != anProvider || got.BaseURL != "" {
		t.Errorf("unexpected GET body: %+v", got)
	}
	if strings.Contains(gw.Body.String(), "sk-ant-secret-123") || strings.Contains(gw.Body.String(), "api_key") {
		t.Fatalf("GET must never carry key material: %s", gw.Body.String())
	}

	rt, err := ResolveAIProviderRuntime(context.Background(), pool, anProvider)
	if err != nil {
		t.Fatalf("ResolveAIProviderRuntime: %v", err)
	}
	if rt.Provider != anProvider || rt.Model != "claude-sonnet-5-5" || rt.APIKey != "sk-ant-secret-123" || rt.BaseURL != "" {
		t.Errorf("runtime: provider=%q model=%q baseURL=%q keyKept=%v", rt.Provider, rt.Model, rt.BaseURL, rt.APIKey == "sk-ant-secret-123")
	}
}

// A base URL is not offered for this kind: the request is refused and
// nothing is stored.
func TestAnthropicProvider_BaseURLRejected(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := putOpenAICompatible(t, h, super, anProvider, `{"model":"claude-sonnet-5-5","api_key":"sk-ant-x","base_url":"https://proxy.example/v1","enabled":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "base_url is only supported by the openai_compatible provider") {
		t.Fatalf("PUT with base_url: got %d %s", w.Code, w.Body.String())
	}
	if _, err := ResolveAIProviderRuntime(context.Background(), pool, anProvider); !errors.Is(err, ErrAIProviderUnavailable) {
		t.Errorf("a refused PUT must leave the provider unconfigured, got %v", err)
	}
}

// Without a key the provider is unavailable, like openai.
func TestAnthropicProvider_KeyRequiredAtRuntime(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	if w := putOpenAICompatible(t, h, super, anProvider, `{"model":"claude-sonnet-5-5","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	if _, err := ResolveAIProviderRuntime(context.Background(), pool, anProvider); !errors.Is(err, ErrAIProviderUnavailable) {
		t.Errorf("key-less anthropic: want ErrAIProviderUnavailable, got %v", err)
	}
}

// The builder can be pointed at the kind, an unknown kind is a 400 with an
// English message, and the next turn gets the Anthropic adapter.
func TestBuilderSettings_AnthropicProviderSelectedAndBuilt(t *testing.T) {
	rt := newTestBuilderRuntime(t)
	pool := rt.pool
	h := NewHandler(pool, nil, nil)
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()

	w := callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"provider":"anthropic"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT settings: %d %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[BuilderSettings](t, w); got.Provider != anProvider {
		t.Errorf("provider = %q, want anthropic", got.Provider)
	}
	w = callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"provider":"claude"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "provider must be 'openai', 'openai_compatible' or 'anthropic'") {
		t.Errorf("unknown kind: got %d %s", w.Code, w.Body.String())
	}

	base := agent.Deps{Provider: unavailableProvider{}}
	if d := rt.prepareDeps(ctx, base); d.Provider.Name() == anProvider {
		t.Fatal("the provider row does not exist yet: the turn must not get an Anthropic adapter")
	}
	if w := putOpenAICompatible(t, h, super, anProvider, `{"model":"claude-sonnet-5-5","api_key":"sk-ant-local","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT provider: %d %s", w.Code, w.Body.String())
	}
	d := rt.prepareDeps(ctx, base)
	if d.Provider.Name() != anProvider || d.Provider.Model() != "claude-sonnet-5-5" {
		t.Fatalf("provider = %s/%s, want anthropic/claude-sonnet-5-5", d.Provider.Name(), d.Provider.Model())
	}
}

// The kind is priced by the model catalog (ai-model-catalog AMC-09), so a
// price row for it is refused; an unknown kind is refused too.
func TestBuilderPrices_AnthropicKindIsCatalogPriced(t *testing.T) {
	if _, _, err := normalizeBuilderPriceKey(anProvider, "claude-sonnet-5-5"); err == nil || err.Error() != errCatalogPriceReadOnly {
		t.Errorf("normalizeBuilderPriceKey(anthropic) = %v, want %q", err, errCatalogPriceReadOnly)
	}
	if _, _, err := normalizeBuilderPriceKey("claude", "m"); err == nil {
		t.Error("an unknown provider kind must be rejected")
	}
}

// A stored key that cannot be decrypted (saved under another secret) is
// unavailable like a missing one, but carries its own sentinel so the builder
// status can log the real cause.
func TestAnthropicProvider_UndecryptableKeyIsDistinguishable(t *testing.T) {
	pool, h := aiProviderHandlerTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	if w := putOpenAICompatible(t, h, super, anProvider, `{"model":"claude-sonnet-5-5","api_key":"sk-ant-x","enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE zeep_system.ai_providers SET api_key_encrypted = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' WHERE provider = $1`, anProvider); err != nil {
		t.Fatalf("corrupt key: %v", err)
	}
	_, err := ResolveAIProviderRuntime(context.Background(), pool, anProvider)
	if !errors.Is(err, ErrAIProviderUnavailable) || !errors.Is(err, ErrAIProviderKeyUndecryptable) {
		t.Fatalf("want both ErrAIProviderUnavailable and ErrAIProviderKeyUndecryptable, got %v", err)
	}
	// A key-less row is not this case.
	if _, err := pool.Exec(context.Background(),
		`UPDATE zeep_system.ai_providers SET api_key_encrypted = '' WHERE provider = $1`, anProvider); err != nil {
		t.Fatalf("clear key: %v", err)
	}
	if _, err := ResolveAIProviderRuntime(context.Background(), pool, anProvider); errors.Is(err, ErrAIProviderKeyUndecryptable) {
		t.Errorf("a missing key must not be reported as undecryptable: %v", err)
	}
}
