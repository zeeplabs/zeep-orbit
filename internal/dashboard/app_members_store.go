package dashboard

// app_members_store.go — read + write helpers for the app_members table.
// T-03 (CountAppAdmins for the "≥1 admin per app" invariant) and T-06
// (CRUD operations for the membership management API) land here. See
// `.specs/features/rbac-per-app/{spec,design}.md` for the full model.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// AppMember is one row of zeep_system.app_members. Returned by
// ListAppMembers and used as the request/response shape of the
// membership management API.
type AppMember struct {
	ID            string  `json:"id"`
	BackendAppID  *string `json:"backend_app_id,omitempty"`
	FrontendAppID *string `json:"frontend_app_id,omitempty"`
	UserID        string  `json:"user_id"`
	Role          AppRole `json:"role"`
	CreatedAt     string  `json:"created_at"`
}

// ErrLastAppAdmin is the sentinel returned by UpdateAppMemberRole and
// RemoveAppMember when the operation would leave the app with zero
// admins. Callers translate this to HTTP 400 with a user-facing message
// ("app precisa de ao menos um admin"). Same pattern as errLastSuperadmin
// in users.go for the "≥1 superadmin" invariant.
var ErrLastAppAdmin = errors.New("rbac: app must have at least one admin")

// ErrAlreadyMember is the sentinel returned by AddAppMember when the
// (app, user) pair already exists in app_members. The UNIQUE partial
// index from T-01 guarantees this is enforced at the DB level, not just
// in the application — concurrent POSTs that race past the application
// check will still get a UNIQUE violation that maps to this error.
var ErrAlreadyMember = errors.New("rbac: user is already a member of this app")

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

