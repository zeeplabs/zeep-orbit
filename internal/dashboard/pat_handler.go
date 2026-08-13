package dashboard

// pat_handler.go — dashboard-session-authenticated CRUD for an admin's own
// personal access tokens (mcp-server spec T13), mounted at
// /dashboard/api/me/pats behind RequireAuth (never RequirePAT — an admin
// manages tokens over their browser session, never uses a PAT to manage
// PATs; design.md: Dashboard PAT Handler + Settings UI). Mirrors the
// webhook handler pattern: validate -> store call -> h.audit(...) on
// mutation -> JSON response.

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type createPATRequest struct {
	Name string `json:"name"`
}

// createPATResponse embeds PATRow (never includes token_hash) plus the
// plaintext Token, present only on this one response — CreatePAT is the
// only place a PAT's plaintext is ever returned (spec P1 AC1: "shown once,
// never retrievable again in plaintext").
type createPATResponse struct {
	PATRow
	Token string `json:"token"`
}

// CreatePAT handles POST /dashboard/api/me/pats — creates a "manual" PAT
// for the caller and returns its plaintext token exactly once.
func (h *Handler) CreatePAT(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body createPATRequest
	if !h.decodeJSONBody(w, r, &body) {
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	token, row, err := CreatePAT(r.Context(), h.pool, user.ID, body.Name, PATKindManual, nil)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusCreated, createPATResponse{PATRow: row, Token: token})
	h.audit(r.Context(), user.ID, user.Email, "pat.create", "pat", row.ID, row.Name, nil, r.RemoteAddr)
}

// ListPATs handles GET /dashboard/api/me/pats — lists the caller's own
// tokens; PATRow never carries a token value, so there is no leak path.
func (h *Handler) ListPATs(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	rows, err := ListPATs(r.Context(), h.pool, user.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// RevokePAT handles DELETE /dashboard/api/me/pats/{patId} — revokes one of
// the caller's own tokens. An id belonging to another user is rejected the
// same way an unknown id is (ownership-scoped, mirrors the webhook-mapping
// IDOR fix): neither case leaks which one occurred.
func (h *Handler) RevokePAT(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	patID := chi.URLParam(r, "patId")

	if err := RevokePAT(r.Context(), h.pool, user.ID, patID); err != nil {
		if errors.Is(err, ErrPATNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "personal access token not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "personal access token revoked"})
	h.audit(r.Context(), user.ID, user.Email, "pat.revoke", "pat", patID, "", nil, r.RemoteAddr)
}
