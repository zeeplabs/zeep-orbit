package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

// JWTSecret is omitted from JSON when empty (list responses never populate it).
type AppRow struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	JWTSecret        string          `json:"jwt_secret,omitempty"`
	AuthEmailEnabled bool            `json:"auth_email_enabled"`
	AuthProviders    json.RawMessage `json:"auth_providers,omitempty"`
	OwnerID          string          `json:"owner_id"`
	CreatedAt        time.Time       `json:"created_at"`
	Tables           []AppTableRow   `json:"tables"`
}

// AppTableRow represents a row from zeep_system.app_tables.
type AppTableRow struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	RLS     string              `json:"rls"`
	Columns []config.ColumnConfig `json:"columns"`
}

// superadmin → all apps; admin → only apps owned by userID or listed in app_ownership.
func ListApps(ctx context.Context, pool *db.Pool, userID, role string) ([]*AppRow, error) {
	var (
		rows pgx.Rows
		err  error
	)

	if role == "superadmin" {
		rows, err = pool.Query(ctx,
			`SELECT id, name, auth_email_enabled, COALESCE(auth_providers, '{}'), owner_id, created_at
			 FROM zeep_system.apps
			 ORDER BY created_at DESC`,
		)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT DISTINCT a.id, a.name, a.auth_email_enabled, COALESCE(a.auth_providers, '{}'), a.owner_id, a.created_at
			 FROM zeep_system.apps a
			 LEFT JOIN zeep_system.app_ownership o ON o.app_id = a.id AND o.user_id = $1
			 WHERE a.owner_id = $1 OR o.user_id = $1
			 ORDER BY a.created_at DESC`,
			userID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("dashboard: list apps: %w", err)
	}
	defer rows.Close()

	var apps []*AppRow
	for rows.Next() {
		var a AppRow
		var providersJSON []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.AuthEmailEnabled, &providersJSON, &a.OwnerID, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: list apps scan: %w", err)
		}
		if len(providersJSON) > 0 {
			a.AuthProviders = providersJSON
		}
		apps = append(apps, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: list apps rows: %w", err)
	}

	for _, app := range apps {
		tables, err := loadAppTables(ctx, pool, app.ID)
		if err != nil {
			return nil, err
		}
		app.Tables = tables
	}

	return apps, nil
}

// Returns the created AppRow with ID and CreatedAt populated.
func CreateApp(ctx context.Context, pool *db.Pool, name, ownerID string, authEmail bool, tables []AppTableRow) (*AppRow, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create app begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var app AppRow
	err = tx.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id, auth_email_enabled)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), owner_id, created_at`,
		name, ownerID, authEmail,
	).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &app.OwnerID, &app.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create app insert: %w", err)
	}

	app.Tables, err = insertAppTables(ctx, tx, app.ID, tables)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.app_ownership (user_id, app_id) VALUES ($1, $2)`,
		ownerID, app.ID,
	); err != nil {
		return nil, fmt.Errorf("dashboard: create app ownership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("dashboard: create app commit: %w", err)
	}

	return &app, nil
}

// admin can only access apps they own or are members of; superadmin accesses any.
func GetApp(ctx context.Context, pool *db.Pool, appID, userID, role string) (*AppRow, error) {
	var app AppRow
	var err error

	if role == "superadmin" {
		err = pool.QueryRow(ctx,
			`SELECT id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), owner_id, created_at
			 FROM zeep_system.apps WHERE id = $1`,
			appID,
		).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &app.OwnerID, &app.CreatedAt)
	} else {
		err = pool.QueryRow(ctx,
			`SELECT DISTINCT a.id, a.name, a.jwt_secret, a.auth_email_enabled, COALESCE(a.auth_providers, '{}'), a.owner_id, a.created_at
			 FROM zeep_system.apps a
			 LEFT JOIN zeep_system.app_ownership o ON o.app_id = a.id AND o.user_id = $2
			 WHERE a.id = $1 AND (a.owner_id = $2 OR o.user_id = $2)`,
			appID, userID,
		).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &app.OwnerID, &app.CreatedAt)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dashboard: get app: %w", err)
	}

	app.Tables, err = loadAppTables(ctx, pool, app.ID)
	if err != nil {
		return nil, err
	}

	return &app, nil
}

// Ownership check is the same as GetApp.
func UpdateApp(ctx context.Context, pool *db.Pool, appID, userID, role string, authEmail bool, tables []AppTableRow) (*AppRow, error) {
	existing, err := GetApp(ctx, pool, appID, userID, role)
	if err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard: update app begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var app AppRow
	err = tx.QueryRow(ctx,
		`UPDATE zeep_system.apps
		 SET auth_email_enabled = $2
		 WHERE id = $1
		 RETURNING id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), owner_id, created_at`,
		appID, authEmail,
	).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &app.OwnerID, &app.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("dashboard: update app: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM zeep_system.app_tables WHERE app_id = $1`, appID); err != nil {
		return nil, fmt.Errorf("dashboard: update app delete tables: %w", err)
	}

	app.Tables, err = insertAppTables(ctx, tx, appID, tables)
	if err != nil {
		return nil, err
	}

	_ = existing

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("dashboard: update app commit: %w", err)
	}

	return &app, nil
}

