package dashboard

// registry_snapshot.go — multi-replica-consistency T5: the data source for
// registry.Syncer. Every pod reads zeep_system.state_version on each tick and,
// when it moved, rebuilds its whole in-memory registry from here.

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// ReadStateVersion returns the current cross-replica state version, bumped
// by triggers on every write that changes what the registry holds.
func ReadStateVersion(ctx context.Context, pool *db.Pool) (int64, error) {
	var v int64
	if err := pool.QueryRow(ctx,
		`SELECT version FROM zeep_system.state_version WHERE id = 1`,
	).Scan(&v); err != nil {
		return 0, fmt.Errorf("dashboard: read state version: %w", err)
	}
	return v, nil
}

// LoadRegistrySnapshot loads every app (with its real secrets — this feeds
// the live registry, never a response) plus the system config, built with
// appRowToRegistryApp, the same conversion the dashboard handlers use after a
// write. A pod that just restarted and a pod that saw the write therefore
// hold identical state. Any error returns no snapshot at all, so the caller
// keeps its last good state instead of a partial one.
func LoadRegistrySnapshot(ctx context.Context, pool *db.Pool) (registry.Snapshot, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, jwt_secret, auth_email_enabled, COALESCE(auth_providers, '{}'), COALESCE(storage_config, '{}'), COALESCE(rate_limit_config, '{}'), COALESCE(enduser_roles_config, '["member"]'), owner_id, created_at
		 FROM zeep_system.apps ORDER BY name`,
	)
	if err != nil {
		return registry.Snapshot{}, fmt.Errorf("dashboard: load registry snapshot: query apps: %w", err)
	}
	var apps []*AppRow
	for rows.Next() {
		var a AppRow
		var storageJSON, rateLimitJSON, enduserRolesJSON []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.JWTSecret, &a.AuthEmailEnabled, &a.AuthProviders, &storageJSON, &rateLimitJSON, &enduserRolesJSON, &a.OwnerID, &a.CreatedAt); err != nil {
			rows.Close()
			return registry.Snapshot{}, fmt.Errorf("dashboard: load registry snapshot: scan app: %w", err)
		}
		decodeAppJSONColumns(&a, storageJSON, rateLimitJSON, enduserRolesJSON)
		apps = append(apps, &a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return registry.Snapshot{}, fmt.Errorf("dashboard: load registry snapshot: apps rows: %w", err)
	}

	regApps := make(map[string]*registry.App, len(apps))
	for _, a := range apps {
		a.Tables, err = loadAppTables(ctx, pool, a.ID)
		if err != nil {
			return registry.Snapshot{}, fmt.Errorf("dashboard: load registry snapshot: tables for %s: %w", a.Name, err)
		}
		regApps[a.Name] = appRowToRegistryApp(a)
	}

	sysCfg, err := GetSystemConfig(ctx, pool)
	if err != nil {
		return registry.Snapshot{}, fmt.Errorf("dashboard: load registry snapshot: %w", err)
	}

	return registry.Snapshot{
		Apps: regApps,
		System: registry.SystemConfig{
			SoftDeleteEnabled:  sysCfg.SoftDeleteEnabled,
			StatementTimeoutMs: sysCfg.StatementTimeoutMs,
		},
	}, nil
}
