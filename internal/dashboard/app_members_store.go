package dashboard

// app_members_store.go holds read/write helpers for the app_members table,
// including CountAppAdmins (used to enforce the "≥1 admin per app"
// invariant) and CRUD operations backing the membership management API.
// See `.specs/features/rbac-per-app/{spec,design}.md` for the full model.

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
// index on (app, user) guarantees this is enforced at the DB level, not
// just in the application — concurrent POSTs that race past the
// application check will still get a UNIQUE violation that maps to this
// error.
var ErrAlreadyMember = errors.New("rbac: user is already a member of this app")

// CountAppAdmins returns the number of users with role='admin' on the given
// app. Used to enforce the "≥1 admin per app" invariant from spec P1 (AC-5):
// any PATCH/DELETE on app_members that would leave this count at 0 must be
// rejected with 400. No-op for the "superadmin bypass" / "CanReadAnyApp
// read-only" paths — those return non-zero roles to ResolveAppRole and are
// not affected by membership counts.
//
// Does NOT take a lock. Callers that combine count+write (the membership
// management API) must wrap both in a transaction with
// `SELECT ... FOR UPDATE` on the relevant app_members rows, so the
// invariant is not lost to a race between two concurrent PATCH/DELETE
// operations. Same pattern as the "≥1 superadmin" invariant in users.go.
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

	members, err := lockAppMembers(ctx, tx, app)
	if err != nil {
		return err
	}
	oldRole, ok := memberRole(members, userID)
	if !ok {
		return ErrNotFound
	}

	// Invariant: demoting the last admin would leave the app with zero
	// admins. Only meaningful when the old role is 'admin' and the new
	// role is something else.
	if oldRole == string(AppRoleAdmin) && newRole != AppRoleAdmin {
		// After the UPDATE, this user will no longer be an admin. The
		// remaining count must be ≥ 1 — meaning the locked count is
		// currently ≥ 2 (this user + at least one other).
		if countAdmins(members) < 2 {
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

	members, err := lockAppMembers(ctx, tx, app)
	if err != nil {
		return err
	}
	oldRole, ok := memberRole(members, userID)
	if !ok {
		return ErrNotFound
	}

	// Invariant: removing the last admin would leave the app with zero
	// admins.
	if oldRole == string(AppRoleAdmin) {
		// After the DELETE, this user will no longer be an admin. The
		// remaining count must be ≥ 1.
		if countAdmins(members) < 2 {
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

// memberLock is one row of an app's membership, locked FOR UPDATE.
type memberLock struct {
	userID string
	role   string
}

// lockAppMembers locks every membership row for app, in a single query,
// ordered by user_id — the canonical, single lock order used by both
// UpdateAppMemberRole and RemoveAppMember. Must be called inside an open tx.
//
// A single query locking the whole set (rather than "lock the target row,
// then separately lock the admin rows") matters for more than just the
// aggregate-with-FOR-UPDATE restriction below: locking in two steps means
// the order depends on which row the caller happens to touch first, so two
// admins demoting/removing each other concurrently could lock in reversed
// order and deadlock (Postgres error 40P01) — the transaction that loses
// the deadlock gets a raw 500 instead of the clean ErrLastAppAdmin/success
// split the invariant is supposed to produce. A single ORDER BY user_id
// FOR UPDATE query is order-independent: every caller acquires the same
// locks in the same order, regardless of which user initiated the request.
//
// This also sidesteps Postgres rejecting `SELECT COUNT(*) ... FOR UPDATE`
// outright ("FOR UPDATE is not allowed with aggregate functions",
// SQLSTATE 0A000) — the locking query here has no aggregate; counting
// happens in Go over the already-locked rows via countAdmins.
func lockAppMembers(ctx context.Context, tx pgx.Tx, app AppRef) ([]memberLock, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if app.BackendAppID != "" {
		rows, err = tx.Query(ctx,
			`SELECT user_id, role FROM zeep_system.app_members
			 WHERE backend_app_id = $1 ORDER BY user_id FOR UPDATE`,
			app.BackendAppID)
	} else {
		rows, err = tx.Query(ctx,
			`SELECT user_id, role FROM zeep_system.app_members
			 WHERE frontend_app_id = $1 ORDER BY user_id FOR UPDATE`,
			app.FrontendAppID)
	}
	if err != nil {
		return nil, fmt.Errorf("rbac: lock app members: %w", err)
	}
	defer rows.Close()

	var members []memberLock
	for rows.Next() {
		var m memberLock
		if err := rows.Scan(&m.userID, &m.role); err != nil {
			return nil, fmt.Errorf("rbac: scan locked member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rbac: iterate locked members: %w", err)
	}
	return members, nil
}

// memberRole returns userID's role among the locked members, and whether
// they're a member at all.
func memberRole(members []memberLock, userID string) (string, bool) {
	for _, m := range members {
		if m.userID == userID {
			return m.role, true
		}
	}
	return "", false
}

// countAdmins counts admin rows among the already-locked members.
func countAdmins(members []memberLock) int {
	n := 0
	for _, m := range members {
		if m.role == string(AppRoleAdmin) {
			n++
		}
	}
	return n
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
// violation (SQLSTATE 23505). Used to map the partial UNIQUE indexes on
// app_members to ErrAlreadyMember.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// ListAppMembersForUser is the shared operation behind ListAppMembers' REST
// handler and the mcp-read-only-tools spec's orbit_list_app_members tool —
// backend-app case only (this spec excludes frontend-apps). Resolves the
// caller's role via ResolveAppRole and requires role.CanManage(), matching
// app_members.go:93's exact check, then returns the member list.
//
// SPEC_DEVIATION: unlike GetApp-based tools (orbit_get_app,
// orbit_list_table_policies), this function has no distinct "app not
// found" error for a well-formed but nonexistent/invisible appID —
// ResolveAppRole has no app-existence check of its own, so a caller
// without a matching app_members row (whether the app exists and they're
// just not a member, or the app id doesn't exist at all) resolves to the
// zero-value role and fails CanManage() the same way, returning
// ErrForbidden either way. This mirrors ListAppMembers' REST handler
// verbatim, including its own doc comment's stated reasoning: "the caller
// already knows the app id from the URL, so 403 doesn't leak existence the
// way 404 would" (app_members.go:64-67). ErrNotFound is returned only for
// a malformed AppRef (ErrInvalidAppRef, e.g. an empty appID), the one case
// the REST handler itself maps to 404.
func ListAppMembersForUser(ctx context.Context, pool *db.Pool, user *DashboardUser, appID string) ([]*AppMember, error) {
	ref := AppRef{BackendAppID: appID}
	role, err := ResolveAppRole(ctx, pool, user, ref)
	if err != nil {
		if errors.Is(err, ErrInvalidAppRef) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !role.CanManage() {
		return nil, ErrForbidden
	}
	return ListAppMembers(ctx, pool, ref)
}