// ListAppMembers returns every member of the given app, ordered by
// role (admin first) then created_at. No auth check here — the caller
// (the handler) is responsible for ensuring the actor is allowed to
// see the membership list (admin of the app, superadmin, or CanReadAnyApp).
func ListAppMembers(ctx context.Context, pool *db.Pool, app AppRef) ([]*AppMember, error) {
	if (app.BackendAppID == "" && app.FrontendAppID == "") ||
		(app.BackendAppID != "" && app.FrontendAppID != "") {
		return nil, ErrInvalidAppRef
	}
	var (
		rows pgx.Rows
		err  error
	)
	if app.BackendAppID != "" {
		rows, err = pool.Query(ctx,
			`SELECT id, backend_app_id, frontend_app_id, user_id, role, created_at::text
			 FROM zeep_system.app_members
			 WHERE backend_app_id = $1
			 ORDER BY CASE role WHEN 'admin' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, created_at`,
			app.BackendAppID)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT id, backend_app_id, frontend_app_id, user_id, role, created_at::text
			 FROM zeep_system.app_members
			 WHERE frontend_app_id = $1
			 ORDER BY CASE role WHEN 'admin' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, created_at`,
			app.FrontendAppID)
	}
	if err != nil {
		return nil, fmt.Errorf("rbac: list app members: %w", err)
	}
	defer rows.Close()
	var out []*AppMember
	for rows.Next() {
		var m AppMember
		if err := rows.Scan(&m.ID, &m.BackendAppID, &m.FrontendAppID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("rbac: scan app member: %w", err)
		}
		out = append(out, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rbac: app member rows: %w", err)
	}
	if out == nil {
		out = []*AppMember{}
	}
	return out, nil
}

// AddAppMember inserts a new (app, user, role) row. Returns the
// created AppMember on success. Returns ErrAlreadyMember if the
// (app, user) pair already exists (UNIQUE partial index violation);
// this is enforced at the DB level so concurrent POSTs cannot both
// succeed. The caller is responsible for the "actor is allowed to
// add members" auth check (CanManage on the app, or superadmin).
func AddAppMember(ctx context.Context, pool *db.Pool, app AppRef, userID string, role AppRole) (*AppMember, error) {
	if (app.BackendAppID == "" && app.FrontendAppID == "") ||
		(app.BackendAppID != "" && app.FrontendAppID != "") {
		return nil, ErrInvalidAppRef
	}
	if !validAppRole(role) {
		return nil, fmt.Errorf("rbac: invalid app role %q (want admin/editor/viewer)", role)
	}
	var (
		m   AppMember
		err error
	)
	if app.BackendAppID != "" {
		err = pool.QueryRow(ctx,
			`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role)
			 VALUES ($1, $2, $3)
			 RETURNING id, backend_app_id, frontend_app_id, user_id, role, created_at::text`,
			app.BackendAppID, userID, string(role)).Scan(&m.ID, &m.BackendAppID, &m.FrontendAppID, &m.UserID, &m.Role, &m.CreatedAt)
	} else {
		err = pool.QueryRow(ctx,
			`INSERT INTO zeep_system.app_members (frontend_app_id, user_id, role)
			 VALUES ($1, $2, $3)
			 RETURNING id, backend_app_id, frontend_app_id, user_id, role, created_at::text`,
			app.FrontendAppID, userID, string(role)).Scan(&m.ID, &m.BackendAppID, &m.FrontendAppID, &m.UserID, &m.Role, &m.CreatedAt)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyMember
		}
		return nil, fmt.Errorf("rbac: add app member: %w", err)
	}
	return &m, nil
}

// UpdateAppMemberRole changes a member's role. Enforces the
// "≥1 admin per app" invariant: if the target is currently 'admin'
// and the new role is not, and they are the only admin, returns
// ErrLastAppAdmin. The check + write happen in a single transaction
// with SELECT FOR UPDATE on the admin rows, so two concurrent admins
// cannot both see "there's still another admin" and both proceed to
// demote themselves. Returns ErrNotFound if the user is not currently
// a member of the app.
func UpdateAppMemberRole(ctx context.Context, pool *db.Pool, app AppRef, userID string, newRole AppRole) error {
	if (app.BackendAppID == "" && app.FrontendAppID == "") ||
		(app.BackendAppID != "" && app.FrontendAppID != "") {
		return ErrInvalidAppRef
	}
	if !validAppRole(newRole) {
		return fmt.Errorf("rbac: invalid app role %q (want admin/editor/viewer)", newRole)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rbac: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the target's current row and read the old role.
	var oldRole string
	var query string
	var arg any
	if app.BackendAppID != "" {
		query = `SELECT role FROM zeep_system.app_members
		         WHERE backend_app_id = $1 AND user_id = $2 FOR UPDATE`
		arg = app.BackendAppID
	} else {
		query = `SELECT role FROM zeep_system.app_members
		         WHERE frontend_app_id = $1 AND user_id = $2 FOR UPDATE`
		arg = app.FrontendAppID
	}
	if err := tx.QueryRow(ctx, query, arg, userID).Scan(&oldRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("rbac: read app member: %w", err)
	}

	// Invariant: demoting the last admin would leave the app with zero
	// admins. Only meaningful when the old role is 'admin' and the new
	// role is something else.
	if oldRole == string(AppRoleAdmin) && newRole != AppRoleAdmin {
		var count int
		if app.BackendAppID != "" {
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM zeep_system.app_members
				 WHERE backend_app_id = $1 AND role = 'admin' FOR UPDATE`,
				app.BackendAppID).Scan(&count); err != nil {
				return fmt.Errorf("rbac: count admins under lock: %w", err)
			}
		} else {
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM zeep_system.app_members
				 WHERE frontend_app_id = $1 AND role = 'admin' FOR UPDATE`,
				app.FrontendAppID).Scan(&count); err != nil {
				return fmt.Errorf("rbac: count admins under lock: %w", err)
			}
		}
		// After the UPDATE, this user will no longer be an admin. The
		// remaining count must be ≥ 1 — meaning the locked count is
		// currently ≥ 2 (this user + at least one other).
		if count < 2 {
			return ErrLastAppAdmin
		}
	}

	// Apply the role change.
	if app.BackendAppID != "" {
		_, err = tx.Exec(ctx,
			`UPDATE zeep_system.app_members SET role = $1
			 WHERE backend_app_id = $2 AND user_id = $3`,
			string(newRole), app.BackendAppID, userID)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE zeep_system.app_members SET role = $1
			 WHERE frontend_app_id = $2 AND user_id = $3`,
			string(newRole), app.FrontendAppID, userID)
	}
	if err != nil {
		return fmt.Errorf("rbac: update app member role: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rbac: commit tx: %w", err)
	}
	return nil
}

