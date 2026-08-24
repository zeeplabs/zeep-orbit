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
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// aiProviderNames is the fixed set of provider names the API accepts —
// mirrors the spec's 'openai' | 'gemini' | 'claude' enum (design.md Data
// Models). Any other value is rejected as an unknown provider.
var aiProviderNames = map[string]bool{
	"openai": true,
	"gemini": true,
	"claude": true,
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

	if provider != "openai" {
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
	if provider != "openai" {
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

	result, err := UpsertAIProvider(r.Context(), h.pool, provider, &body)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "failed to update provider", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
	h.audit(r.Context(), user.ID, user.Email, "ai_provider.update", "ai_provider", provider, provider, nil, r.RemoteAddr)
}
