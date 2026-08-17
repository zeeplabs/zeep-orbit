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

// webhookResponse is the dashboard-facing shape of a WebhookRow. Token is
// the decrypted plaintext token, included on every response (list/get,
// not just create/rotate) so the dashboard can always render the full
// callback URL — the token is recoverable AES-256-GCM ciphertext, not a
// one-way hash, precisely to support that. Empty when decryption fails
// (a legacy webhook whose token predates the hash -> encryption
// migration): the frontend must prompt the owner to rotate it.
type webhookResponse struct {
	ID             string         `json:"id"`
	AppID          string         `json:"app_id"`
	Name           string         `json:"name"`
	Method         string         `json:"method"`
	Token          string         `json:"token"`
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
	token, _ := DecryptWebhookToken(w) // "" on legacy/undecryptable token — frontend prompts rotation
	return webhookResponse{
		ID:             w.ID,
		AppID:          w.AppID,
		Name:           w.Name,
		Method:         w.Method,
		Token:          token,
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
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var body createWebhookRequest
	if !h.decodeJSONBody(w, r, &body) {
		return
	}

	row, err := h.CreateWebhookForUser(r.Context(), user, appID, CreateWebhookInput{
		Name:          body.Name,
		Method:        body.Method,
		EventTypePath: body.EventTypePath,
		EventIDPath:   body.EventIDPath,
	}, r.RemoteAddr)
	if err != nil {
		var valErr *ValidationError
		switch {
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.As(err, &valErr):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": valErr.Error()})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}

	writeJSON(w, http.StatusCreated, toWebhookResponse(*row))
}

type updateWebhookRequest struct {
	Name          string `json:"name"`
	Method        string `json:"method"`
	EventTypePath string `json:"event_type_path"`
	EventIDPath   string `json:"event_id_path"`
}

// UpdateWebhook handles PATCH /dashboard/api/apps/{id}/webhooks/{webhookId}.
// Full-replace: name, method, and both event-shape paths must all be sent
// (mirrors CreateWebhook's required-fields shape, not a partial patch).
// Token, status, and captured_sample are untouched.
func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
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
	var body updateWebhookRequest
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Name == "" || body.EventTypePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and event_type_path are required"})
		return
	}

	updated, err := UpdateWebhook(r.Context(), h.pool, wh.ID, UpdateWebhookInput{
		Name:          body.Name,
		Method:        body.Method,
		EventTypePath: body.EventTypePath,
		EventIDPath:   body.EventIDPath,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidWebhookMethod):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "method must be one of GET, POST, PUT, PATCH"})
		case errors.Is(err, ErrWebhookNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}

	writeJSON(w, http.StatusOK, toWebhookResponse(updated))
	user, _ := UserFromContext(r.Context())
	h.audit(r.Context(), user.ID, user.Email, "webhook.update", "webhook", wh.ID, app.Name+"/"+updated.Name, nil, r.RemoteAddr)
}

