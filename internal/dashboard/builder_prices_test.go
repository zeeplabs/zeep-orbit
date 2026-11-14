package dashboard

// builder_prices_test.go — integration tests (real Postgres through
// TEST_DATABASE_URL) for the model price table and the currency and money
// caps of the builder settings (ai-builder-costs C03, ABC-06 to ABC-10).

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func builderPricesTestPool(t *testing.T) (*db.Pool, *Handler) {
	t.Helper()
	pool, h := builderSettingsTestPool(t)
	truncate := func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.ai_model_prices, zeep_system.ai_usage_ledger`)
	}
	truncate()
	t.Cleanup(truncate)
	return pool, h
}

func builderPriceRowCount(t *testing.T, pool *db.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM zeep_system.ai_model_prices`).Scan(&n); err != nil {
		t.Fatalf("count ai_model_prices: %v", err)
	}
	return n
}

const builderPriceBody = `{"provider":"openai_compatible","model":"gpt-test","input_per_mtok":"3","cached_input_per_mtok":"0.3","cache_write_per_mtok":null,"output_per_mtok":"15.000000"}`

func TestBuilderPrices_NonSuperadminForbiddenNoMutation(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	admin := mustCreateRegularUser(t, pool)
	super := mustCreateSuperadmin(t, pool)
	callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", builderPriceBody)

	routes := []struct {
		name   string
		fn     http.HandlerFunc
		meth   string
		target string
		body   string
	}{
		{"GET prices", h.ListBuilderPricesConfig, http.MethodGet, "/x", ""},
		{"PUT price", h.UpsertBuilderPriceConfig, http.MethodPut, "/x", strings.Replace(builderPriceBody, `"3"`, `"99"`, 1)},
		{"DELETE price", h.DeleteBuilderPriceConfig, http.MethodDelete, "/x?provider=openai_compatible&model=gpt-test", ""},
	}
	for _, rt := range routes {
		if w := callBuilder(t, rt.fn, admin, rt.meth, rt.target, rt.body); w.Code != http.StatusForbidden {
			t.Errorf("%s as admin: expected 403, got %d: %s", rt.name, w.Code, w.Body.String())
		}
		if w := callBuilder(t, rt.fn, nil, rt.meth, rt.target, rt.body); w.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated: expected 401, got %d", rt.name, w.Code)
		}
	}
	p, err := GetBuilderPrice(context.Background(), pool, "openai_compatible", "gpt-test")
	if err != nil || p == nil || p.Input != "3.00" {
		t.Fatalf("the price must be untouched by forbidden calls, got %+v, %v", p, err)
	}
}

