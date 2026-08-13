package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-orbit/internal/config"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

func TestValidateAppInputAcceptsValidName(t *testing.T) {
	if err := validateAppInput("meu-app"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateAppInputRejectsInvalidName(t *testing.T) {
	if err := validateAppInput("Meu App"); err == nil {
		t.Fatal("expected error for invalid app name, got nil")
	}
}

func TestValidateTableInputDuplicateTableName(t *testing.T) {
	t1 := AppTableRow{Name: "clientes", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}}
	others := []AppTableRow{{Name: "clientes"}}
	if err := validateTableInput(t1, true, others); err == nil {
		t.Fatal("expected error for duplicate table name, got nil")
	}
}

func TestValidateTableInputDuplicateColumnName(t *testing.T) {
	tbl := AppTableRow{
		Name: "clientes",
		Columns: []config.ColumnConfig{
			{Name: "nome", Type: "text"},
			{Name: "nome", Type: "text"},
		},
	}
	if err := validateTableInput(tbl, true, nil); err == nil {
		t.Fatal("expected error for duplicate column name, got nil")
	}
}

func TestValidateTableInputRejectsRLSWithoutEmailAuth(t *testing.T) {
	tbl := AppTableRow{Name: "clientes", RLS: "enabled", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}}
	if err := validateTableInput(tbl, false, nil); err == nil {
		t.Fatal("expected error for RLS-enabled table without email auth, got nil")
	}
}

func TestValidateTableInputAcceptsRLSWithEmailAuth(t *testing.T) {
	tbl := AppTableRow{Name: "clientes", RLS: "enabled", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}}
	if err := validateTableInput(tbl, true, nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateTableInputAcceptsDistinctNameFromOthers(t *testing.T) {
	tbl := AppTableRow{Name: "pedidos", Columns: []config.ColumnConfig{{Name: "total", Type: "numeric"}}}
	others := []AppTableRow{{Name: "clientes"}}
	if err := validateTableInput(tbl, true, others); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateTableInputRejectsUnsupportedColumnType(t *testing.T) {
	tbl := AppTableRow{Name: "clientes", Columns: []config.ColumnConfig{{Name: "nome", Type: "money"}}}
	if err := validateTableInput(tbl, true, nil); err == nil {
		t.Fatal("expected error for unsupported column type, got nil")
	}
}

func TestValidateTableInputRejectsReferenceToUnknownTable(t *testing.T) {
	tbl := AppTableRow{
		Name: "pedidos",
		Columns: []config.ColumnConfig{
			{Name: "cliente_id", Type: "uuid", References: &config.ReferenceConfig{Table: "clientes", Column: "id"}},
		},
	}
	if err := validateTableInput(tbl, true, nil); err == nil {
		t.Fatal("expected error for reference to a table not in the app, got nil")
	}
}

func TestValidateTableInputAcceptsValidReference(t *testing.T) {
	tbl := AppTableRow{
		Name: "pedidos",
		Columns: []config.ColumnConfig{
			{Name: "cliente_id", Type: "uuid", References: &config.ReferenceConfig{Table: "clientes", Column: "id"}},
		},
	}
	others := []AppTableRow{{Name: "clientes", Columns: []config.ColumnConfig{{Name: "id", Type: "uuid"}}}}
	if err := validateTableInput(tbl, true, others); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestValidateTableInputRejectsUnknownRLS covers rls-policy-mode spec AC
// P1-7 / RLSP-09: any rls value outside "", "owner", "enabled", "policy"
// (a typo like "disabled") must be rejected with a clear error, closing the
// pre-existing gap where an unrecognized value silently fell through to
// the public/no-filter branch.
func TestValidateTableInputRejectsUnknownRLS(t *testing.T) {
	tbl := AppTableRow{Name: "clientes", RLS: "disabled", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}}
	err := validateTableInput(tbl, true, nil)
	if err == nil {
		t.Fatal("expected error for unrecognized rls value, got nil")
	}
	wantMsg := `table clientes has an invalid rls value: disabled (must be one of "", "owner", "enabled", "policy")`
	if err.Error() != wantMsg {
		t.Fatalf("err = %q, want %q", err.Error(), wantMsg)
	}
}

// TestValidateTableInputAcceptsPolicyWithEmailAuth covers RLSP-10: "policy"
// is accepted exactly like "owner"/"enabled" when email auth is on.
func TestValidateTableInputAcceptsPolicyWithEmailAuth(t *testing.T) {
	tbl := AppTableRow{Name: "posts", RLS: "policy", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}}}
	if err := validateTableInput(tbl, true, nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestValidateTableInputRejectsPolicyWithoutEmailAuth covers spec Edge
// Cases: "policy" needs owner_id -> _auth_users FK just like "owner"/
// "enabled", so it must be rejected without email auth too.
func TestValidateTableInputRejectsPolicyWithoutEmailAuth(t *testing.T) {
	tbl := AppTableRow{Name: "posts", RLS: "policy", Columns: []config.ColumnConfig{{Name: "title", Type: "text"}}}
	err := validateTableInput(tbl, false, nil)
	if err == nil {
		t.Fatal("expected error for rls:policy table without email auth, got nil")
	}
	wantMsg := `table posts uses restricted access (RLS), which requires 'Email Authentication' to be enabled for this app`
	if err.Error() != wantMsg {
		t.Fatalf("err = %q, want %q", err.Error(), wantMsg)
	}
}

func TestValidateTableInputRejectsIndexOnUnknownColumn(t *testing.T) {
	tbl := AppTableRow{
		Name:    "clientes",
		Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}},
		Indexes: []config.IndexConfig{{Name: "idx_missing", Columns: []string{"does_not_exist"}}},
	}
	if err := validateTableInput(tbl, true, nil); err == nil {
		t.Fatal("expected error for index on unknown column, got nil")
	}
}

