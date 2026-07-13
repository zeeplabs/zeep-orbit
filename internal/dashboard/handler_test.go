package dashboard

import (
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

func TestValidateAppInputDuplicateTableName(t *testing.T) {
	tables := []AppTableRow{
		{Name: "clientes", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}},
		{Name: "clientes", Columns: []config.ColumnConfig{{Name: "email", Type: "text"}}},
	}
	if err := validateAppInput("meu-app", true, tables); err == nil {
		t.Fatal("expected error for duplicate table name, got nil")
	}
}

func TestValidateAppInputDuplicateColumnName(t *testing.T) {
	tables := []AppTableRow{
		{Name: "clientes", Columns: []config.ColumnConfig{
			{Name: "nome", Type: "text"},
			{Name: "nome", Type: "text"},
		}},
	}
	if err := validateAppInput("meu-app", true, tables); err == nil {
		t.Fatal("expected error for duplicate column name, got nil")
	}
}

func TestValidateAppInputRejectsRLSWithoutEmailAuth(t *testing.T) {
	tables := []AppTableRow{
		{Name: "clientes", RLS: "enabled", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}},
	}
	if err := validateAppInput("meu-app", false, tables); err == nil {
		t.Fatal("expected error for RLS-enabled table without email auth, got nil")
	}
}

func TestValidateAppInputAcceptsRLSWithEmailAuth(t *testing.T) {
	tables := []AppTableRow{
		{Name: "clientes", RLS: "enabled", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}},
	}
	if err := validateAppInput("meu-app", true, tables); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateAppInputAcceptsDistinctNames(t *testing.T) {
	tables := []AppTableRow{
		{Name: "clientes", Columns: []config.ColumnConfig{{Name: "nome", Type: "text"}}},
		{Name: "pedidos", Columns: []config.ColumnConfig{{Name: "total", Type: "numeric"}}},
	}
	if err := validateAppInput("meu-app", true, tables); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