func TestBuilderPrices_UpsertListDelete(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	w := callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", builderPriceBody)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	saved := decodeJSON[BuilderModelPrice](t, w)
	if saved.Input != "3.00" || saved.Output != "15.00" || saved.CachedInput == nil || *saved.CachedInput != "0.30" || saved.CacheWrite != nil {
		t.Fatalf("saved price = %+v (cached %v, write %v)", saved, saved.CachedInput, saved.CacheWrite)
	}
	if saved.UpdatedBy == nil || *saved.UpdatedBy != super.ID {
		t.Errorf("updated_by = %v, want the superadmin", saved.UpdatedBy)
	}

	// Sending null for an optional price clears it; the row is replaced, not merged.
	w = callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x",
		`{"provider":"openai_compatible","model":"gpt-test","input_per_mtok":"2.5","cached_input_per_mtok":null,"cache_write_per_mtok":"3.125","output_per_mtok":"10"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("second PUT: %d %s", w.Code, w.Body.String())
	}
	saved = decodeJSON[BuilderModelPrice](t, w)
	if saved.Input != "2.50" || saved.CachedInput != nil || saved.CacheWrite == nil || *saved.CacheWrite != "3.125" {
		t.Fatalf("replaced price = %+v", saved)
	}
	if n := builderPriceRowCount(t, pool); n != 1 {
		t.Fatalf("price rows = %d, want 1", n)
	}

	list := decodeJSON[builderPricesResponse](t, callBuilder(t, h.ListBuilderPricesConfig, super, http.MethodGet, "/x", ""))
	if list.Currency != "USD" || len(list.Prices) != 1 || list.Prices[0].Model != "gpt-test" {
		t.Fatalf("list = %+v", list)
	}

	if w := callBuilder(t, h.DeleteBuilderPriceConfig, super, http.MethodDelete, "/x?provider=openai_compatible&model=gpt-test", ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", w.Code, w.Body.String())
	}
	if w := callBuilder(t, h.DeleteBuilderPriceConfig, super, http.MethodDelete, "/x?provider=openai_compatible&model=gpt-test", ""); w.Code != http.StatusNotFound {
		t.Errorf("second DELETE: %d, want 404", w.Code)
	}
	if n := builderPriceRowCount(t, pool); n != 0 {
		t.Errorf("price rows after delete = %d", n)
	}
}

// Gateways name models like "org/model:tag"; the key travels in the body and
// the query string so such names round-trip.
func TestBuilderPrices_ModelNameWithSlash(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	const model = "meta-llama/llama-3.1:70b free"

	body := `{"provider":"openai_compatible","model":"` + model + `","input_per_mtok":"0","cached_input_per_mtok":null,"cache_write_per_mtok":null,"output_per_mtok":"0"}`
	if w := callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", body); w.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	p, err := GetBuilderPrice(context.Background(), pool, "openai_compatible", model)
	if err != nil || p == nil || p.Input != "0.00" {
		t.Fatalf("stored price = %+v, %v (a zero price is a real price)", p, err)
	}
	target := "/x?provider=openai_compatible&model=" + url.QueryEscape(model)
	if w := callBuilder(t, h.DeleteBuilderPriceConfig, super, http.MethodDelete, target, ""); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", w.Code, w.Body.String())
	}
}

func TestBuilderPrices_InvalidValuesRejected(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	bad := map[string]string{
		"unknown provider": `{"provider":"nope","model":"m","input_per_mtok":"1","output_per_mtok":"1"}`,
		"empty model":      `{"provider":"openai_compatible","model":"  ","input_per_mtok":"1","output_per_mtok":"1"}`,
		"negative":         `{"provider":"openai_compatible","model":"m","input_per_mtok":"-1","output_per_mtok":"1"}`,
		"seven decimals":   `{"provider":"openai_compatible","model":"m","input_per_mtok":"1.0000001","output_per_mtok":"1"}`,
		"exponent":         `{"provider":"openai_compatible","model":"m","input_per_mtok":"1e3","output_per_mtok":"1"}`,
		"missing output":   `{"provider":"openai_compatible","model":"m","input_per_mtok":"1"}`,
		"bad optional":     `{"provider":"openai_compatible","model":"m","input_per_mtok":"1","cached_input_per_mtok":"x","output_per_mtok":"1"}`,
		"control char":     `{"provider":"openai_compatible","model":"m\u0007","input_per_mtok":"1","output_per_mtok":"1"}`,
	}
	for name, body := range bad {
		w := callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
	if n := builderPriceRowCount(t, pool); n != 0 {
		t.Errorf("rejected bodies created %d price rows", n)
	}
}

func TestBuilderPrices_AuditCarriesOldAndNewValues(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", builderPriceBody)
	callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", strings.Replace(builderPriceBody, `"3"`, `"4.5"`, 1))
	callBuilder(t, h.DeleteBuilderPriceConfig, super, http.MethodDelete, "/x?provider=openai_compatible&model=gpt-test", "")

	updates, _, err := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: "builder.price.update"})
	if err != nil || len(updates) != 2 {
		t.Fatalf("update audit rows = %d, %v; want 2", len(updates), err)
	}
	var second string
	for _, e := range updates {
		if strings.Contains(string(e.Metadata), "4.50") {
			second = string(e.Metadata)
		}
	}
	if !strings.Contains(second, "3.00") || !strings.Contains(second, "4.50") {
		t.Errorf("the second update must carry the old (3.00) and new (4.50) input price: %s", second)
	}
	deletes, _, err := ListAuditLog(context.Background(), pool, AuditLogFilter{Action: "builder.price.delete"})
	if err != nil || len(deletes) != 1 || !strings.Contains(string(deletes[0].Metadata), "4.50") {
		t.Fatalf("delete audit rows = %+v, %v", deletes, err)
	}
}

// Models used recently with no price row are listed, so the operator sees what
// is missing; a priced model and an old row are not.
func TestBuilderPrices_UnpricedModelsFromLedger(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	super := mustCreateSuperadmin(t, pool)
	ctx := context.Background()

	insert := func(provider, model, age string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO zeep_system.ai_usage_ledger (user_id, feature, provider, model, input_tokens, output_tokens, created_at)
			 VALUES ($1::uuid, 'builder', $2, $3, 1, 1, now() - $4::interval)`, super.ID, provider, model, age); err != nil {
			t.Fatalf("insert ledger row: %v", err)
		}
	}
	insert("openai_compatible", "gpt-test", "1 hour")     // priced below
	insert("openai_compatible", "gpt-unpriced", "1 hour") // listed
	insert("openai_compatible", "gpt-unpriced", "2 days") // listed, same model
	insert("openai_compatible", "old-model", "90 days")   // outside the window
	insert("openai", "gpt-4.1", "1 hour")                 // priced by the catalog
	insert("openai", "o3-mini", "1 hour")                 // not in the catalog: listed
	callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", builderPriceBody)
	// A price row of a catalog provider (left from before the catalog) does
	// not price the model, so o3-mini stays listed.
	if _, err := pool.Exec(ctx, `INSERT INTO zeep_system.ai_model_prices (provider, model, input_per_mtok, output_per_mtok)
		 VALUES ('openai', 'o3-mini', 1, 1)`); err != nil {
		t.Fatalf("seed stale price: %v", err)
	}

	list := decodeJSON[builderPricesResponse](t, callBuilder(t, h.ListBuilderPricesConfig, super, http.MethodGet, "/x", ""))
	got := list.UnpricedModels
	if len(got) != 2 || got[0].Model != "gpt-unpriced" || got[0].Calls != 2 || got[1].Provider != "openai" || got[1].Model != "o3-mini" {
		t.Fatalf("unpriced models = %+v, want gpt-unpriced (2 calls) and openai/o3-mini", got)
	}
	// The stale catalog-provider row is not listed as a price.
	for _, p := range list.Prices {
		if p.Provider == "openai" {
			t.Errorf("a catalog provider row must not be listed: %+v", p)
		}
	}
}

