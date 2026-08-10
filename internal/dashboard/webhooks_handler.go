package dashboard

// webhooks_handler.go — dashboard-session-authenticated CRUD for webhook
// subscriptions (T9), their event mappings + activation (T10), and delivery
// log listing (T11). Mirrors the table_policies handler pattern in
// handler.go: RBAC check (GetApp + role.CanManage()) → validate → store call
// → h.audit(...) on mutation → JSON response (design.md, Dashboard Webhook
// Handler component).

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// webhookResponse is the dashboard-facing shape of a WebhookRow — never
// includes TokenHash (AGENTS.md: never leak secrets in API responses; the
// plaintext token itself is returned exactly once, only from CreateWebhook
// and RotateWebhookToken, via createWebhookResponse/rotateTokenResponse).
type webhookResponse struct {
	ID             string         `json:"id"`
	AppID          string         `json:"app_id"`
	Name           string         `json:"name"`
	Method         string         `json:"method"`
	EventTypePath  string         `json:"event_type_path"`
	EventIDPath    *string        `json:"event_id_path"`
	Status         string         `json:"status"`
	CapturedSample map[string]any `json:"captured_sample"`
	DeletedAt      *time.Time     `json:"deleted_at"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func toWebhookResponse(w WebhookRow) webhookResponse {
	return webhookResponse{
		ID:             w.ID,
		AppID:          w.AppID,
		Name:           w.Name,
		Method:         w.Method,
		EventTypePath:  w.EventTypePath,
		EventIDPath:    w.EventIDPath,
		Status:         w.Status,
		CapturedSample: w.CapturedSample,
		DeletedAt:      w.DeletedAt,
		CreatedBy:      w.CreatedBy,
		CreatedAt:      w.CreatedAt,
		UpdatedAt:      w.UpdatedAt,
	}
}

type createWebhookRequest struct {
	Name          string `json:"name"`
	Method        string `json:"method"`
	EventTypePath string `json:"event_type_path"`
	EventIDPath   string `json:"event_id_path"`
}

type createWebhookResponse struct {
	webhookResponse
	Token string `json:"token"`
}

// webhookRBACGate resolves the app and enforces the access control this
// feature reuses from table_policies (CanManage — admin/superadmin only; see
// design.md Out of Scope: rbac-per-app granular roles aren't shipped yet, so
// webhook management inherits whatever gates other app-config resources
// today). Returns the app on success; writes the response and returns
// ok=false otherwise.
func (h *Handler) webhookRBACGate(w http.ResponseWriter, r *http.Request, appID string) (*AppRow, bool) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil, false
	}
	app, role, err := GetApp(r.Context(), h.pool, appID, user)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return nil, false
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return nil, false
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return nil, false
	}
	return app, true
}

// getScopedWebhook resolves {webhookId} scoped to appID, writing a 404 if it
// doesn't resolve (unknown id, wrong app, or soft-deleted).
func (h *Handler) getScopedWebhook(w http.ResponseWriter, r *http.Request, appID, webhookID string) (WebhookRow, bool) {
	wh, err := GetWebhookByID(r.Context(), h.pool, appID, webhookID)
	if err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return WebhookRow{}, false
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return WebhookRow{}, false
	}
	return wh, true
}

// CreateWebhook handles POST /dashboard/api/apps/{id}/webhooks.
func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	app, ok := h.webhookRBACGate(w, r, appID)
	if !ok {
		return
	}

	user, _ := UserFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body createWebhookRequest
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	switch body.Method {
	case "GET", "POST", "PUT", "PATCH":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "method must be one of GET, POST, PUT, PATCH"})
		return
	}
	if body.Name == "" || body.EventTypePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and event_type_path are required"})
		return
	}

	row, token, err := CreateWebhook(r.Context(), h.pool, CreateWebhookInput{
		AppID:         appID,
		Name:          body.Name,
		Method:        body.Method,
		EventTypePath: body.EventTypePath,
		EventIDPath:   body.EventIDPath,
		CreatedBy:     user.ID,
	})
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusCreated, createWebhookResponse{webhookResponse: toWebhookResponse(row), Token: token})
	h.audit(r.Context(), user.ID, user.Email, "webhook.create", "webhook", row.ID, app.Name+"/"+row.Name, nil, r.RemoteAddr)
}

// ListWebhooks handles GET /dashboard/api/apps/{id}/webhooks.
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	if _, ok := h.webhookRBACGate(w, r, appID); !ok {
		return
	}

	rows, err := ListWebhooks(r.Context(), h.pool, appID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	resp := make([]webhookResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, toWebhookResponse(row))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetWebhook handles GET /dashboard/api/apps/{id}/webhooks/{webhookId}.
func (h *Handler) GetWebhook(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	if _, ok := h.webhookRBACGate(w, r, appID); !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toWebhookResponse(wh))
}

// RotateWebhookToken handles POST /dashboard/api/apps/{id}/webhooks/{webhookId}/rotate-token.
// Invalidates the old token immediately and returns the new plaintext once
// (spec P3 AC1) without touching saved mappings.
func (h *Handler) RotateWebhookToken(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	app, ok := h.webhookRBACGate(w, r, appID)
	if !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}

	token, err := RotateToken(r.Context(), h.pool, wh.ID)
	if err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
	user, _ := UserFromContext(r.Context())
	h.audit(r.Context(), user.ID, user.Email, "webhook.rotate_token", "webhook", wh.ID, app.Name+"/"+wh.Name, nil, r.RemoteAddr)
}

// DeleteWebhook handles DELETE /dashboard/api/apps/{id}/webhooks/{webhookId}.
// Soft-deletes only — the webhook's URL then 404s (T6's GetWebhookByID
// lookup filters deleted_at IS NULL), but its delivery log survives until
// the independent 30-day purge (spec P3 AC2).
func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	app, ok := h.webhookRBACGate(w, r, appID)
	if !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}

	if err := SoftDeleteWebhook(r.Context(), h.pool, wh.ID); err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "webhook deleted"})
	user, _ := UserFromContext(r.Context())
	h.audit(r.Context(), user.ID, user.Email, "webhook.delete", "webhook", wh.ID, app.Name+"/"+wh.Name, nil, r.RemoteAddr)
}

// ----------------------------------------------------------------------------
// Event mapping CRUD + activation (T10)
// ----------------------------------------------------------------------------

type saveEventMappingRequest struct {
	EventTypeValue string            `json:"event_type_value"`
	Action         string            `json:"action"`
	TargetTable    string            `json:"target_table"`
	MatchKeyColumn string            `json:"match_key_column"`
	FieldMappings  []FieldMappingDef `json:"field_mappings"`
}

// SaveEventMapping handles POST /dashboard/api/apps/{id}/webhooks/{webhookId}/mappings.
func (h *Handler) SaveEventMapping(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	app, ok := h.webhookRBACGate(w, r, appID)
	if !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body saveEventMappingRequest
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	def := EventMappingDef{
		EventTypeValue: body.EventTypeValue,
		Action:         body.Action,
		TargetTable:    body.TargetTable,
		MatchKeyColumn: body.MatchKeyColumn,
		FieldMappings:  body.FieldMappings,
	}
	row, err := SaveEventMapping(r.Context(), h.pool, h.reg, app.Name, wh.ID, def)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidAction),
			errors.Is(err, ErrMatchKeyRequired),
			errors.Is(err, ErrUnknownTargetTable),
			errors.Is(err, ErrUnknownTargetColumn):
			// Safe to expose: fixed, non-dynamic sentinel messages, never raw
			// DB detail (AGENTS.md §4's typed-error exception).
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrMappingConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}

	writeJSON(w, http.StatusCreated, row)
	user, _ := UserFromContext(r.Context())
	h.audit(r.Context(), user.ID, user.Email, "webhook.mapping.save", "webhook_event_mapping", row.ID, app.Name+"/"+wh.Name+"/"+row.EventTypeValue, nil, r.RemoteAddr)
}

// ListEventMappings handles GET /dashboard/api/apps/{id}/webhooks/{webhookId}/mappings.
func (h *Handler) ListEventMappings(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	if _, ok := h.webhookRBACGate(w, r, appID); !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}

	rows, err := ListEventMappings(r.Context(), h.pool, wh.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// DeleteEventMapping handles DELETE /dashboard/api/apps/{id}/webhooks/{webhookId}/mappings/{mappingId}.
func (h *Handler) DeleteEventMapping(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	app, ok := h.webhookRBACGate(w, r, appID)
	if !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}
	mappingID := chi.URLParam(r, "mappingId")

	if err := DeleteEventMapping(r.Context(), h.pool, mappingID); err != nil {
		if errors.Is(err, ErrMappingNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "mapping not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "mapping deleted"})
	user, _ := UserFromContext(r.Context())
	h.audit(r.Context(), user.ID, user.Email, "webhook.mapping.delete", "webhook_event_mapping", mappingID, app.Name+"/"+wh.Name, nil, r.RemoteAddr)
}

// ActivateWebhook handles POST /dashboard/api/apps/{id}/webhooks/{webhookId}/activate.
// Rejects activation with zero saved mappings (spec Edge Cases) as a 400
// validation error, never a silent no-op.
func (h *Handler) ActivateWebhook(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	app, ok := h.webhookRBACGate(w, r, appID)
	if !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}

	if err := ActivateWebhook(r.Context(), h.pool, wh.ID); err != nil {
		switch {
		case errors.Is(err, ErrNoMappings):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot activate a webhook with zero saved mappings"})
		case errors.Is(err, ErrWebhookNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
	user, _ := UserFromContext(r.Context())
	h.audit(r.Context(), user.ID, user.Email, "webhook.activate", "webhook", wh.ID, app.Name+"/"+wh.Name, nil, r.RemoteAddr)
}

// ----------------------------------------------------------------------------
// Delivery log listing (T11)
// ----------------------------------------------------------------------------

const (
	defaultDeliveryPageSize = 50
	maxDeliveryPageSize     = 200
)

// ListWebhookDeliveries handles GET /dashboard/api/apps/{id}/webhooks/{webhookId}/deliveries.
// Returns newest-first, raw payload and error detail included per entry
// (spec P2 dashboard-delivery-log AC1/AC2) — reuses WebhookDeliveryStore.ListDeliveries
// exactly, no extra filtering/shaping at the handler layer.
func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "id")
	if _, ok := h.webhookRBACGate(w, r, appID); !ok {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}

	limit := defaultDeliveryPageSize
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxDeliveryPageSize {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	rows, err := ListDeliveries(r.Context(), h.pool, wh.ID, limit, offset)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
