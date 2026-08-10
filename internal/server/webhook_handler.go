package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// WebhookHandler serves the public, unauthenticated (token-in-URL) inbound
// webhook route — no dashboard session, no end-user JWT. See design.md
// Architecture Overview and Components > Public Webhook Handler.
type WebhookHandler struct {
	pool *db.Pool
	reg  *registry.Registry
}

// NewWebhookHandler creates a WebhookHandler with injected dependencies.
func NewWebhookHandler(pool *db.Pool, reg *registry.Registry) *WebhookHandler {
	return &WebhookHandler{pool: pool, reg: reg}
}

// HandleWebhookDelivery is the single entrypoint for
// /hooks/{webhookId}/{token}, registered method-agnostically — the handler
// itself checks the stored method against r.Method and 404s on mismatch,
// per WEBHOOK-04, before ever touching the token.
func (h *WebhookHandler) HandleWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	webhookID := chi.URLParam(r, "webhookId")
	token := chi.URLParam(r, "token")

	// Unscoped lookup: the public URL carries only {webhookId}, not the
	// owning app. A soft-deleted or nonexistent webhook resolves to
	// ErrWebhookNotFound identically — both cases plain 404, no delivery
	// logged (design.md Error Handling Strategy: "call to a deleted
	// webhook's URL → 404; no delivery logged" and "method mismatch → 404,
	// not logged" — a nonexistent id is the same shape of non-event).
	wh, err := dashboard.GetWebhookByID(ctx, h.pool, "", webhookID)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	if r.Method != wh.Method {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	if !dashboard.VerifyWebhookToken(wh.TokenHash, token) {
		h.logDelivery(ctx, wh.ID, http.StatusUnauthorized, "invalid_token", nil, "", "", "")
		writeError(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	payload, err := parseWebhookPayload(r)
	if err != nil {
		h.logDelivery(ctx, wh.ID, http.StatusBadRequest, "malformed", nil, "", "", err.Error())
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		// Marshaling a map built entirely from decoded JSON / query params
		// back to JSON cannot realistically fail — defensive only.
		h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", nil, "", "", err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if wh.Status == "capture" {
		if err := dashboard.StoreCapturedSample(ctx, h.pool, wh.ID, rawPayload); err != nil {
			h.logDelivery(ctx, wh.ID, http.StatusInternalServerError, "write_error", rawPayload, "", "", err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		h.logDelivery(ctx, wh.ID, http.StatusOK, "captured", rawPayload, "", "", "")
		writeJSON(w, http.StatusOK, map[string]string{"status": "captured"})
		return
	}

	h.handleActiveDelivery(ctx, w, wh, payload, rawPayload)
}

// handleActiveDelivery dispatches an active-mode call once a mapping is
// resolved. Insert dispatch lands in T7, update/delete in T8 — until then
// there is no active-mode business logic to run yet (T6's scope is
// routing/auth/capture only, per tasks.md).
func (h *WebhookHandler) handleActiveDelivery(ctx context.Context, w http.ResponseWriter, wh dashboard.WebhookRow, payload map[string]any, rawPayload []byte) {
	writeError(w, http.StatusNotImplemented, "active-mode dispatch not yet implemented")
}

// parseWebhookPayload normalizes the incoming request into a
// JSON-like map, uniformly regardless of the webhook's configured HTTP
// method (design.md Assumptions: query params for GET, JSON body
// otherwise). An empty/absent body or query string is a valid empty
// payload, not an error (spec Edge Cases).
func parseWebhookPayload(r *http.Request) (map[string]any, error) {
	if r.Method == http.MethodGet {
		payload := make(map[string]any, len(r.URL.Query()))
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				payload[key] = values[0]
			}
		}
		return payload, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// logDelivery records one call to a webhook. Logging failures are
// swallowed (best-effort, matches the existing purge/audit pattern in this
// codebase) — a delivery-log write failure must never itself change the
// HTTP response already decided by the caller.
func (h *WebhookHandler) logDelivery(ctx context.Context, webhookID string, httpStatus int, outcome string, rawPayload []byte, eventTypeValue, eventID, errorDetail string) {
	_ = dashboard.InsertDelivery(ctx, h.pool, dashboard.DeliveryEntry{
		WebhookID:      webhookID,
		HTTPStatus:     httpStatus,
		Outcome:        outcome,
		EventTypeValue: eventTypeValue,
		EventID:        eventID,
		RawPayload:     rawPayload,
		ErrorDetail:    errorDetail,
	})
}
