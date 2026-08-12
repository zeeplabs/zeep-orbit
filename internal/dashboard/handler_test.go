package dashboard

import (
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
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
