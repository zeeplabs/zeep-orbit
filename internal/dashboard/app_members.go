package dashboard

// app_members.go — HTTP handlers for the membership management API.
//
// Four handlers cover both axes (backend apps and frontend apps) by
// switching on the request URL path prefix:
//   - GET    /dashboard/api/apps/{id}/members
//   - GET    /dashboard/api/frontend-apps/{id}/members
//   - POST   /dashboard/api/apps/{id}/members              {user_id, role}
//   - POST   /dashboard/api/frontend-apps/{id}/members     {user_id, role}
//   - PATCH  /dashboard/api/apps/{id}/members/{userId}     {role}
//   - PATCH  /dashboard/api/frontend-apps/{id}/members/{userId} {role}
//   - DELETE /dashboard/api/apps/{id}/members/{userId}
//   - DELETE /dashboard/api/frontend-apps/{id}/members/{userId}
//
// All mutations require CanManage on the app (or superadmin). GET requires
// any effective role (read access). The "≥1 admin per app" invariant is
// enforced inside UpdateAppMemberRole and RemoveAppMember in the store,
// with a SELECT ... FOR UPDATE to prevent races between concurrent
// admin demotions (see app_members_store.go).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// appRefFromRequest builds an AppRef from the request URL path. The
// router mounts the same handler at /api/apps/{id}/... and
// /api/frontend-apps/{id}/... — the path prefix is the only signal we
// have for which axis the handler is being invoked on. Returns
// ErrInvalidAppRef for any other prefix (which should be unreachable
// from a correctly-wired router, but is safe to map to 404).
func appRefFromRequest(r *http.Request, id string) (AppRef, error) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/dashboard/api/apps/"):
		return AppRef{BackendAppID: id}, nil
	case strings.HasPrefix(r.URL.Path, "/dashboard/api/frontend-apps/"):
		return AppRef{FrontendAppID: id}, nil
	default:
		return AppRef{}, fmt.Errorf("app_members: unknown axis for path %q", r.URL.Path)
	}
}

// appMembersResourceID is the value written to audit_log.resource_id for
// member-management events. We use the app's own id (not the membership
// row id) so audit events for the same app are easy to filter. The
// affected user's id goes into metadata.
func appMembersResourceID(app AppRef) string {
	if app.BackendAppID != "" {
		return app.BackendAppID
	}
	return app.FrontendAppID
}

// ListAppMembers handles GET /dashboard/api/{apps|frontend-apps}/{id}/members.
// Returns the full membership list of the app, ordered by role (admin
// first, then editor, then viewer) then by created_at. Requires
// CanManage on the app (or superadmin bypass) — per spec AC-6, the
// member list is a management surface, so editor/viewer/non-members
// all get 403. (The caller already knows the app id from the URL, so
// 403 doesn't leak existence the way 404 would for individual app
// GET endpoints.)
func (h *Handler) ListAppMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing app id"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/dashboard/api/frontend-apps/") {
		app, err := appRefFromRequest(r, id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		role, err := ResolveAppRole(r.Context(), h.pool, actor, app)
		if err != nil {
			if errors.Is(err, ErrInvalidAppRef) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
			return
		}
		if !role.CanManage() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		members, err := ListAppMembers(r.Context(), h.pool, app)
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"members": members})
		return
	}

	members, err := ListAppMembersForUser(r.Context(), h.pool, actor, id)
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
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

