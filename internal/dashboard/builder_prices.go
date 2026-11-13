package dashboard

// builder_prices.go — operator-maintained model prices (ai-builder-costs C03,
// ABC-06 to ABC-10). Prices are per million tokens, in the installation
// currency (builder_settings.cost_currency), and travel as decimal strings so
// no floating point touches money. Orbit ships no prices and fetches none.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

const (
	builderPriceMaxModelLen = 200
	// builderUnpricedWindow is how far back the ledger is searched for models
	// that were used and have no price.
	builderUnpricedWindow = 30 * 24 * time.Hour
)

// BuilderModelPrice is one price row. The four prices are decimal strings per
// million tokens; CachedInput and CacheWrite are null when not set, in which
// case that class is billed at the input price.
type BuilderModelPrice struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Input       string    `json:"input_per_mtok"`
	CachedInput *string   `json:"cached_input_per_mtok"`
	CacheWrite  *string   `json:"cache_write_per_mtok"`
	Output      string    `json:"output_per_mtok"`
	UpdatedBy   *string   `json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BuilderUnpricedModel is a provider and model that appear in the usage
// ledger recently and have no price row.
type BuilderUnpricedModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Calls    int64  `json:"calls"`
}

// builderPriceInput is the body of PUT /api/builder/prices. The key travels
// in the body because model names contain slashes on many gateways. All four
// price keys are always sent: null clears an optional price.
type builderPriceInput struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Input       string  `json:"input_per_mtok"`
	CachedInput *string `json:"cached_input_per_mtok"`
	CacheWrite  *string `json:"cache_write_per_mtok"`
	Output      string  `json:"output_per_mtok"`
}

// builderPricesResponse is the body of GET /api/builder/prices.
type builderPricesResponse struct {
	Currency       string                 `json:"currency"`
	Prices         []BuilderModelPrice    `json:"prices"`
	UnpricedModels []BuilderUnpricedModel `json:"unpriced_models"`
}

// normalizeBuilderPriceKey trims and validates the provider kind and model of
// a price row.
func normalizeBuilderPriceKey(provider, model string) (string, string, error) {
	provider, model = strings.TrimSpace(provider), strings.TrimSpace(model)
	if !builderProviders[provider] {
		return "", "", errors.New("provider is not a known provider kind")
	}
	if model == "" {
		return "", "", errors.New("model is required")
	}
	if len(model) > builderPriceMaxModelLen || !utf8.ValidString(model) || strings.ContainsFunc(model, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", "", fmt.Errorf("model must be at most %d characters of printable text", builderPriceMaxModelLen)
	}
	return provider, model, nil
}

// parseBuilderPriceInput validates a price body and returns the normalized
// key and the prices as canonical decimal strings.
func parseBuilderPriceInput(in builderPriceInput) (*BuilderModelPrice, error) {
	provider, model, err := normalizeBuilderPriceKey(in.Provider, in.Model)
	if err != nil {
		return nil, err
	}
	canon := func(field, v string) (string, error) {
		micros, err := store.ParsePrice(v)
		if err != nil {
			return "", fmt.Errorf("%s must be a non-negative decimal with at most 6 decimal places", field)
		}
		return store.FormatPrice(micros), nil
	}
	p := &BuilderModelPrice{Provider: provider, Model: model}
	if p.Input, err = canon("input_per_mtok", in.Input); err != nil {
		return nil, err
	}
	if p.Output, err = canon("output_per_mtok", in.Output); err != nil {
		return nil, err
	}
	if in.CachedInput != nil {
		v, err := canon("cached_input_per_mtok", *in.CachedInput)
		if err != nil {
			return nil, err
		}
		p.CachedInput = &v
	}
	if in.CacheWrite != nil {
		v, err := canon("cache_write_per_mtok", *in.CacheWrite)
		if err != nil {
			return nil, err
		}
		p.CacheWrite = &v
	}
	return p, nil
}

const builderPriceCols = `provider, model, input_per_mtok::text, cached_input_per_mtok::text,
	cache_write_per_mtok::text, output_per_mtok::text, updated_by::text, updated_at`

func scanBuilderPrice(row pgx.Row) (*BuilderModelPrice, error) {
	var p BuilderModelPrice
	if err := row.Scan(&p.Provider, &p.Model, &p.Input, &p.CachedInput, &p.CacheWrite, &p.Output, &p.UpdatedBy, &p.UpdatedAt); err != nil {
		return nil, err
	}
	// NUMERIC(18,6)::text always carries six decimals; show the canonical form.
	for _, f := range []*string{&p.Input, &p.Output, p.CachedInput, p.CacheWrite} {
		if f == nil {
			continue
		}
		micros, err := store.ParsePrice(*f)
		if err != nil {
			return nil, fmt.Errorf("stored price %q: %w", *f, err)
		}
		*f = store.FormatPrice(micros)
	}
	return &p, nil
}

// ListBuilderPrices returns every price row, ordered by provider and model.
func ListBuilderPrices(ctx context.Context, pool *db.Pool) ([]BuilderModelPrice, error) {
	rows, err := pool.Query(ctx, `SELECT `+builderPriceCols+` FROM zeep_system.ai_model_prices ORDER BY provider, model`)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list builder prices: %w", err)
	}
	defer rows.Close()
	out := []BuilderModelPrice{}
	for rows.Next() {
		p, err := scanBuilderPrice(rows)
		if err != nil {
			return nil, fmt.Errorf("dashboard: list builder prices: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetBuilderPrice returns one price row, or nil when it does not exist.
func GetBuilderPrice(ctx context.Context, pool *db.Pool, provider, model string) (*BuilderModelPrice, error) {
	p, err := scanBuilderPrice(pool.QueryRow(ctx,
		`SELECT `+builderPriceCols+` FROM zeep_system.ai_model_prices WHERE provider = $1 AND model = $2`, provider, model))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dashboard: get builder price: %w", err)
	}
	return p, nil
}

// UpsertBuilderPrice writes one price row. Costs already recorded keep the
// price they were computed with.
func UpsertBuilderPrice(ctx context.Context, pool *db.Pool, updatedBy string, p *BuilderModelPrice) (*BuilderModelPrice, error) {
	out, err := scanBuilderPrice(pool.QueryRow(ctx,
		`INSERT INTO zeep_system.ai_model_prices
		   (provider, model, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok, output_per_mtok, updated_by, updated_at)
		 VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::uuid, now())
		 ON CONFLICT (provider, model) DO UPDATE SET
		   input_per_mtok        = EXCLUDED.input_per_mtok,
		   cached_input_per_mtok = EXCLUDED.cached_input_per_mtok,
		   cache_write_per_mtok  = EXCLUDED.cache_write_per_mtok,
		   output_per_mtok       = EXCLUDED.output_per_mtok,
		   updated_by            = EXCLUDED.updated_by,
		   updated_at            = now()
		 RETURNING `+builderPriceCols,
		p.Provider, p.Model, p.Input, p.CachedInput, p.CacheWrite, p.Output, updatedBy))
	if err != nil {
		return nil, fmt.Errorf("dashboard: upsert builder price: %w", err)
	}
	return out, nil
}

// DeleteBuilderPrice removes one price row and reports whether it existed.
func DeleteBuilderPrice(ctx context.Context, pool *db.Pool, provider, model string) (bool, error) {
	tag, err := pool.Exec(ctx, `DELETE FROM zeep_system.ai_model_prices WHERE provider = $1 AND model = $2`, provider, model)
	if err != nil {
		return false, fmt.Errorf("dashboard: delete builder price: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListBuilderUnpricedModels returns the models used since `since` that have
// no price row, most used first.
func ListBuilderUnpricedModels(ctx context.Context, pool *db.Pool, since time.Time) ([]BuilderUnpricedModel, error) {
	rows, err := pool.Query(ctx,
		`SELECT l.provider, l.model, count(*)
		   FROM zeep_system.ai_usage_ledger l
		  WHERE l.created_at >= $1
		    AND NOT EXISTS (SELECT 1 FROM zeep_system.ai_model_prices p WHERE p.provider = l.provider AND p.model = l.model)
		  GROUP BY l.provider, l.model
		  ORDER BY count(*) DESC, l.provider, l.model
		  LIMIT 50`, since)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list unpriced models: %w", err)
	}
	defer rows.Close()
	out := []BuilderUnpricedModel{}
	for rows.Next() {
		var m BuilderUnpricedModel
		if err := rows.Scan(&m.Provider, &m.Model, &m.Calls); err != nil {
			return nil, fmt.Errorf("dashboard: list unpriced models: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Handlers (superadmin only)
// ---------------------------------------------------------------------------

// ListBuilderPricesConfig handles GET /api/builder/prices.
func (h *Handler) ListBuilderPricesConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBuilderAdmin(w, r); !ok {
		return
	}
	settings, err := GetBuilderSettings(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load builder prices", err)
		return
	}
	prices, err := ListBuilderPrices(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load builder prices", err)
		return
	}
	unpriced, err := ListBuilderUnpricedModels(r.Context(), h.pool, time.Now().Add(-builderUnpricedWindow))
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to load builder prices", err)
		return
	}
	writeJSON(w, http.StatusOK, builderPricesResponse{Currency: settings.CostCurrency, Prices: prices, UnpricedModels: unpriced})
}

// UpsertBuilderPriceConfig handles PUT /api/builder/prices.
func (h *Handler) UpsertBuilderPriceConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body builderPriceInput
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	in, err := parseBuilderPriceInput(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	before, err := GetBuilderPrice(r.Context(), h.pool, in.Provider, in.Model)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update the price", err)
		return
	}
	saved, err := UpsertBuilderPrice(r.Context(), h.pool, user.ID, in)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update the price", err)
		return
	}
	writeJSON(w, http.StatusOK, saved)

	// Prices are not secrets: the audit row carries the old and new values.
	meta, _ := json.Marshal(map[string]any{"before": builderPriceAudit(before), "after": builderPriceAudit(saved)})
	h.audit(r.Context(), user.ID, user.Email, "builder.price.update", "builder_price", in.Provider+"/"+in.Model, in.Model, meta, r.RemoteAddr)
}

// DeleteBuilderPriceConfig handles DELETE /api/builder/prices?provider=&model=.
func (h *Handler) DeleteBuilderPriceConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}
	provider, model, err := normalizeBuilderPriceKey(r.URL.Query().Get("provider"), r.URL.Query().Get("model"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	before, err := GetBuilderPrice(r.Context(), h.pool, provider, model)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to delete the price", err)
		return
	}
	if before == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "price not found"})
		return
	}
	if _, err := DeleteBuilderPrice(r.Context(), h.pool, provider, model); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to delete the price", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)

	meta, _ := json.Marshal(map[string]any{"before": builderPriceAudit(before)})
	h.audit(r.Context(), user.ID, user.Email, "builder.price.delete", "builder_price", provider+"/"+model, model, meta, r.RemoteAddr)
}

// builderPriceAudit is the audit view of a price: the four prices only.
func builderPriceAudit(p *BuilderModelPrice) map[string]any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"input_per_mtok": p.Input, "cached_input_per_mtok": p.CachedInput,
		"cache_write_per_mtok": p.CacheWrite, "output_per_mtok": p.Output,
	}
}
