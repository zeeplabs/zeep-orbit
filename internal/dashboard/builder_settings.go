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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Bounds for the numeric settings. They protect the platform from a typo (a
// 10-hour turn, a one-token context window), not from a deliberate choice: the
// superadmin owns the budget. Zero caps are valid and mean "unset".
const (
	builderMinContextWindowTokens = 4096
	builderMaxContextWindowTokens = 2_000_000
	builderMaxTokenCap            = int64(1_000_000_000_000)
	builderMinMaxSteps            = 1
	builderMaxMaxSteps            = 200
	builderMinMaxFixIterations    = 1
	builderMaxMaxFixIterations    = 20
	builderMinMaxTurnSeconds      = 10
	builderMaxMaxTurnSeconds      = 3600

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
		in.UserMonthlyTokenCap == nil && in.MaxStepsPerTurn == nil && in.MaxFixIterations == nil && in.MaxTurnSeconds == nil {
		return errors.New("no settings to update")
	}
	if in.Provider != nil {
		p := strings.TrimSpace(*in.Provider)
		if !builderProviders[p] {
			return errors.New("provider must be 'openai' or 'openai_compatible'")
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
	return nil
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