// dataBrowserColumnsFor registers a single-table app in a fresh registry and
// calls the real ListDataBrowserApps handler as a superadmin (bypasses the
// ownership filter, so no app row needs to exist in zeep_system.apps),
// returning the column names the Data Browser would list for that table.
// Covers T9's "Done when": internal/dashboard/handler.go:1899 must recognize
// owner_id for "owner", "enabled" and "policy" — not just "owner".
func dataBrowserColumnsFor(t *testing.T, rls string) []string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)

	reg := registry.New()
	reg.Register(&registry.App{
		Config:     config.AppConfig{Name: "databrowser-test-app"},
		SchemaName: "databrowser_test_app",
		Tables: map[string]*registry.Table{
			"widgets": {Name: "widgets", RLS: rls},
		},
	})

	h := NewHandler(pool, reg, zap.NewNop())
	user := &DashboardUser{ID: "00000000-0000-0000-0000-000000000001", Role: "superadmin"}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/data-browser/apps", nil)
	req = req.WithContext(context.WithValue(req.Context(), userCtxKey, user))
	w := httptest.NewRecorder()

	h.ListDataBrowserApps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var apps []DataBrowserApp
	if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(apps) != 1 || len(apps[0].Tables) != 1 {
		t.Fatalf("expected 1 app with 1 table, got %+v", apps)
	}

	names := make([]string, len(apps[0].Tables[0].Columns))
	for i, c := range apps[0].Tables[0].Columns {
		names[i] = c.Name
	}
	return names
}

func containsColumn(cols []string, name string) bool {
	for _, c := range cols {
		if c == name {
			return true
		}
	}
	return false
}

// TestListDataBrowserApps_EnabledRLSShowsOwnerIDColumn covers the
// pre-existing gap fixed by this task: before the predicate swap, the Data
// Browser only recognized "owner", never "enabled", even though both modes
// have the owner_id column physically present.
func TestListDataBrowserApps_EnabledRLSShowsOwnerIDColumn(t *testing.T) {
	cols := dataBrowserColumnsFor(t, "enabled")
	if !containsColumn(cols, "owner_id") {
		t.Fatalf("expected owner_id column for rls: enabled, got %v", cols)
	}
}

// TestListDataBrowserApps_PolicyRLSShowsOwnerIDColumn covers T9's "Done
// when": rls: "policy" must also show owner_id, same as "owner"/"enabled".
func TestListDataBrowserApps_PolicyRLSShowsOwnerIDColumn(t *testing.T) {
	cols := dataBrowserColumnsFor(t, "policy")
	if !containsColumn(cols, "owner_id") {
		t.Fatalf("expected owner_id column for rls: policy, got %v", cols)
	}
}

// TestListDataBrowserApps_NoRLSDoesNotShowOwnerIDColumn is the negative
// regression case: a table with no RLS at all has no owner_id column and
// must not list it.
func TestListDataBrowserApps_NoRLSDoesNotShowOwnerIDColumn(t *testing.T) {
	cols := dataBrowserColumnsFor(t, "")
	if containsColumn(cols, "owner_id") {
		t.Fatalf("expected no owner_id column for rls: \"\", got %v", cols)
	}
}