// RemoveAppMember deletes a member. Enforces the same "≥1 admin"
// invariant as UpdateAppMemberRole: removing the last admin returns
// ErrLastAppAdmin. Returns ErrNotFound if the user is not currently
// a member of the app.
func RemoveAppMember(ctx context.Context, pool *db.Pool, app AppRef, userID string) error {
	if (app.BackendAppID == "" && app.FrontendAppID == "") ||
		(app.BackendAppID != "" && app.FrontendAppID != "") {
		return ErrInvalidAppRef
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rbac: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Read the target's current role under FOR UPDATE so a concurrent
	// UpdateAppMemberRole can't change it between our check and our delete.
	var oldRole string
	if app.BackendAppID != "" {
		if err := tx.QueryRow(ctx,
			`SELECT role FROM zeep_system.app_members
			 WHERE backend_app_id = $1 AND user_id = $2 FOR UPDATE`,
			app.BackendAppID, userID).Scan(&oldRole); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("rbac: read app member: %w", err)
		}
	} else {
		if err := tx.QueryRow(ctx,
			`SELECT role FROM zeep_system.app_members
			 WHERE frontend_app_id = $1 AND user_id = $2 FOR UPDATE`,
			app.FrontendAppID, userID).Scan(&oldRole); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("rbac: read app member: %w", err)
		}
	}

	// Invariant: removing the last admin would leave the app with zero
	// admins. Lock the admin count and check.
	if oldRole == string(AppRoleAdmin) {
		var count int
		if app.BackendAppID != "" {
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM zeep_system.app_members
				 WHERE backend_app_id = $1 AND role = 'admin' FOR UPDATE`,
				app.BackendAppID).Scan(&count); err != nil {
				return fmt.Errorf("rbac: count admins under lock: %w", err)
			}
		} else {
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM zeep_system.app_members
				 WHERE frontend_app_id = $1 AND role = 'admin' FOR UPDATE`,
				app.FrontendAppID).Scan(&count); err != nil {
				return fmt.Errorf("rbac: count admins under lock: %w", err)
			}
		}
		// After the DELETE, this user will no longer be an admin. The
		// remaining count must be ≥ 1.
		if count < 2 {
			return ErrLastAppAdmin
		}
	}

	// Apply the delete.
	if app.BackendAppID != "" {
		_, err = tx.Exec(ctx,
			`DELETE FROM zeep_system.app_members
			 WHERE backend_app_id = $1 AND user_id = $2`,
			app.BackendAppID, userID)
	} else {
		_, err = tx.Exec(ctx,
			`DELETE FROM zeep_system.app_members
			 WHERE frontend_app_id = $1 AND user_id = $2`,
			app.FrontendAppID, userID)
	}
	if err != nil {
		return fmt.Errorf("rbac: remove app member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rbac: commit tx: %w", err)
	}
	return nil
}

// validAppRole reports whether r is one of the three AppRole constants.
func validAppRole(r AppRole) bool {
	switch r {
	case AppRoleAdmin, AppRoleEditor, AppRoleViewer:
		return true
	}
	return false
}

// isUniqueViolation reports whether err is a Postgres UNIQUE constraint
// violation (SQLSTATE 23505). Used to map the partial UNIQUE indexes
// from T-01 to ErrAlreadyMember.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
