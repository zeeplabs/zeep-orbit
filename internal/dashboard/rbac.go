package dashboard

// Per-app role resolution. See `.specs/features/rbac-per-app/{spec,design}.md`
// for the full model. This file is the **single source of truth** for "what
// role does this user have on this app?" — every handler that needs to gate
// a per-app action calls ResolveAppRole rather than reimplementing the check.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AppRole names a user's effective per-app role. The zero value ("") means
// "not a member" — the user has no access to the app, regardless of which
// handler is asking.
type AppRole string

const (
	AppRoleAdmin  AppRole = "admin"
	AppRoleEditor AppRole = "editor"
	AppRoleViewer AppRole = "viewer"
)

// Effective reports whether the role grants at least read access.
func (r AppRole) Effective() bool { return r != "" }

// CanWrite reports whether the role can mutate app data/schema.
// admin and editor; viewer and zero-value cannot.
func (r AppRole) CanWrite() bool {
	return r == AppRoleAdmin || r == AppRoleEditor
}

// CanManage reports whether the role can perform app-management actions
// (auth/storage config, deploy, archive, member management). admin only.
func (r AppRole) CanManage() bool { return r == AppRoleAdmin }

// AppRef identifies which app to resolve a role for. Exactly one of
// BackendAppID / FrontendAppID must be set. Both empty or both set is a
// programmer error and ResolveAppRole returns ErrInvalidAppRef.
type AppRef struct {
	BackendAppID  string // UUID; empty if FrontendAppID is set
	FrontendAppID string // UUID; empty if BackendAppID is set
}

// ErrInvalidAppRef is returned by ResolveAppRole if the AppRef is malformed
// (both fields empty, or both set).
var ErrInvalidAppRef = errors.New("rbac: AppRef must have exactly one of BackendAppID/FrontendAppID set")

// ResolveAppRole returns the user's effective role on the given app.
//
// Resolution order (first match wins):
//  1. user is superadmin → AppRoleAdmin (bypass app_members entirely).
//  2. CanReadAnyApp(user.Role) is true (admin/auditor global) → AppRoleViewer.
//     This is the cross-spec extension documented in
//     dashboard-global-roles/design.md ("Where the extensão de `CanReadAnyApp`
//     é implementada → Dentro de `ResolveAppRole` (spec `rbac-per-app`)").
//     Gives global admin/auditor read access to any app without explicit
//     membership; writes are still blocked at the handler via CanWrite().
//  3. Look up app_members for (user_id, app). Returns the stored role, or ""
//     if the user is not a member of the app.
//
// Returns ("", ErrInvalidAppRef) if AppRef is malformed.
// Returns ("", nil) when the user is not a member and has no platform-level
// read access. Any other error is a database failure to be surfaced.
func ResolveAppRole(ctx context.Context, pool *db.Pool, user *DashboardUser, app AppRef) (AppRole, error) {
	if (app.BackendAppID == "" && app.FrontendAppID == "") ||
		(app.BackendAppID != "" && app.FrontendAppID != "") {
		return "", ErrInvalidAppRef
	}
	if user == nil {
		return "", errors.New("rbac: ResolveAppRole called with nil user")
	}
	// 1. superadmin bypass — never consult app_members.
	if user.Role == "superadmin" {
		return AppRoleAdmin, nil
	}
	// 2. Cross-spec: admin/auditor global gets read-only access to any app.
	//    If dashboard-global-roles is not yet implemented (CanReadAnyApp
	//    always false), this branch is skipped and we fall through to the
	//    normal membership lookup — safe default.
	if CanReadAnyApp(user.Role) {
		return AppRoleViewer, nil
	}
	// 3. Normal path: member must be in app_members.
	var role string
	var err error
	if app.BackendAppID != "" {
		err = pool.QueryRow(ctx,
			`SELECT role FROM zeep_system.app_members WHERE backend_app_id = $1 AND user_id = $2`,
			app.BackendAppID, user.ID).Scan(&role)
	} else {
		err = pool.QueryRow(ctx,
			`SELECT role FROM zeep_system.app_members WHERE frontend_app_id = $1 AND user_id = $2`,
			app.FrontendAppID, user.ID).Scan(&role)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("rbac: query app_members: %w", err)
	}
	return AppRole(role), nil
}
