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
