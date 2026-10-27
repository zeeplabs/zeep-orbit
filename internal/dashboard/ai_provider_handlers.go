package dashboard

// ai_provider_handlers.go — HTTP handlers for the global AI provider
// config (GET/PUT /dashboard/api/ai-providers/{provider}).
//
// SPEC_DEVIATION: spec.md/design.md write this endpoint's path as
// "/api/dashboard/ai-providers/{provider}". The router in
// internal/server/server.go mounts every dashboard REST route under
// r.Route("/dashboard", ...) with paths starting at "/api/..." (e.g. the
// existing "/api/config/auth/providers/{provider}" this endpoint mirrors)
// — there is no "/api/dashboard/..." prefix anywhere in the codebase.
// Reason: following the one real routing convention in the repo instead of
// introducing a second, inconsistent prefix. The actual route is
// "/dashboard/api/ai-providers/{provider}".

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Provider names with a functional persistence path.
const (
	aiProviderOpenAI           = "openai"
	aiProviderOpenAICompatible = "openai_compatible"
)

// aiProviderNames is the fixed set of provider names the API accepts —
// the spec's 'openai' | 'gemini' | 'claude' enum (ai-build-chat design.md)
// plus 'openai_compatible' (ai-frontend-builder AFB-01: any server speaking
// the OpenAI Chat Completions wire format, with its own base URL). Any other
// value is rejected as an unknown provider.
var aiProviderNames = map[string]bool{
	aiProviderOpenAI:           true,
	aiProviderOpenAICompatible: true,
	"gemini":                   true,
	"claude":                   true,
}

// aiProviderImplemented reports whether provider has a functional
// persistence path. gemini/claude do not yet (spec Out of Scope).
func aiProviderImplemented(provider string) bool {
	return provider == aiProviderOpenAI || provider == aiProviderOpenAICompatible
}

// GetAIProviderConfig handles GET /dashboard/api/ai-providers/{provider}.
// Any authenticated user may call this (AIBC-04) — the response never
// carries the API key, only {has_key, model, enabled}. gemini/claude have
// no functional persistence path yet (spec Out of Scope), so they always
// report {available: false} instead of the openai shape.
func (h *Handler) GetAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	_, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	provider := chi.URLParam(r, "provider")
	if !aiProviderNames[provider] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown provider"})
		return
	}

	if !aiProviderImplemented(provider) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}

	resp, err := GetAIProvider(r.Context(), h.pool, provider)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// UpsertAIProviderConfig handles PUT /dashboard/api/ai-providers/{provider}.
// Superadmin-only (AIBC-02), gated by ActionManageIntegrations — an AI
// provider is an integration in the same sense GitHub/SSO providers are
// (design.md Tech Decisions), so no new PlatformAction is introduced.
// gemini/claude have no functional persistence path yet and reject with
// 501 before touching the store (AIBC-06).
func (h *Handler) UpsertAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if !HasPlatformPermission(user.Role, ActionManageIntegrations) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	provider := chi.URLParam(r, "provider")
	if !aiProviderNames[provider] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown provider"})
		return
	}
	if !aiProviderImplemented(provider) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "provider not implemented"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var body aiProviderUpsertInput
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	// An empty/blank model (e.g. the dashboard's model select left on
	// "custom" with no text typed in) saved without error, since the store
	// never validated it — every chat turn afterward then failed with a
	// generic "couldn't generate a plan" error, with nothing in the config
	// UI to explain why. Reject it here instead.
	if strings.TrimSpace(body.Model) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model is required"})
		return
	}

	if msg, ok := h.prepareAIProviderBaseURL(w, r, provider, &body); !ok {
		if msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		}
		return
	}

	result, err := UpsertAIProvider(r.Context(), h.pool, provider, &body)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update provider", err)
		return
	}

	writeJSON(w, http.StatusOK, result)

	var meta json.RawMessage
	if provider == aiProviderOpenAICompatible {
		// base_url is not a secret (credentials in the URL are rejected); the
		// key is never written to the audit log.
		meta, _ = json.Marshal(map[string]any{"base_url": result.BaseURL, "model": result.Model, "enabled": result.Enabled})
	}
	h.audit(r.Context(), user.ID, user.Email, "ai_provider.update", "ai_provider", provider, provider, meta, r.RemoteAddr)
}

// prepareAIProviderBaseURL validates body.BaseURL for provider and rewrites it
// to its normalized form. It returns ok=false with the message to send as a
// 400 (msg != ""), or ok=false with msg=="" when it already wrote a response
// (a store failure).
//
//   - openai: no base URL (the stock API). An explicit "" is accepted as a
//     no-op so clients may send the field uniformly; anything else is a 400.
//   - openai_compatible: a base URL is required. Absent keeps the stored one
//     (merge-on-absent-key), so a model-only update works; an explicit "" tries
//     to clear it and is rejected because the provider cannot work without it.
func (h *Handler) prepareAIProviderBaseURL(w http.ResponseWriter, r *http.Request, provider string, body *aiProviderUpsertInput) (msg string, ok bool) {
	if provider == aiProviderOpenAI {
		if body.BaseURL != nil {
			if strings.TrimSpace(*body.BaseURL) != "" {
				return "base_url is only supported by the openai_compatible provider", false
			}
			empty := ""
			body.BaseURL = &empty
		}
		return "", true
	}

	// openai_compatible
	if body.BaseURL != nil {
		normalized, err := normalizeAIProviderBaseURL(*body.BaseURL)
		if err != nil {
			return err.Error(), false
		}
		body.BaseURL = &normalized
		if normalized == "" {
			return "base_url is required for the openai_compatible provider", false
		}
		return "", true
	}

	existing, err := GetAIProvider(r.Context(), h.pool, provider)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update provider", err)
		return "", false
	}
	if existing.BaseURL == "" {
		return "base_url is required for the openai_compatible provider", false
	}
	return "", true
}
