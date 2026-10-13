package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
)

func columnNotNullInDB(t *testing.T, pool *db.Pool, schema, table, column string) bool {
	t.Helper()
	var nullable string
	if err := pool.QueryRow(context.Background(),
		`SELECT is_nullable FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
		schema, table, column,
	).Scan(&nullable); err != nil {
		t.Fatalf("read is_nullable %s.%s.%s: %v", schema, table, column, err)
	}
	return nullable == "NO"
}

func storedColumn(t *testing.T, pool *db.Pool, appID string, actor *DashboardUser, table, column string) config.ColumnConfig {
	t.Helper()
	app, _, err := GetApp(context.Background(), pool, appID, actor)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	tbl := findAppTableByName(app, table)
	if tbl == nil {
		t.Fatalf("table %q not found", table)
	}
	for _, c := range tbl.Columns {
		if c.Name == column {
			return c
		}
	}
	t.Fatalf("column %q not found in stored metadata", column)
	return config.ColumnConfig{}
}

// CCC-01: the production incident (asset-manager assets.serial, 2026-10-13).
// Turning "required" off on an existing column must reach Postgres, not just
// the stored metadata.
func TestUpdateAppTable_DropRequiredOnExistingColumnAltersDatabase(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "ccc-drop-req")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	assets, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "serial", Type: "text", Required: true}, {Name: "model", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	schema := schemaNameForDB(app.Name)

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, assets.ID, TableRequestBody{
		RLS:     assets.RLS,
		Columns: []config.ColumnConfig{{Name: "serial", Type: "text"}, {Name: "model", Type: "text"}},
		Indexes: assets.Indexes,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if columnNotNullInDB(t, pool, schema, "assets", "serial") {
		t.Fatal("serial is still NOT NULL in Postgres — metadata and database diverged again")
	}
	if storedColumn(t, pool, app.ID, actors["loner"], "assets", "serial").Required {
		t.Fatal("stored metadata should say required=false")
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (model) VALUES ('x')`, schema)); err != nil {
		t.Fatalf("insert without serial must succeed: %v", err)
	}
}

// CCC-04: making a column with NULL rows required is a 400 naming the column
// and row count; neither Postgres nor the stored metadata changes.
func TestUpdateAppTable_SetRequiredWithNullRowsRejected(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "ccc-set-req")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	assets, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "serial", Type: "text"}, {Name: "model", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	schema := schemaNameForDB(app.Name)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (model) VALUES ('no serial')`, schema)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, assets.ID, TableRequestBody{
		RLS: assets.RLS,
		Columns: []config.ColumnConfig{
			{Name: "serial", Type: "text", Required: true},
			// A brand-new column in the same request must not be added
			// either: the rejection happens before any DDL.
			{Name: "model", Type: "text"}, {Name: "notes", Type: "text"},
		},
		Indexes: assets.Indexes,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `cannot make \"serial\" required: 1 existing row(s) have no value`) {
		t.Fatalf("error must name column and count, got %s", w.Body.String())
	}
	if columnNotNullInDB(t, pool, schema, "assets", "serial") {
		t.Fatal("rejected request must not alter the column")
	}
	if storedColumn(t, pool, app.ID, actors["loner"], "assets", "serial").Required {
		t.Fatal("rejected request must not persist metadata")
	}
	var notesExists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'assets' AND column_name = 'notes')`,
		schema,
	).Scan(&notesExists); err != nil {
		t.Fatalf("check notes column: %v", err)
	}
	if notesExists {
		t.Fatal("a rejected request must not add the brand-new column either")
	}
}

// CCC-04: adding a new required column without default to a non-empty table
// used to fail as a 500 "provisioning failed"; it is now a 400 that says why.
func TestUpdateAppTable_AddRequiredColumnWithoutDefaultOnNonEmptyTable(t *testing.T) {
	pool, h, actors, _, _ := appsHandlerTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	app, err := h.CreateAppForUser(ctx, actors["loner"], AppRequestBody{Name: uniqueAppName(t, "ccc-add-req")}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppForUser: %v", err)
	}
	assets, err := h.CreateAppTableForUser(ctx, actors["loner"], app.ID, TableRequestBody{
		Name:    "assets",
		Columns: []config.ColumnConfig{{Name: "model", Type: "text"}},
	}, "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateAppTableForUser: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %q.assets (model) VALUES ('x')`, schemaNameForDB(app.Name))); err != nil {
		t.Fatalf("seed: %v", err)
	}

	w := callUpdateAppTable(t, h, actors["loner"], app.ID, assets.ID, TableRequestBody{
		RLS:     assets.RLS,
		Columns: []config.ColumnConfig{{Name: "model", Type: "text"}, {Name: "starbem_code", Type: "text", Required: true}},
		Indexes: assets.Indexes,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `cannot add required column \"starbem_code\" without a default`) {
		t.Fatalf("unexpected error body: %s", w.Body.String())
	}
}
