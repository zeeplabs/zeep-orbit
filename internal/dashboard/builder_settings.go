package dashboard

// builder_settings.go — HTTP handlers for the AI Frontend Builder's global
// settings and company persona + rules (ai-frontend-builder T22,
// AFB-01/02/25). Every route is superadmin-only (ActionManageIntegrations,
// the same gate as the AI provider config) and mounted by the router under
// /dashboard:
//
//	GET  /api/builder/settings          GetBuilderSettingsConfig
//	PUT  /api/builder/settings          UpsertBuilderSettingsConfig
//	GET  /api/builder/prompt            GetBuilderPromptConfig
//	PUT  /api/builder/prompt            UpsertBuilderPromptConfig
//	GET  /api/builder/prompt/versions   ListBuilderPromptVersionsConfig
//
// The settings carry no secrets (the provider key lives in ai_providers), and
// persona/rules are company-authored text that is never logged: the audit rows
// record sizes and version ids only.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zeeplabs/zeep-orbit/internal/agent/catalog"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// Bounds for the numeric settings. They protect the platform from a typo (a
// 10-hour turn, a one-token context window), not from a deliberate choice: the
// superadmin owns the budget. Zero caps are valid and mean "unset".
const (
	builderMinContextWindowTokens = 4096
	builderMaxContextWindowTokens = 2_000_000
	builderMaxTokenCap            = int64(1_000_000_000_000)
	// builderMaxCostCapMicros is 1 billion units of currency, far above any
	// real cap and well inside what a JSON number carries exactly.
	builderMaxCostCapMicros    = int64(1_000_000_000_000_000)
	builderMinMaxSteps         = 1
	builderMaxMaxSteps         = 200
	builderMinMaxFixIterations = 1
	builderMaxMaxFixIterations = 20
	builderMinMaxTurnSeconds   = 10
	builderMaxMaxTurnSeconds   = 3600

	// builderPromptBodyLimit bounds the raw PUT body: 8000 characters of
	// 4-byte runes, JSON-escaped, with headroom.
	builderPromptBodyLimit = 128 << 10

	builderPromptVersionsDefaultLimit = 20
	builderPromptVersionsMaxLimit     = 100
)

// builderProviders are the ai_providers rows the builder can run on today.
var builderProviders = map[string]bool{
	aiProviderOpenAI:           true,
	aiProviderOpenAICompatible: true,
	aiProviderAnthropic:        true,
}

// requireBuilderAdmin resolves the caller and enforces superadmin. It writes
// the 401/403 itself and returns ok=false in that case.
func (h *Handler) requireBuilderAdmin(w http.ResponseWriter, r *http.Request) (*DashboardUser, bool) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, false
	}
	if !HasPlatformPermission(user.Role, ActionManageIntegrations) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return nil, false
	}
	return user, true
}

// GetBuilderSettingsConfig handles GET /dashboard/api/builder/settings.
// Before the first save it returns the defaults (disabled, caps 0,
// configured=false) without writing a row.
func (h *Handler) GetBuilderSettingsConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBuilderAdmin(w, r); !ok {
		return
	}
	s, err := GetBuilderSettings(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// UpsertBuilderSettingsConfig handles PUT /dashboard/api/builder/settings.
// Merge-on-absent-key: only the fields present in the body change; to zero a
// cap send 0. The first PUT creates the singleton row.
func (h *Handler) UpsertBuilderSettingsConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body BuilderSettingsInput
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if err := validateBuilderSettingsInput(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if msg, err := resolveBuilderPlanningModel(r.Context(), h.pool, &body); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	} else if msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	s, err := UpsertBuilderSettings(r.Context(), h.pool, user.ID, body)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update builder settings", err)
		return
	}
	writeJSON(w, http.StatusOK, s)

	meta, _ := json.Marshal(body) // only the fields that were sent are non-null; no secrets here
	h.audit(r.Context(), user.ID, user.Email, "builder.settings.update", "builder_settings", "global", "builder settings", meta, r.RemoteAddr)
}

