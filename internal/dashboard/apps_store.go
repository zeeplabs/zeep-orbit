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
	"github.com/zeeplabs/zeep-orbit/internal/storage"
)

// JWTSecret is omitted from JSON when empty (list responses never populate it).
type AppRow struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	JWTSecret        string                  `json:"jwt_secret,omitempty"`
	AuthEmailEnabled bool                    `json:"auth_email_enabled"`
	AuthProviders    json.RawMessage         `json:"auth_providers,omitempty"`
	StorageConfig    *storage.StorageConfig  `json:"storage_config,omitempty"`
	RateLimit        *config.RateLimitConfig `json:"rate_limit,omitempty"`
	OwnerID          string                  `json:"owner_id"`
	OwnerEmail       string                  `json:"owner_email,omitempty"`
	OwnerName        string                  `json:"owner_name,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
	Tables           []AppTableRow           `json:"tables"`
}

// AppTableRow represents a row from zeep_system.app_tables.
type AppTableRow struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	RLS     string                `json:"rls"`
	Columns []config.ColumnConfig `json:"columns"`
	Indexes []config.IndexConfig  `json:"indexes"`
}

// superadmin or CanReadAnyApp (admin/auditor global) → all apps. Members → only
// apps in app_members for this user. ErrNotFound semantics are NOT used here —
// ListApps returns an empty list when the user can't see anything.
func ListApps(ctx context.Context, pool *db.Pool, user *DashboardUser) ([]*AppRow, error) {
	if user == nil {
		return nil, errors.New("dashboard: ListApps called with nil user")
	}

	var (
		rows pgx.Rows
		err  error
	)

	if user.Role == "superadmin" || CanReadAnyApp(user.Role) {
		rows, err = pool.Query(ctx,
			`SELECT a.id, a.name, a.auth_email_enabled, COALESCE(a.auth_providers, '{}'), COALESCE(a.storage_config, '{}'), COALESCE(a.rate_limit_config, '{}'), a.owner_id, COALESCE(u.email, ''), COALESCE(u.name, ''), a.created_at
			 FROM zeep_system.apps a
			 LEFT JOIN zeep_system.dashboard_users u ON u.id = a.owner_id
			 ORDER BY a.created_at DESC`,
		)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT a.id, a.name, a.auth_email_enabled, COALESCE(a.auth_providers, '{}'), COALESCE(a.storage_config, '{}'), COALESCE(a.rate_limit_config, '{}'), a.owner_id, COALESCE(u.email, ''), COALESCE(u.name, ''), a.created_at
			 FROM zeep_system.apps a
			 LEFT JOIN zeep_system.dashboard_users u ON u.id = a.owner_id
			 INNER JOIN zeep_system.app_members m ON m.backend_app_id = a.id AND m.user_id = $1
			 ORDER BY a.created_at DESC`,
			user.ID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("dashboard: list apps: %w", err)
	}
	defer rows.Close()

	var apps []*AppRow
	for rows.Next() {
		var a AppRow
		var providersJSON, storageJSON, rateLimitJSON []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.AuthEmailEnabled, &providersJSON, &storageJSON, &rateLimitJSON, &a.OwnerID, &a.OwnerEmail, &a.OwnerName, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: list apps scan: %w", err)
		}
		if len(providersJSON) > 0 {
			a.AuthProviders = providersJSON
		}
		if len(storageJSON) > 0 && string(storageJSON) != "{}" {
			var sc storage.StorageConfig
			if err := json.Unmarshal(storageJSON, &sc); err == nil && sc.Bucket != "" {
				a.StorageConfig = &sc
			}
		}
		if len(rateLimitJSON) > 0 && string(rateLimitJSON) != "{}" {
			var rc config.RateLimitConfig
			if json.Unmarshal(rateLimitJSON, &rc) == nil && rc.Enabled {
				a.RateLimit = &rc
			}
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

// Returns the created AppRow with ID and CreatedAt populated. Tables are
// created afterwards, one at a time, via InsertAppTable.
func CreateApp(ctx context.Context, pool *db.Pool, name, ownerID string, authEmail bool) (*AppRow, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard: create app begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var app AppRow
	var storageJSON, rateLimitJSONCreate []byte
	err = tx.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id, auth_email_enabled)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), COALESCE(storage_config, '{}'), COALESCE(rate_limit_config, '{}'), owner_id, created_at`,
		name, ownerID, authEmail,
	).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &storageJSON, &rateLimitJSONCreate, &app.OwnerID, &app.CreatedAt)
	if len(storageJSON) > 0 && string(storageJSON) != "{}" {
		var sc storage.StorageConfig
		if json.Unmarshal(storageJSON, &sc) == nil && sc.Bucket != "" {
			app.StorageConfig = &sc
		}
	}
	if len(rateLimitJSONCreate) > 0 && string(rateLimitJSONCreate) != "{}" {
		var rc config.RateLimitConfig
		if json.Unmarshal(rateLimitJSONCreate, &rc) == nil && rc.Enabled {
			app.RateLimit = &rc
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dashboard: create app insert: %w", err)
	}
	app.Tables = make([]AppTableRow, 0)

	// rbac-per-app T-08: the owner is now added directly to `app_members`
	// as `admin`, replacing the pre-rbac `app_ownership` insert. The T-02
	// migration in `ProvisionZeepSystem` handles existing apps; this is
	// the path new apps take. The partial UNIQUE index on
	// (backend_app_id, user_id) WHERE backend_app_id IS NOT NULL prevents
	// duplicates (e.g. if a future migration re-runs for some reason).
	if _, err := tx.Exec(ctx,
		`INSERT INTO zeep_system.app_members (backend_app_id, user_id, role) VALUES ($1, $2, 'admin')`,
		app.ID, ownerID,
	); err != nil {
		return nil, fmt.Errorf("dashboard: add app owner to app_members: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("dashboard: create app commit: %w", err)
	}

	return &app, nil
}

// Returns the app plus the user's role on it. The role is the source of truth
// for any further capability checks the handler wants to make. ErrNotFound is
// returned both when the app doesn't exist and when the user has no access —
// "doesn't exist for you" is the security-sensitive default.
func GetApp(ctx context.Context, pool *db.Pool, appID string, user *DashboardUser) (*AppRow, AppRole, error) {
	role, err := ResolveAppRole(ctx, pool, user, AppRef{BackendAppID: appID})
	if err != nil {
		if errors.Is(err, ErrInvalidAppRef) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	if !role.Effective() {
		return nil, "", ErrNotFound
	}

	var app AppRow
	var storageJSON, rateLimitJSONGet []byte
	err = pool.QueryRow(ctx,
		`SELECT id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), COALESCE(storage_config, '{}'), COALESCE(rate_limit_config, '{}'), owner_id, created_at
		 FROM zeep_system.apps WHERE id = $1`,
		appID,
	).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &storageJSON, &rateLimitJSONGet, &app.OwnerID, &app.CreatedAt)
	if len(storageJSON) > 0 && string(storageJSON) != "{}" {
		var sc storage.StorageConfig
		if json.Unmarshal(storageJSON, &sc) == nil && sc.Bucket != "" {
			app.StorageConfig = &sc
		}
	}
	if len(rateLimitJSONGet) > 0 && string(rateLimitJSONGet) != "{}" {
		var rc config.RateLimitConfig
		if json.Unmarshal(rateLimitJSONGet, &rc) == nil && rc.Enabled {
			app.RateLimit = &rc
		}
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("dashboard: get app: %w", err)
	}

	app.Tables, err = loadAppTables(ctx, pool, app.ID)
	if err != nil {
		return nil, "", err
	}

	return &app, role, nil
}

// Requires CanWrite() (admin/editor). Tables are managed separately via
// InsertAppTable/UpdateAppTable/DeleteAppTable, one at a time. The HTTP
// handler does its own CanManage() check on top when the request touches
// auth_providers / storage_config / rate_limit_config (management-level fields).
func UpdateApp(ctx context.Context, pool *db.Pool, appID string, user *DashboardUser, authEmail bool) (*AppRow, error) {
	existing, role, err := GetApp(ctx, pool, appID, user)
	if err != nil {
		return nil, err
	}
	if !role.CanWrite() {
		return nil, ErrForbidden
	}

	var app AppRow
	var storageJSON, rateLimitJSONUpd []byte
	err = pool.QueryRow(ctx,
		`UPDATE zeep_system.apps
		 SET auth_email_enabled = $2
		 WHERE id = $1
		 RETURNING id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), COALESCE(storage_config, '{}'), COALESCE(rate_limit_config, '{}'), owner_id, created_at`,
		appID, authEmail,
	).Scan(&app.ID, &app.Name, &app.JWTSecret, &app.AuthEmailEnabled, &app.AuthProviders, &storageJSON, &rateLimitJSONUpd, &app.OwnerID, &app.CreatedAt)
	if len(storageJSON) > 0 && string(storageJSON) != "{}" {
		var sc storage.StorageConfig
		if json.Unmarshal(storageJSON, &sc) == nil && sc.Bucket != "" {
			app.StorageConfig = &sc
		}
	}
	if len(rateLimitJSONUpd) > 0 && string(rateLimitJSONUpd) != "{}" {
		var rc config.RateLimitConfig
		if json.Unmarshal(rateLimitJSONUpd, &rc) == nil && rc.Enabled {
			app.RateLimit = &rc
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dashboard: update app: %w", err)
	}

	app.Tables = existing.Tables

	return &app, nil
}

// Requires CanManage() (admin only). Returns ErrForbidden if the user can see
// the app but cannot delete it; ErrNotFound if the app doesn't exist or the
// user has no access at all.
func DeleteApp(ctx context.Context, pool *db.Pool, appID string, user *DashboardUser) error {
	if _, role, err := GetApp(ctx, pool, appID, user); err != nil {
		return err
	} else if !role.CanManage() {
		return ErrForbidden
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

func UpdateAppRateLimitConfig(ctx context.Context, pool *db.Pool, appID string, cfg *config.RateLimitConfig) error {
	jsonCfg, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("dashboard: marshal rate limit config: %w", err)
	}
	_, err = pool.Exec(ctx,
		`UPDATE zeep_system.apps SET rate_limit_config = $1 WHERE id = $2`,
		jsonCfg, appID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update rate limit config: %w", err)
	}
	return nil
}

func UpdateAppStorageConfig(ctx context.Context, pool *db.Pool, appID string, cfg *storage.StorageConfig) error {
	jsonCfg, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("dashboard: marshal storage config: %w", err)
	}
	_, err = pool.Exec(ctx,
		`UPDATE zeep_system.apps SET storage_config = $1 WHERE id = $2`,
		jsonCfg, appID,
	)
	if err != nil {
		return fmt.Errorf("dashboard: update storage config: %w", err)
	}
	return nil
}

// ResolveAppRoleByName looks up a backend app's ID by name and resolves the
// user's effective role on it via ResolveAppRole. Used by handlers that only
// have the app name (e.g. the Data Browser, which addresses apps by name via
// the registry, not by UUID). Returns ErrNotFound if no app has that name.
func ResolveAppRoleByName(ctx context.Context, pool *db.Pool, user *DashboardUser, appName string) (AppRole, error) {
	var appID string
	err := pool.QueryRow(ctx,
		`SELECT id FROM zeep_system.apps WHERE name = $1`, appName,
	).Scan(&appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("dashboard: lookup app by name: %w", err)
	}
	return ResolveAppRole(ctx, pool, user, AppRef{BackendAppID: appID})
}

// superadmin or CanReadAnyApp (admin/auditor global) → nil (no filter).
// Members → names of apps in app_members for this user. Used by the table-name
// collision checks in the handler layer; mirrors ListApps' filter.
func ListOwnedAppNames(ctx context.Context, pool *db.Pool, user *DashboardUser) (map[string]bool, error) {
	if user == nil {
		return nil, errors.New("dashboard: ListOwnedAppNames called with nil user")
	}
	if user.Role == "superadmin" || CanReadAnyApp(user.Role) {
		return nil, nil
	}

	rows, err := pool.Query(ctx,
		`SELECT DISTINCT a.name
		 FROM zeep_system.apps a
		 INNER JOIN zeep_system.app_members m ON m.backend_app_id = a.id AND m.user_id = $1`,
		user.ID,
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
		`SELECT id, name, rls, columns, indexes FROM zeep_system.app_tables WHERE app_id = $1 ORDER BY name`,
		appID,
	)
	if err != nil {
		return nil, fmt.Errorf("dashboard: load app tables: %w", err)
	}
	defer rows.Close()
	return scanAppTableRows(rows)
}

// InsertAppTable creates a single table for an app and returns the saved row.
func InsertAppTable(ctx context.Context, pool *db.Pool, appID string, t AppTableRow) (AppTableRow, error) {
	if t.Indexes == nil {
		t.Indexes = []config.IndexConfig{}
	}
	colsJSON, err := json.Marshal(t.Columns)
	if err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: marshal columns for table %q: %w", t.Name, err)
	}
	idxJSON, err := json.Marshal(t.Indexes)
	if err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: marshal indexes for table %q: %w", t.Name, err)
	}
	var row AppTableRow
	err = pool.QueryRow(ctx,
		`INSERT INTO zeep_system.app_tables (app_id, name, rls, columns, indexes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, rls, columns, indexes`,
		appID, t.Name, t.RLS, colsJSON, idxJSON,
	).Scan(&row.ID, &row.Name, &row.RLS, &colsJSON, &idxJSON)
	if err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: insert app table %q: %w", t.Name, err)
	}
	if err := json.Unmarshal(colsJSON, &row.Columns); err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: unmarshal columns for table %q: %w", t.Name, err)
	}
	if err := json.Unmarshal(idxJSON, &row.Indexes); err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: unmarshal indexes for table %q: %w", t.Name, err)
	}
	return row, nil
}

// UpdateAppTable updates rls/columns/indexes of an existing table. The table
// name is immutable once created — renaming would require renaming the
// physical table too, out of scope here.
func UpdateAppTable(ctx context.Context, pool *db.Pool, appID, tableID, rls string, columns []config.ColumnConfig, indexes []config.IndexConfig) (AppTableRow, error) {
	if indexes == nil {
		indexes = []config.IndexConfig{}
	}
	colsJSON, err := json.Marshal(columns)
	if err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: marshal columns for table %s: %w", tableID, err)
	}
	idxJSON, err := json.Marshal(indexes)
	if err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: marshal indexes for table %s: %w", tableID, err)
	}
	var row AppTableRow
	err = pool.QueryRow(ctx,
		`UPDATE zeep_system.app_tables
		 SET rls = $3, columns = $4, indexes = $5
		 WHERE id = $1 AND app_id = $2
		 RETURNING id, name, rls, columns, indexes`,
		tableID, appID, rls, colsJSON, idxJSON,
	).Scan(&row.ID, &row.Name, &row.RLS, &colsJSON, &idxJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AppTableRow{}, ErrNotFound
		}
		return AppTableRow{}, fmt.Errorf("dashboard: update app table %s: %w", tableID, err)
	}
	if err := json.Unmarshal(colsJSON, &row.Columns); err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: unmarshal columns for table %s: %w", tableID, err)
	}
	if err := json.Unmarshal(idxJSON, &row.Indexes); err != nil {
		return AppTableRow{}, fmt.Errorf("dashboard: unmarshal indexes for table %s: %w", tableID, err)
	}
	return row, nil
}

// DeleteAppTable removes a table's metadata row and returns its name so the
// caller can drop the physical table too.
func DeleteAppTable(ctx context.Context, pool *db.Pool, appID, tableID string) (string, error) {
	var name string
	err := pool.QueryRow(ctx,
		`DELETE FROM zeep_system.app_tables WHERE id = $1 AND app_id = $2 RETURNING name`,
		tableID, appID,
	).Scan(&name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("dashboard: delete app table %s: %w", tableID, err)
	}
	return name, nil
}

// scanAppTableRows scans pgx.Rows into a slice of AppTableRow. Always
// returns a non-nil slice (even when empty) so it serializes as JSON "[]"
// instead of "null" — the frontend always expects an array to call
// .length/.map on.
func scanAppTableRows(rows pgx.Rows) ([]AppTableRow, error) {
	result := make([]AppTableRow, 0)
	for rows.Next() {
		var t AppTableRow
		var colsJSON, idxJSON []byte
		if err := rows.Scan(&t.ID, &t.Name, &t.RLS, &colsJSON, &idxJSON); err != nil {
			return nil, fmt.Errorf("dashboard: scan app table row: %w", err)
		}
		if err := json.Unmarshal(colsJSON, &t.Columns); err != nil {
			return nil, fmt.Errorf("dashboard: unmarshal columns: %w", err)
		}
		if err := json.Unmarshal(idxJSON, &t.Indexes); err != nil {
			return nil, fmt.Errorf("dashboard: unmarshal indexes: %w", err)
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: app table rows: %w", err)
	}
	return result, nil
}
