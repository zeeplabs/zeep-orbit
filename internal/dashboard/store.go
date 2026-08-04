package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// ErrNotFound is returned when a record is not found.
var ErrNotFound = errors.New("not found")

// ErrForbidden is returned when the caller has no permission to perform the
// action. The record may exist; the store layer is telling the caller "you
// can't touch this." Handlers map this to HTTP 403.
var ErrForbidden = errors.New("forbidden")

// DashboardUser represents a row in zeep_system.dashboard_users.
type DashboardUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name,omitempty"`
	PasswordHash string    `json:"-"`
	GoogleID     string    `json:"-"`
	Role         string    `json:"role"`
	Language     string    `json:"language,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	// SignIn is the derived sign-in method ("google" when the account is
	// linked to a Google identity, "email" otherwise). Not a stored column;
	// populated by list/read queries so the dashboard can show a SIGN-IN column.
	SignIn string `json:"sign_in,omitempty"`
}

// GetUserByEmail fetches a dashboard user by email.
func GetUserByEmail(ctx context.Context, pool *db.Pool, email string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`SELECT id, email, COALESCE(name, ''), password_hash, COALESCE(google_id, ''), role, COALESCE(language, 'en'), created_at
		 FROM zeep_system.dashboard_users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.GoogleID, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get user: %w", err)
	}
	return &u, nil
}

// GetUserByGoogleID fetches a dashboard user by google_id.
func GetUserByGoogleID(ctx context.Context, pool *db.Pool, googleID string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`SELECT id, email, COALESCE(name, ''), password_hash, COALESCE(google_id, ''), role, COALESCE(language, 'en'), created_at
		 FROM zeep_system.dashboard_users WHERE google_id = $1`,
		googleID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.GoogleID, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get user by google id: %w", err)
	}
	return &u, nil
}

// CreateGoogleUser creates a new dashboard user with Google OAuth (no password).
func CreateGoogleUser(ctx context.Context, pool *db.Pool, email, googleID string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, password_hash, google_id, role)
		 VALUES ($1, '', $2, 'admin')
		 RETURNING id, email, COALESCE(name, ''), password_hash, google_id, role, COALESCE(language, 'en'), created_at`,
		email, googleID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.GoogleID, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create google user: %w", err)
	}
	return &u, nil
}

// LinkGoogleID associates a Google ID with an existing dashboard user.
func LinkGoogleID(ctx context.Context, pool *db.Pool, userID, googleID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.dashboard_users SET google_id = $1 WHERE id = $2`,
		googleID, userID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: link google id: %w", err)
	}
	return nil
}

// CreateUser inserts a new dashboard user with a pre-hashed password.
func CreateUser(ctx context.Context, pool *db.Pool, email, name, passwordHash, role string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, name, password_hash, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, COALESCE(name, ''), password_hash, role, 'en', created_at`,
		email, name, passwordHash, role,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create user: %w", err)
	}
	return &u, nil
}

// UserCount returns the total number of dashboard users.
func UserCount(ctx context.Context, pool *db.Pool) (int, error) {
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.dashboard_users`,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("dashboard: user count: %w", err)
	}
	return n, nil
}

// IsBootstrapped returns true when at least one dashboard user exists.
func IsBootstrapped(ctx context.Context, pool *db.Pool) (bool, error) {
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM zeep_system.dashboard_users`,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("dashboard: is bootstrapped: %w", err)
	}
	return n > 0, nil
}

// users already exist.
func BootstrapFirstSuperadmin(ctx context.Context, pool *db.Pool, email, name, passwordHash string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("dashboard: bootstrap begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `LOCK TABLE zeep_system.dashboard_users IN EXCLUSIVE MODE`); err != nil {
		return false, fmt.Errorf("dashboard: bootstrap lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM zeep_system.dashboard_users`).Scan(&count); err != nil {
		return false, fmt.Errorf("dashboard: bootstrap count: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, name, password_hash, role) VALUES ($1, $2, $3, 'superadmin')`,
		email, name, passwordHash,
	); err != nil {
		return false, fmt.Errorf("dashboard: bootstrap insert: %w", err)
	}

	return true, tx.Commit(ctx)
}