func validateBuilderSettingsInput(in *BuilderSettingsInput) error {
	if in.Enabled == nil && in.Provider == nil && in.ContextWindowTokens == nil && in.SessionTokenCap == nil &&
		in.UserMonthlyTokenCap == nil && in.MaxStepsPerTurn == nil && in.MaxFixIterations == nil && in.MaxTurnSeconds == nil &&
		in.BackendApprovalMode == nil && in.PublishRLSGate == nil &&
		in.CostCurrency == nil && in.SessionCostCapMicros == nil && in.UserMonthlyCostCapMicros == nil &&
		in.PlanningMode == nil && in.PlanningModel == nil {
		return errors.New("no settings to update")
	}
	if in.Provider != nil {
		p := strings.TrimSpace(*in.Provider)
		if !builderProviders[p] {
			return errors.New("provider must be 'openai', 'openai_compatible' or 'anthropic'")
		}
		in.Provider = &p
	}
	if v := in.ContextWindowTokens; v != nil && (*v < builderMinContextWindowTokens || *v > builderMaxContextWindowTokens) {
		return fmt.Errorf("context_window_tokens must be between %d and %d", builderMinContextWindowTokens, builderMaxContextWindowTokens)
	}
	if v := in.SessionTokenCap; v != nil && (*v < 0 || *v > builderMaxTokenCap) {
		return fmt.Errorf("session_token_cap must be between 0 and %d (0 = unset)", builderMaxTokenCap)
	}
	if v := in.UserMonthlyTokenCap; v != nil && (*v < 0 || *v > builderMaxTokenCap) {
		return fmt.Errorf("user_monthly_token_cap must be between 0 and %d (0 = unset)", builderMaxTokenCap)
	}
	if v := in.MaxStepsPerTurn; v != nil && (*v < builderMinMaxSteps || *v > builderMaxMaxSteps) {
		return fmt.Errorf("max_steps_per_turn must be between %d and %d", builderMinMaxSteps, builderMaxMaxSteps)
	}
	if v := in.MaxFixIterations; v != nil && (*v < builderMinMaxFixIterations || *v > builderMaxMaxFixIterations) {
		return fmt.Errorf("max_fix_iterations must be between %d and %d", builderMinMaxFixIterations, builderMaxMaxFixIterations)
	}
	if v := in.MaxTurnSeconds; v != nil && (*v < builderMinMaxTurnSeconds || *v > builderMaxMaxTurnSeconds) {
		return fmt.Errorf("max_turn_seconds must be between %d and %d", builderMinMaxTurnSeconds, builderMaxMaxTurnSeconds)
	}
	// "auto" and "block" are the column's other valid values, but nothing
	// reads either setting yet (ai-builder-backend B07, B09): refuse them
	// rather than store a mode that changes nothing, which for "block" would
	// read as a publish protection that does not exist.
	if v := in.BackendApprovalMode; v != nil {
		m := strings.TrimSpace(*v)
		switch m {
		case BuilderApprovalAsk:
		case BuilderApprovalAuto:
			return errors.New("backend_approval_mode 'auto' is not supported yet; every backend change waits for the user ('ask')")
		default:
			return errors.New("backend_approval_mode must be 'ask'")
		}
		in.BackendApprovalMode = &m
	}
	if v := in.PublishRLSGate; v != nil {
		g := strings.TrimSpace(*v)
		switch g {
		case BuilderRLSGateWarn:
		case BuilderRLSGateBlock:
			return errors.New("publish_rls_gate 'block' is not supported yet; publishing does not check open tables")
		default:
			return errors.New("publish_rls_gate must be 'warn'")
		}
		in.PublishRLSGate = &g
	}
	// Costs are in the catalog's currency (ai-model-catalog AMC-10). The field
	// is still accepted with that value so older clients keep working.
	if v := in.CostCurrency; v != nil {
		c := strings.ToUpper(strings.TrimSpace(*v))
		if c != catalog.Currency {
			return errors.New("cost_currency must be USD")
		}
		in.CostCurrency = &c
	}
	if v := in.SessionCostCapMicros; v != nil && (*v < 0 || *v > builderMaxCostCapMicros) {
		return fmt.Errorf("session_cost_cap_micros must be between 0 and %d (0 = no cap)", builderMaxCostCapMicros)
	}
	if v := in.UserMonthlyCostCapMicros; v != nil && (*v < 0 || *v > builderMaxCostCapMicros) {
		return fmt.Errorf("user_monthly_cost_cap_micros must be between 0 and %d (0 = no cap)", builderMaxCostCapMicros)
	}
	if v := in.PlanningMode; v != nil {
		m := strings.TrimSpace(*v)
		if m != BuilderPlanningRequired && m != BuilderPlanningSuggested && m != BuilderPlanningOff {
			return errors.New("planning_mode must be 'required', 'suggested' or 'off'")
		}
		in.PlanningMode = &m
	}
	return nil
}

// builderPlanningModelMaxLen bounds the planning model of an openai_compatible
// provider, whose models the catalog does not cover.
const builderPlanningModelMaxLen = 200

const errPlanningModelNotInCatalog = "planning_model must be a model of the selected provider's catalog"