// AddAppMember handles POST /dashboard/api/{apps|frontend-apps}/{id}/members.
// Body: {"user_id": "<uuid>", "role": "admin|editor|viewer"}. Returns
// 201 with the new AppMember. Requires CanManage on the app (or
// superadmin). The UNIQUE partial index on (app, user) enforces no-duplicate
// at the DB level; a violation maps to ErrAlreadyMember → 400.
func (h *Handler) AddAppMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing app id"})
		return
	}
	app, err := appRefFromRequest(r, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	// Write access: CanManage (admin only) or superadmin bypass.
	role, err := ResolveAppRole(r.Context(), h.pool, actor, app)
	if err != nil {
		if errors.Is(err, ErrInvalidAppRef) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return
	}
	switch body.Role {
	case "admin", "editor", "viewer":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be one of: admin, editor, viewer"})
		return
	}
	member, err := AddAppMember(r.Context(), h.pool, app, body.UserID, AppRole(body.Role))
	if err != nil {
		if errors.Is(err, ErrAlreadyMember) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user is already a member of this app"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	metadata, _ := json.Marshal(map[string]string{
		"user_id": body.UserID,
		"role":    body.Role,
	})
	h.audit(r.Context(), actor.ID, actor.Email, "app_member.added", "app_member",
		appMembersResourceID(app), id, metadata, r.RemoteAddr)
	writeJSON(w, http.StatusCreated, member)
}

// UpdateAppMember handles PATCH /dashboard/api/{apps|frontend-apps}/{id}/members/{userId}.
// Body: {"role": "admin|editor|viewer"}. Returns 200 with the updated
// AppMember. Requires CanManage on the app. The "≥1 admin" invariant is
// enforced in the store — a demotion that would leave zero admins
// returns 400.
func (h *Handler) UpdateAppMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	if id == "" || userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing app id or user id"})
		return
	}
	app, err := appRefFromRequest(r, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	role, err := ResolveAppRole(r.Context(), h.pool, actor, app)
	if err != nil {
		if errors.Is(err, ErrInvalidAppRef) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Role string `json:"role"`
	}
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	switch body.Role {
	case "admin", "editor", "viewer":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be one of: admin, editor, viewer"})
		return
	}
	if err := UpdateAppMemberRole(r.Context(), h.pool, app, userID, AppRole(body.Role)); err != nil {
		if errors.Is(err, ErrLastAppAdmin) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app must have at least one admin"})
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user is not a member of this app"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	// Read back the updated member to return its current state.
	members, err := ListAppMembers(r.Context(), h.pool, app)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	var updated *AppMember
	for _, m := range members {
		if m.UserID == userID {
			updated = m
			break
		}
	}
	if updated == nil {
		// Should not happen — we just updated the row. Surface as 500.
		h.writeError(w, r, http.StatusInternalServerError, "internal error",
			fmt.Errorf("app_members: updated row %s vanished", userID))
		return
	}
	metadata, _ := json.Marshal(map[string]string{
		"user_id": userID,
		"role":    body.Role,
	})
	h.audit(r.Context(), actor.ID, actor.Email, "app_member.role_changed", "app_member",
		appMembersResourceID(app), id, metadata, r.RemoteAddr)
	writeJSON(w, http.StatusOK, updated)
}

// RemoveAppMember handles DELETE /dashboard/api/{apps|frontend-apps}/{id}/members/{userId}.
// Returns 204 No Content on success. Requires CanManage on the app.
// The "≥1 admin" invariant is enforced in the store — removing the last
// admin returns 400. Removing a non-member returns 404.
func (h *Handler) RemoveAppMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	if id == "" || userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing app id or user id"})
		return
	}
	app, err := appRefFromRequest(r, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	role, err := ResolveAppRole(r.Context(), h.pool, actor, app)
	if err != nil {
		if errors.Is(err, ErrInvalidAppRef) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	if !role.CanManage() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := RemoveAppMember(r.Context(), h.pool, app, userID); err != nil {
		if errors.Is(err, ErrLastAppAdmin) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app must have at least one admin"})
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user is not a member of this app"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	metadata, _ := json.Marshal(map[string]string{"user_id": userID})
	h.audit(r.Context(), actor.ID, actor.Email, "app_member.removed", "app_member",
		appMembersResourceID(app), id, metadata, r.RemoteAddr)
	w.WriteHeader(http.StatusNoContent)
}