// ListUsers returns all dashboard users (password hash excluded from results).
func ListUsers(ctx context.Context, pool *db.Pool) ([]*DashboardUser, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, email, COALESCE(name, ''), role, COALESCE(language, 'en'), created_at, COALESCE(google_id, '')
		 FROM zeep_system.dashboard_users
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list users: %w", err)
	}
	defer rows.Close()

	var users []*DashboardUser
	for rows.Next() {
		var u DashboardUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Language, &u.CreatedAt, &u.GoogleID); err != nil {
			return nil, fmt.Errorf("dashboard: list users scan: %w", err)
		}
		u.SignIn = signInMethod(u.GoogleID)
		users = append(users, &u)
	}
	return users, nil
}

// signInMethod derives the sign-in method shown in the dashboard from the
// presence of a linked Google identity.
func signInMethod(googleID string) string {
	if googleID != "" {
		return "google"
	}
	return "email"
}

// GetUser fetches a dashboard user by ID (without password hash).
func GetUser(ctx context.Context, pool *db.Pool, id string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`SELECT id, email, COALESCE(name, ''), role, COALESCE(language, 'en'), created_at
		 FROM zeep_system.dashboard_users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get user: %w", err)
	}
	return &u, nil
}

// UpdateUserName updates the name for a dashboard user.
func UpdateUserName(ctx context.Context, pool *db.Pool, userID, name string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.dashboard_users SET name = $1 WHERE id = $2`,
		name, userID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update user name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword updates the password hash for a dashboard user.
func UpdatePassword(ctx context.Context, pool *db.Pool, userID, passwordHash string) error {
	tag, err := pool.Exec(ctx,
		`UPDATE zeep_system.dashboard_users SET password_hash = $1 WHERE id = $2`,
		passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a dashboard user by ID.
func DeleteUser(ctx context.Context, pool *db.Pool, id string) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.dashboard_users WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("dashboard: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserRole updates the role of an existing dashboard user. Returns
// ErrNotFound if the user does not exist. The caller is responsible for
// authorization checks (action gate, role-creation gate, ≥1-superadmin
// invariant) — this function is a thin store layer.
func UpdateUserRole(ctx context.Context, pool *db.Pool, id, role string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`UPDATE zeep_system.dashboard_users SET role = $1 WHERE id = $2
		 RETURNING id, email, COALESCE(name, ''), role, COALESCE(language, 'en'), created_at`,
		role, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: update user role: %w", err)
	}
	return &u, nil
}

// DeleteExpiredSessions removes sessions past their expiry time.
func DeleteExpiredSessions(ctx context.Context, pool *db.Pool) error {
	_, err := pool.Exec(ctx, `DELETE FROM zeep_system.sessions WHERE expires_at <= now()`)
	if err != nil {
		return fmt.Errorf("dashboard: cleanup sessions: %w", err)
	}
	return nil
}

// CreateSession inserts a new session token.
func CreateSession(ctx context.Context, pool *db.Pool, token, userID string, expiresAt time.Time) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO zeep_system.sessions (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		token, userID, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("dashboard: create session: %w", err)
	}
	return nil
}

// GetSessionUser fetches the user for a valid (non-expired) session token.
func GetSessionUser(ctx context.Context, pool *db.Pool, token string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`SELECT u.id, u.email, COALESCE(u.name, ''), u.password_hash, COALESCE(u.google_id, ''), u.role, COALESCE(u.language, 'en'), u.created_at
		 FROM zeep_system.sessions s
		 JOIN zeep_system.dashboard_users u ON u.id = s.user_id
		 WHERE s.token = $1 AND s.expires_at > now()`,
		token,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.GoogleID, &u.Role, &u.Language, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get session user: %w", err)
	}
	return &u, nil
}

// SetUserLanguage updates the language preference for a user.
func SetUserLanguage(ctx context.Context, pool *db.Pool, userID, language string) error {
	_, err := pool.Exec(ctx,
		`UPDATE zeep_system.dashboard_users SET language = $1 WHERE id = $2`,
		language, userID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: set language: %w", err)
	}
	return nil
}

// DeleteSession removes a session by token.
func DeleteSession(ctx context.Context, pool *db.Pool, token string) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM zeep_system.sessions WHERE token = $1`,
		token,
	)
	if err != nil {
		return fmt.Errorf("dashboard: delete session: %w", err)
	}
	return nil
}