// resolveBuilderPlanningModel checks in.PlanningModel against the provider the
// settings will have after this save, and clears a saved planning model when
// the provider changes (ai-builder-economy AEC-03, AEC-04). It returns a
// client error message, or "" when the input may be saved. The model is the
// provider row's model when empty, so "" is always valid.
func resolveBuilderPlanningModel(ctx context.Context, pool *db.Pool, in *BuilderSettingsInput) (string, error) {
	if in.PlanningModel == nil && in.Provider == nil {
		return "", nil
	}
	cur, err := GetBuilderSettings(ctx, pool)
	if err != nil {
		return "", err
	}
	provider := cur.Provider
	if in.Provider != nil {
		provider = *in.Provider
	}
	if in.PlanningModel == nil {
		// The operator did not touch the field but the provider moved: the
		// saved model belonged to the old provider, so it is cleared, not kept.
		if provider != cur.Provider && cur.PlanningModel != "" {
			empty := ""
			in.PlanningModel = &empty
		}
		return "", nil
	}
	model := strings.TrimSpace(*in.PlanningModel)
	in.PlanningModel = &model
	if model == "" {
		return "", nil
	}
	saved := cur.PlanningModel
	if provider != cur.Provider {
		saved = "" // a deprecated model stays valid only for the provider that already holds it
	}
	if catalog.Covers(provider) {
		if !catalogModelChoosable(provider, model, saved) {
			return errPlanningModelNotInCatalog, nil
		}
		return "", nil
	}
	if len(model) > builderPlanningModelMaxLen || strings.ContainsFunc(model, unicode.IsControl) {
		return "planning_model must be at most 200 characters without control characters", nil
	}
	return "", nil
}

// builderPromptResponse is the API view of the active persona + rules. With no
// saved version, id/created_* are empty and persona/rules are "".
type builderPromptResponse struct {
	ID             string     `json:"id"`
	Persona        string     `json:"persona"`
	Rules          string     `json:"rules"`
	CreatedBy      string     `json:"created_by"`
	CreatedByEmail string     `json:"created_by_email"`
	CreatedAt      *time.Time `json:"created_at"`
	// Chars is the size of the company layer (persona + rules) and MaxChars its
	// save-time cap, so the UI can show a counter.
	Chars    int `json:"chars"`
	MaxChars int `json:"max_chars"`
	// Changed is set by PUT only: false when the save matched the active
	// version and no new version was created.
	Changed *bool `json:"changed,omitempty"`
}

func newBuilderPromptResponse(v *BuilderPromptVersion) builderPromptResponse {
	resp := builderPromptResponse{MaxChars: BuilderPromptMaxChars}
	if v == nil {
		return resp
	}
	created := v.CreatedAt
	resp.ID, resp.Persona, resp.Rules = v.ID, v.Persona, v.Rules
	resp.CreatedBy, resp.CreatedByEmail, resp.CreatedAt = v.CreatedBy, v.CreatedByEmail, &created
	resp.Chars = builderPromptChars(v.Persona, v.Rules)
	return resp
}

// GetBuilderPromptConfig handles GET /dashboard/api/builder/prompt: the
// active persona + rules (the newest version).
func (h *Handler) GetBuilderPromptConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBuilderAdmin(w, r); !ok {
		return
	}
	v, err := GetActiveBuilderPrompt(r.Context(), h.pool)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, newBuilderPromptResponse(v))
}

// UpsertBuilderPromptConfig handles PUT /dashboard/api/builder/prompt. It
// appends a new version (author = caller) built from the active one merged
// with the body (absent field = keep, "" = clear). Persona + rules larger than
// BuilderPromptMaxChars are rejected with 400 and nothing is stored. A save
// identical to the active version creates no row and no audit entry
// (changed=false).
func (h *Handler) UpsertBuilderPromptConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireBuilderAdmin(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, builderPromptBodyLimit)
	var body BuilderPromptInput
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Persona == nil && body.Rules == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona or rules is required"})
		return
	}

	// The size cap applies to the merged content, so it is validated inside
	// the store, against what would actually be saved.
	v, created, err := SaveBuilderPrompt(r.Context(), h.pool, user.ID, body)
	if err != nil {
		if errors.Is(err, ErrBuilderPromptTooLong) || errors.Is(err, errBuilderPromptInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "failed to save builder prompt", err)
		return
	}

	resp := newBuilderPromptResponse(v)
	resp.Changed = &created
	writeJSON(w, http.StatusOK, resp)

	if created {
		meta, _ := json.Marshal(map[string]any{
			"persona_chars": utf8.RuneCountInString(v.Persona),
			"rules_chars":   utf8.RuneCountInString(v.Rules),
			"chars":         resp.Chars,
		})
		h.audit(r.Context(), user.ID, user.Email, "builder.prompt.update", "builder_prompt", v.ID, "builder prompt", meta, r.RemoteAddr)
	}
}

// ListBuilderPromptVersionsConfig handles GET
// /dashboard/api/builder/prompt/versions?limit=&offset= — the full history,
// newest first, each with its author.
func (h *Handler) ListBuilderPromptVersionsConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireBuilderAdmin(w, r); !ok {
		return
	}
	limit := parseBuilderPageParam(r, "limit", builderPromptVersionsDefaultLimit, 1, builderPromptVersionsMaxLimit)
	offset := parseBuilderPageParam(r, "offset", 0, 0, 1<<30)

	versions, total, err := ListBuilderPromptVersions(r.Context(), h.pool, limit, offset)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions, "total": total})
}

// parseBuilderPageParam reads an integer query parameter, falling back to def
// when absent or malformed and clamping to [min, max].
func parseBuilderPageParam(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
