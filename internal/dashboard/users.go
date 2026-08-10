package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// errLastSuperadmin is the sentinel returned by assertAtLeastOneSuperadminRemains
// when removing the named user would leave the platform with zero superadmins.
// Callers translate this to HTTP 400 with a user-facing message.
var errLastSuperadmin = errors.New("last superadmin")

// assertAtLeastOneSuperadminRemains reports whether the dashboard has at
// least one superadmin OTHER than the excluded user. Returns errLastSuperadmin
// if the exclusion would leave the platform with zero superadmins; any other
// error is a DB failure.
//
// Reused by both the PATCH /users/{id} role-change path and DELETE /users/{id}
// — same invariant applies to both.
func assertAtLeastOneSuperadminRemains(ctx context.Context, pool *db.Pool, excludeUserID string) error {
	var count int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.dashboard_users WHERE role = 'superadmin' AND id != $1`,
		excludeUserID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("dashboard: count superadmins: %w", err)
	}
	if count == 0 {
		return errLastSuperadmin
	}
	return nil
}

// UpdateUserRole handles PATCH /dashboard/api/users/{id} — promotes or
// demotes an existing user. The body carries only {"role": "..."}. The
// handler enforces three gates before persisting:
//   - ActionManageUsers: actor must be superadmin or admin
//   - CanCreateUserWithRole: same rule as CreateUser (only superadmin can
//     assign superadmin)
//   - ≥1 superadmin invariant: if the target is currently a superadmin
//     and the new role is not, at least one other superadmin must exist
//
// On success, a user.role_changed audit entry is written with metadata
// {"from": oldRole, "to": newRole}.
func (h *Handler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if !HasPlatformPermission(actor.Role, ActionManageUsers) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing user id"})
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
	case "superadmin", "admin", "auditor", "member":
		// valid
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be one of: superadmin, admin, auditor, member"})
		return
	}

	if !CanCreateUserWithRole(actor.Role, body.Role) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a superadmin can create a superadmin"})
		return
	}

	// Look up the target so we know the previous role (for the audit log's
	// "from" field) and can run the invariant check BEFORE persisting.
	target, err := GetUser(r.Context(), h.pool, targetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	// ≥1 superadmin invariant: only meaningful when the target is being
	// demoted OUT of superadmin. Re-promoting an existing superadmin or
	// changing non-superadmin roles never touches the count.
	if target.Role == "superadmin" && body.Role != "superadmin" {
		if err := assertAtLeastOneSuperadminRemains(r.Context(), h.pool, targetID); err != nil {
			if errors.Is(err, errLastSuperadmin) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "platform must have at least one superadmin"})
				return
			}
			h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
			return
		}
	}

	updated, err := UpdateUserRole(r.Context(), h.pool, targetID, body.Role)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Race: target deleted between GetUser and UpdateUserRole.
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "internal error", err)
		return
	}

	metadata, _ := json.Marshal(map[string]string{"from": target.Role, "to": body.Role})
	h.audit(r.Context(), actor.ID, actor.Email, "user.role_changed", "user", updated.ID, updated.Email, metadata, r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]string{
		"id":    updated.ID,
		"email": updated.Email,
		"name":  updated.Name,
		"role":  updated.Role,
	})
}
