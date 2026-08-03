package dashboard

// app_members_store.go — read helpers for the app_members table. T-03
// (CountAppAdmins for the "≥1 admin per app" invariant) and T-06
// (CRUD operations) land here. See `.specs/features/rbac-per-app/{spec,
// design}.md` for the full model.

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// CountAppAdmins returns the number of users with role='admin' on the given
// app. Used to enforce the "≥1 admin per app" invariant from spec P1 (AC-5):
// any PATCH/DELETE on app_members that would leave this count at 0 must be
// rejected with 400. No-op for the "superadmin bypass" / "CanReadAnyApp
// read-only" paths — those return non-zero roles to ResolveAppRole and are
// not affected by membership counts.
//
// Does NOT take a lock. Callers that combine count+write (the membership
// management API in T-06) must wrap both in a transaction with
// `SELECT ... FOR UPDATE` on the relevant app_members rows, so the
// invariant is not lost to a race between two concurrent PATCH/DELETE
// operations. Same pattern as the "≥1 superadmin" invariant in
// dashboard-global-roles T-05.
func CountAppAdmins(ctx context.Context, pool *db.Pool, app AppRef) (int, error) {
	if (app.BackendAppID == "" && app.FrontendAppID == "") ||
		(app.BackendAppID != "" && app.FrontendAppID != "") {
		return 0, ErrInvalidAppRef
	}
	var n int
	var err error
	if app.BackendAppID != "" {
		err = pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM zeep_system.app_members WHERE backend_app_id = $1 AND role = 'admin'`,
			app.BackendAppID).Scan(&n)
	} else {
		err = pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM zeep_system.app_members WHERE frontend_app_id = $1 AND role = 'admin'`,
			app.FrontendAppID).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("rbac: count app admins: %w", err)
	}
	return n, nil
}