// The prices of the catalog providers ship with Orbit: they cannot be
// written or deleted through the API.
func TestBuilderPrices_CatalogProvidersAreReadOnly(t *testing.T) {
	pool, h := builderPricesTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	for _, provider := range []string{"openai", "anthropic"} {
		body := strings.Replace(builderPriceBody, `"provider":"openai_compatible"`, `"provider":"`+provider+`"`, 1)
		w := callBuilder(t, h.UpsertBuilderPriceConfig, super, http.MethodPut, "/x", body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errCatalogPriceReadOnly) {
			t.Errorf("PUT %s: got %d %s, want 400 %q", provider, w.Code, w.Body.String(), errCatalogPriceReadOnly)
		}
		w = callBuilder(t, h.DeleteBuilderPriceConfig, super, http.MethodDelete, "/x?provider="+provider+"&model=gpt-test", "")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errCatalogPriceReadOnly) {
			t.Errorf("DELETE %s: got %d %s, want 400", provider, w.Code, w.Body.String())
		}
	}
}

func TestBuilderSettings_CurrencyAndMoneyCaps(t *testing.T) {
	pool, h := builderSettingsTestPool(t)
	super := mustCreateSuperadmin(t, pool)

	// Defaults before any save.
	s := decodeJSON[BuilderSettings](t, callBuilder(t, h.GetBuilderSettingsConfig, super, http.MethodGet, "/x", ""))
	if s.CostCurrency != "USD" || s.CostCurrencyChangedAt != nil || s.SessionCostCapMicros != 0 || s.UserMonthlyCostCapMicros != 0 {
		t.Fatalf("defaults = %+v", s)
	}

	// A first save that does not mention the currency keeps USD and no change date.
	s = decodeJSON[BuilderSettings](t, callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"session_cost_cap_micros":5000000}`))
	if s.CostCurrency != "USD" || s.CostCurrencyChangedAt != nil || s.SessionCostCapMicros != 5_000_000 {
		t.Fatalf("after cap save = %+v", s)
	}

	// Saving the same currency is not a change; a different one is, and is normalized.
	s = decodeJSON[BuilderSettings](t, callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"cost_currency":"usd"}`))
	if s.CostCurrency != "USD" || s.CostCurrencyChangedAt != nil {
		t.Fatalf("same currency must not set the change date: %+v", s)
	}
	s = decodeJSON[BuilderSettings](t, callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"cost_currency":" brl ","user_monthly_cost_cap_micros":90000000}`))
	if s.CostCurrency != "BRL" || s.CostCurrencyChangedAt == nil || s.UserMonthlyCostCapMicros != 90_000_000 || s.SessionCostCapMicros != 5_000_000 {
		t.Fatalf("after currency change = %+v", s)
	}

	// An absent key keeps the value; 0 clears a cap.
	s = decodeJSON[BuilderSettings](t, callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", `{"session_cost_cap_micros":0}`))
	if s.SessionCostCapMicros != 0 || s.UserMonthlyCostCapMicros != 90_000_000 || s.CostCurrency != "BRL" {
		t.Fatalf("after clearing one cap = %+v", s)
	}

	for name, body := range map[string]string{
		"two letters":  `{"cost_currency":"US"}`,
		"digits":       `{"cost_currency":"U5D"}`,
		"negative cap": `{"session_cost_cap_micros":-1}`,
		"huge cap":     `{"user_monthly_cost_cap_micros":9000000000000000000}`,
	} {
		if w := callBuilder(t, h.UpsertBuilderSettingsConfig, super, http.MethodPut, "/x", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}
}
