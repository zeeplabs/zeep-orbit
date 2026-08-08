package dashboard

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func appsStoreTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}

	if err := ProvisionZeepSystem(ctx, pool); err != nil {
		t.Fatalf("provision zeep_system: %v", err)
	}

	if _, err := pool.Exec(ctx, `TRUNCATE zeep_system.app_tables, zeep_system.apps, zeep_system.dashboard_users CASCADE`); err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE zeep_system.app_tables, zeep_system.apps, zeep_system.dashboard_users CASCADE`)
	})

	return pool
}

func appsStoreTestApp(t *testing.T, pool *db.Pool) (appID string) {
	t.Helper()
	ownerID := testUser(t, pool, "owner@example.com", "superadmin")
	err := pool.QueryRow(context.Background(),
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"test-app", ownerID,
	).Scan(&appID)
	if err != nil {
		t.Fatalf("create test app: %v", err)
	}
	return appID
}

func TestInsertAppTable_RoundTripsIndexes(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	table := AppTableRow{
		Name: "users",
		RLS:  "",
		Columns: []config.ColumnConfig{
			{Name: "email", Type: "text"},
		},
		Indexes: []config.IndexConfig{
			{Name: "idx_users_email", Columns: []string{"email"}, Unique: true},
		},
	}

	row, err := InsertAppTable(context.Background(), pool, appID, table)
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}
	if len(row.Indexes) != 1 || row.Indexes[0].Name != "idx_users_email" {
		t.Fatalf("expected indexes to round-trip, got %+v", row.Indexes)
	}

	loaded, err := loadAppTables(context.Background(), pool, appID)
	if err != nil {
		t.Fatalf("loadAppTables: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Indexes) != 1 {
		t.Fatalf("expected loaded table to carry indexes, got %+v", loaded)
	}
}

// TestEnduserRolesConfig_DefaultsAndRoundTrips covers ROLECFG-01/ROLECFG-07:
// a newly created app gets the seeded ["member"] default, and GetApp/ListApps
// decode a custom list back exactly as persisted.
func TestEnduserRolesConfig_DefaultsAndRoundTrips(t *testing.T) {
	pool := appsStoreTestPool(t)
	ownerID := testUser(t, pool, "roles-owner@example.com", "superadmin")
	owner, err := GetUser(context.Background(), pool, ownerID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	created, err := CreateApp(context.Background(), pool, "roles-app", ownerID, true)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if len(created.EnduserRolesConfig) != 1 || created.EnduserRolesConfig[0] != "member" {
		t.Fatalf("CreateApp: expected default [\"member\"], got %+v", created.EnduserRolesConfig)
	}

	got, _, err := GetApp(context.Background(), pool, created.ID, owner)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if len(got.EnduserRolesConfig) != 1 || got.EnduserRolesConfig[0] != "member" {
		t.Fatalf("GetApp: expected default [\"member\"], got %+v", got.EnduserRolesConfig)
	}

	// Persist a custom list directly (bypassing the store's own writer, which
	// is added in a later task) and confirm both GetApp and ListApps decode
	// it back exactly.
	if _, err := pool.Exec(context.Background(),
		`UPDATE zeep_system.apps SET enduser_roles_config = $1 WHERE id = $2`,
		`["member","viewer"]`, created.ID,
	); err != nil {
		t.Fatalf("seed custom roles: %v", err)
	}

	got, _, err = GetApp(context.Background(), pool, created.ID, owner)
	if err != nil {
		t.Fatalf("GetApp after custom roles: %v", err)
	}
	if len(got.EnduserRolesConfig) != 2 || got.EnduserRolesConfig[0] != "member" || got.EnduserRolesConfig[1] != "viewer" {
		t.Fatalf("GetApp: expected [\"member\",\"viewer\"], got %+v", got.EnduserRolesConfig)
	}

	list, err := ListApps(context.Background(), pool, owner)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	var found *AppRow
	for _, a := range list {
		if a.ID == created.ID {
			found = a
		}
	}
	if found == nil {
		t.Fatalf("ListApps: created app not found")
	}
	if len(found.EnduserRolesConfig) != 2 || found.EnduserRolesConfig[0] != "member" || found.EnduserRolesConfig[1] != "viewer" {
		t.Fatalf("ListApps: expected [\"member\",\"viewer\"], got %+v", found.EnduserRolesConfig)
	}
}

func TestUpdateAppTable_RoundTripsIndexes(t *testing.T) {
	pool := appsStoreTestPool(t)
	appID := appsStoreTestApp(t, pool)

	created, err := InsertAppTable(context.Background(), pool, appID, AppTableRow{
		Name:    "users",
		Columns: []config.ColumnConfig{{Name: "email", Type: "text"}},
	})
	if err != nil {
		t.Fatalf("InsertAppTable: %v", err)
	}

	newIndexes := []config.IndexConfig{{Name: "idx_users_email", Columns: []string{"email"}, Unique: true}}
	updated, err := UpdateAppTable(context.Background(), pool, appID, created.ID, "", created.Columns, newIndexes)
	if err != nil {
		t.Fatalf("UpdateAppTable: %v", err)
	}
	if len(updated.Indexes) != 1 || updated.Indexes[0].Name != "idx_users_email" {
		t.Fatalf("expected updated indexes to round-trip, got %+v", updated.Indexes)
	}
}
