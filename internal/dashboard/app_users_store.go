package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// or "undefined column" (42703) error, meaning the _auth_users table or columns don't exist.
func isPgRelationNotFound(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01" || pgErr.Code == "42703"
	}
	return false
}

// AppUserSummary is a row from _auth_users for the dashboard.
type AppUserSummary struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         *string    `json:"name"`
	Phone        *string    `json:"phone"`
	AvatarURL    *string    `json:"avatar_url"`
	Provider     string     `json:"provider"`
	Role         string     `json:"role"`
	Active       bool       `json:"active"`
	LastSignInAt *time.Time `json:"last_sign_in_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// AppUserProviderCount is a count of users grouped by provider.
type AppUserProviderCount struct {
	Provider string `json:"provider"`
	Count    int    `json:"count"`
}

// schema is the app's schema name (e.g. "app_billing").
func ListAppUsers(ctx context.Context, pool *db.Pool, schema, search string, limit, offset int) ([]*AppUserSummary, int, error) {
	var countArgs []any
	where := ""
	if search != "" {
		where = ` WHERE "email" ILIKE $1`
		countArgs = append(countArgs, "%"+search+"%")
	}

	var total int
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %q."_auth_users"%s`, schema, where)
	if err := pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		if isPgRelationNotFound(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("dashboard: count app users: %w", err)
	}

	var queryArgs []any
	paramOffset := 0
	if search != "" {
		queryArgs = append(queryArgs, "%"+search+"%")
		paramOffset = 1
	}
	queryArgs = append(queryArgs, limit, offset)
	rows, err := pool.Query(ctx,
		fmt.Sprintf(`SELECT id, email, name, phone, avatar_url, provider, role, active, last_sign_in_at, created_at
		 FROM %q."_auth_users"%s
		 ORDER BY created_at DESC
		 LIMIT $%d OFFSET $%d`, schema, where, paramOffset+1, paramOffset+2),
		queryArgs...,
	)
	if err != nil {
		if isPgRelationNotFound(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("dashboard: list app users: %w", err)
	}
	defer rows.Close()

	var users []*AppUserSummary
	for rows.Next() {
		var u AppUserSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Phone, &u.AvatarURL, &u.Provider, &u.Role, &u.Active, &u.LastSignInAt, &u.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("dashboard: scan app user: %w", err)
		}
		users = append(users, &u)
	}
	return users, total, nil
}

// CountAppUsersByProvider returns user counts grouped by provider for an app schema.
func CountAppUsersByProvider(ctx context.Context, pool *db.Pool, schema string) ([]*AppUserProviderCount, error) {
	rows, err := pool.Query(ctx,
		fmt.Sprintf(`SELECT provider, COUNT(*)::int FROM %q."_auth_users" GROUP BY provider ORDER BY provider`, schema),
	)
	if err != nil {
		if isPgRelationNotFound(err) {
			return []*AppUserProviderCount{}, nil
		}
		return nil, fmt.Errorf("dashboard: count app users by provider: %w", err)
	}
	defer rows.Close()

	var counts []*AppUserProviderCount
	for rows.Next() {
		var c AppUserProviderCount
		if err := rows.Scan(&c.Provider, &c.Count); err != nil {
			return nil, fmt.Errorf("dashboard: scan provider count: %w", err)
		}
		counts = append(counts, &c)
	}
	if counts == nil {
		counts = []*AppUserProviderCount{}
	}
	return counts, nil
}

// DeactivateAppUser sets a user's active flag to false.
func DeactivateAppUser(ctx context.Context, pool *db.Pool, schema, userID string) error {
	tag, err := pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %q."_auth_users" SET active = false WHERE id = $1`, schema),
		userID,
	)
	if err != nil {
		if isPgRelationNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("dashboard: deactivate app user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ActivateAppUser sets a user's active flag to true.
func ActivateAppUser(ctx context.Context, pool *db.Pool, schema, userID string) error {
	tag, err := pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %q."_auth_users" SET active = true WHERE id = $1`, schema),
		userID,
	)
	if err != nil {
		if isPgRelationNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("dashboard: activate app user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrEmailConflict is returned by UpdateAppUser when the new email collides
// with another user's email in the same app schema (UNIQUE violation).
var ErrEmailConflict = errors.New("email already in use")

// UpdateAppUser sets a user's email, phone, and business role (the latter
// used by end-user-row-policies clause matching, e.g.
// current_setting('app.jwt_role')). email/role must already be
// validated/normalized by the caller before reaching here. emailChanged
// reports whether the normalized email differed from the stored one, so the
// caller can decide whether to reset sessions.
func UpdateAppUser(ctx context.Context, pool *db.Pool, schema, userID, email, phone, role string) (emailChanged bool, err error) {
	var currentEmail string
	err = pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT email FROM %q."_auth_users" WHERE id = $1`, schema),
		userID,
	).Scan(&currentEmail)
	if err != nil {
		if isPgRelationNotFound(err) || errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("dashboard: read app user email: %w", err)
	}

	emailChanged = normalizeEmail(currentEmail) != normalizeEmail(email)

	query := fmt.Sprintf(`UPDATE %q."_auth_users" SET email = $1, phone = $2, role = $3, updated_at = now() WHERE id = $4`, schema)
	if emailChanged {
		query = fmt.Sprintf(`UPDATE %q."_auth_users" SET email = $1, phone = $2, role = $3, email_confirmed_at = NULL, updated_at = now() WHERE id = $4`, schema)
	}

	tag, err := pool.Exec(ctx, query, email, phone, role, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, ErrEmailConflict
		}
		if isPgRelationNotFound(err) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("dashboard: update app user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, ErrNotFound
	}
	return emailChanged, nil
}

// ResetAppUserSessions deletes all sessions for a given user in an app schema.
func ResetAppUserSessions(ctx context.Context, pool *db.Pool, schema, userID string) error {
	tag, err := pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM %q."_auth_sessions" WHERE user_id = $1`, schema),
		userID,
	)
	if err != nil {
		if isPgRelationNotFound(err) {
			return ErrNotFound
		}
		return fmt.Errorf("dashboard: reset app user sessions: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
