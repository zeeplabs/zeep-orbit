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
