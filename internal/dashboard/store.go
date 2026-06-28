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

// DashboardUser represents a row in zeep_system.dashboard_users.
type DashboardUser struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	GoogleID     string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// GetUserByEmail fetches a dashboard user by email.
func GetUserByEmail(ctx context.Context, pool *db.Pool, email string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`SELECT id, email, password_hash, google_id, role, created_at
		 FROM zeep_system.dashboard_users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.GoogleID, &u.Role, &u.CreatedAt)
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
		`SELECT id, email, password_hash, google_id, role, created_at
		 FROM zeep_system.dashboard_users WHERE google_id = $1`,
		googleID,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.GoogleID, &u.Role, &u.CreatedAt)
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
		 RETURNING id, email, password_hash, google_id, role, created_at`,
		email, googleID,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.GoogleID, &u.Role, &u.CreatedAt)
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
func CreateUser(ctx context.Context, pool *db.Pool, email, passwordHash, role string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, password_hash, role)
		 VALUES ($1, $2, $3)
		 RETURNING id, email, password_hash, role, created_at`,
		email, passwordHash, role,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
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

// BootstrapFirstSuperadmin atomically inserts the first superadmin using an exclusive
// table lock to prevent TOCTOU races. Returns (true, nil) on creation, (false, nil) if
// users already exist.
func BootstrapFirstSuperadmin(ctx context.Context, pool *db.Pool, email, passwordHash string) (bool, error) {
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
		`INSERT INTO zeep_system.dashboard_users (email, password_hash, role) VALUES ($1, $2, 'superadmin')`,
		email, passwordHash,
	); err != nil {
		return false, fmt.Errorf("dashboard: bootstrap insert: %w", err)
	}

	return true, tx.Commit(ctx)
}

// ListUsers returns all dashboard users (password hash excluded from results).
func ListUsers(ctx context.Context, pool *db.Pool) ([]*DashboardUser, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, email, role, created_at
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
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: list users scan: %w", err)
		}
		users = append(users, &u)
	}
	return users, nil
}

// GetUser fetches a dashboard user by ID (without password hash).
func GetUser(ctx context.Context, pool *db.Pool, id string) (*DashboardUser, error) {
	var u DashboardUser
	err := pool.QueryRow(ctx,
		`SELECT id, email, role, created_at
		 FROM zeep_system.dashboard_users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get user: %w", err)
	}
	return &u, nil
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
		`SELECT u.id, u.email, u.password_hash, u.google_id, u.role, u.created_at
		 FROM zeep_system.sessions s
		 JOIN zeep_system.dashboard_users u ON u.id = s.user_id
		 WHERE s.token = $1 AND s.expires_at > now()`,
		token,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.GoogleID, &u.Role, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get session user: %w", err)
	}
	return &u, nil
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