// ListWebhooks handles GET /dashboard/api/apps/{id}/webhooks.
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	appID := chi.URLParam(r, "id")
	rows, err := ListWebhooksForUser(r.Context(), h.pool, user, appID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
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
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	appID := chi.URLParam(r, "id")
	webhookID := chi.URLParam(r, "webhookId")
	// Event mappings aren't part of this REST response shape (webhookResponse
	// has no mappings field) — only orbit_get_webhook's combined response
	// needs them, per design.md's Tech Decisions. Discarded here.
	wh, _, err := GetWebhookForUser(r.Context(), h.pool, user, appID, webhookID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.Is(err, ErrWebhookNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, toWebhookResponse(*wh))
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

	if _, err := RotateToken(r.Context(), h.pool, wh.ID); err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	rotated, ok := h.getScopedWebhook(w, r, appID, webhookID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toWebhookResponse(rotated))
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

// mappingResponse is the dashboard-facing shape of an EventMappingRow.
// EventMappingRow itself has no json tags (same reasoning as WebhookRow: it
// stays a plain internal store type), so writing it directly to the
// response would marshal PascalCase Go field names instead of the
// snake_case shape the frontend decodes — exactly the bug T17 already found
// and fixed once for webhook_deliveries; this is the same bug class in the
// event-mappings endpoints.
type mappingResponse struct {
	ID             string            `json:"id"`
	WebhookID      string            `json:"webhook_id"`
	EventTypeValue string            `json:"event_type_value"`
	Action         string            `json:"action"`
	TargetTable    string            `json:"target_table"`
	MatchKeyColumn *string           `json:"match_key_column"`
	FieldMappings  []FieldMappingDef `json:"field_mappings"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func toMappingResponse(m EventMappingRow) mappingResponse {
	return mappingResponse{
		ID:             m.ID,
		WebhookID:      m.WebhookID,
		EventTypeValue: m.EventTypeValue,
		Action:         m.Action,
		TargetTable:    m.TargetTable,
		MatchKeyColumn: m.MatchKeyColumn,
		FieldMappings:  m.FieldMappings,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

type saveEventMappingRequest struct {
	EventTypeValue string            `json:"event_type_value"`
	Action         string            `json:"action"`
	TargetTable    string            `json:"target_table"`
	MatchKeyColumn string            `json:"match_key_column"`
	FieldMappings  []FieldMappingDef `json:"field_mappings"`
}

// SaveEventMapping handles POST /dashboard/api/apps/{id}/webhooks/{webhookId}/mappings.
func (h *Handler) SaveEventMapping(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	appID := chi.URLParam(r, "id")
	webhookID := chi.URLParam(r, "webhookId")
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
	row, err := h.SaveEventMappingForUser(r.Context(), user, appID, webhookID, def, r.RemoteAddr)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.Is(err, ErrWebhookNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		case errors.Is(err, ErrInvalidAction),
			errors.Is(err, ErrMatchKeyRequired),
			errors.Is(err, ErrUnknownTargetTable),
			errors.Is(err, ErrUnknownTargetColumn),
			errors.Is(err, ErrEventTypeValueRequired),
			errors.Is(err, ErrFieldMappingsRequired):
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

	writeJSON(w, http.StatusCreated, toMappingResponse(*row))
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
	resp := make([]mappingResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, toMappingResponse(row))
	}
	writeJSON(w, http.StatusOK, resp)
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

	if err := DeleteEventMapping(r.Context(), h.pool, wh.ID, mappingID); err != nil {
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

// deliveryResponse is the dashboard-facing shape of a DeliveryRow. DeliveryRow
// itself carries no json tags (it's an internal store type), so marshaling it
// directly would emit Go's default PascalCase field names (ReceivedAt,
// EventTypeValue, ...) instead of the snake_case shape design.md's
// WebhookDelivery model documents and the frontend (WebhookDelivery in
// src/lib/api.ts, Webhooks.tsx's WebhookDeliveryLog) actually reads — mirrors
// the same webhookResponse/toWebhookResponse translation already done for
// WebhookRow above.
type deliveryResponse struct {
	ID             string         `json:"id"`
	WebhookID      string         `json:"webhook_id"`
	ReceivedAt     time.Time      `json:"received_at"`
	HTTPStatus     int            `json:"http_status"`
	Outcome        string         `json:"outcome"`
	EventTypeValue *string        `json:"event_type_value"`
	EventID        *string        `json:"event_id"`
	RawPayload     map[string]any `json:"raw_payload"`
	TargetRowID    *string        `json:"target_row_id"`
	ErrorDetail    *string        `json:"error_detail"`
}

func toDeliveryResponse(d DeliveryRow) deliveryResponse {
	return deliveryResponse{
		ID:             d.ID,
		WebhookID:      d.WebhookID,
		ReceivedAt:     d.ReceivedAt,
		HTTPStatus:     d.HTTPStatus,
		Outcome:        d.Outcome,
		EventTypeValue: d.EventTypeValue,
		EventID:        d.EventID,
		RawPayload:     d.RawPayload,
		TargetRowID:    d.TargetRowID,
		ErrorDetail:    d.ErrorDetail,
	}
}

// ListWebhookDeliveries handles GET /dashboard/api/apps/{id}/webhooks/{webhookId}/deliveries.
// Returns newest-first, raw payload and error detail included per entry
// (spec P2 dashboard-delivery-log AC1/AC2) — reuses WebhookDeliveryStore.ListDeliveries
// for the data, translated through deliveryResponse for the wire shape.
func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	appID := chi.URLParam(r, "id")
	webhookID := chi.URLParam(r, "webhookId")

	// limit/offset are parsed here as sentinel-style raw ints (0/-1 meaning
	// "unset or unparsable") — ListWebhookDeliveriesForUser owns the actual
	// default/max clamping (moved there as part of the T12 extraction), so
	// this parsing step is unchanged in effect from the pre-extraction
	// behavior: an invalid or out-of-range value silently falls back to the
	// default, never a 400.
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	offset := -1
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}

	rows, err := ListWebhookDeliveriesForUser(r.Context(), h.pool, user, appID, webhookID, limit, offset)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.Is(err, ErrWebhookNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		default:
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		}
		return
	}
	resp := make([]deliveryResponse, len(rows))
	for i, row := range rows {
		resp[i] = toDeliveryResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}