// Ownership check is the same as GetApp.
func DeleteApp(ctx context.Context, pool *db.Pool, appID, userID, role string) error {
	if _, err := GetApp(ctx, pool, appID, userID, role); err != nil {
		return err
	}

	tag, err := pool.Exec(ctx, `DELETE FROM zeep_system.apps WHERE id = $1`, appID)
	if err != nil {
		return fmt.Errorf("dashboard: delete app: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// superadmin gets nil (no filter); admin gets only apps they own/are members of.
func ListOwnedAppNames(ctx context.Context, pool *db.Pool, userID, role string) (map[string]bool, error) {
	if role == "superadmin" {
		return nil, nil
	}

	rows, err := pool.Query(ctx,
		`SELECT DISTINCT a.name
		 FROM zeep_system.apps a
		 LEFT JOIN zeep_system.app_ownership o ON o.app_id = a.id AND o.user_id = $1
		 WHERE a.owner_id = $1 OR o.user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: list owned apps: %w", err)
	}
	defer rows.Close()

	apps := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("dashboard: scan app name: %w", err)
		}
		apps[name] = true
	}
	return apps, rows.Err()
}

// loadAppTables fetches all tables for a given app ID from the pool (not in a transaction).
func loadAppTables(ctx context.Context, pool *db.Pool, appID string) ([]AppTableRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, rls, columns FROM zeep_system.app_tables WHERE app_id = $1 ORDER BY name`,
		appID,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: load app tables: %w", err)
	}
	defer rows.Close()
	return scanAppTableRows(rows)
}

// insertAppTables inserts a slice of AppTableRow within an existing transaction.
func insertAppTables(ctx context.Context, tx pgx.Tx, appID string, tables []AppTableRow) ([]AppTableRow, error) {
	result := make([]AppTableRow, 0, len(tables))
	for _, t := range tables {
		colsJSON, err := json.Marshal(t.Columns)
		if err != nil {
			return nil, fmt.Errorf("dashboard: marshal columns for table %q: %w", t.Name, err)
		}
		var row AppTableRow
		err = tx.QueryRow(ctx,
			`INSERT INTO zeep_system.app_tables (app_id, name, rls, columns)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, name, rls, columns`,
			appID, t.Name, t.RLS, colsJSON,
		).Scan(&row.ID, &row.Name, &row.RLS, &colsJSON)
		if err != nil {
			return nil, fmt.Errorf("dashboard: insert app table %q: %w", t.Name, err)
		}
		if err := json.Unmarshal(colsJSON, &row.Columns); err != nil {
			return nil, fmt.Errorf("dashboard: unmarshal columns for table %q: %w", t.Name, err)
		}
		result = append(result, row)
	}
	return result, nil
}

// scanAppTableRows scans pgx.Rows into a slice of AppTableRow.
func scanAppTableRows(rows pgx.Rows) ([]AppTableRow, error) {
	var result []AppTableRow
	for rows.Next() {
		var t AppTableRow
		var colsJSON []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.RLS, &colsJSON); err != nil {
			return nil, fmt.Errorf("dashboard: scan app table row: %w", err)
		}
		if err := json.Unmarshal(colsJSON, &t.Columns); err != nil {
			return nil, fmt.Errorf("dashboard: unmarshal columns: %w", err)
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: app table rows: %w", err)
	}
	return result, nil
}
