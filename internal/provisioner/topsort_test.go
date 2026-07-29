package provisioner

import (
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

func TestTopoSortTables_OrdersReferencedTableFirst(t *testing.T) {
	tables := []config.TableConfig{
		{
			Name: "orders",
			Columns: []config.ColumnConfig{
				{Name: "id", Type: "uuid"},
				{Name: "customer_id", Type: "uuid", References: &config.ReferenceConfig{Table: "customers", Column: "id"}},
			},
		},
		{
			Name: "customers",
			Columns: []config.ColumnConfig{
				{Name: "id", Type: "uuid"},
			},
		},
	}

	ordered, err := topoSortTables(tables)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 2 || ordered[0].Name != "customers" || ordered[1].Name != "orders" {
		t.Fatalf("expected [customers, orders], got %v", tableNames(ordered))
	}
}

func TestTopoSortTables_SelfReferenceDoesNotBlock(t *testing.T) {
	tables := []config.TableConfig{
		{
			Name: "employees",
			Columns: []config.ColumnConfig{
				{Name: "id", Type: "uuid"},
				{Name: "manager_id", Type: "uuid", References: &config.ReferenceConfig{Table: "employees", Column: "id"}},
			},
		},
	}

	ordered, err := topoSortTables(tables)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 1 {
		t.Fatalf("expected 1 table, got %d", len(ordered))
	}
}

func TestTopoSortTables_CycleDetected(t *testing.T) {
	tables := []config.TableConfig{
		{
			Name: "a",
			Columns: []config.ColumnConfig{
				{Name: "id", Type: "uuid"},
				{Name: "b_id", Type: "uuid", References: &config.ReferenceConfig{Table: "b", Column: "id"}},
			},
		},
		{
			Name: "b",
			Columns: []config.ColumnConfig{
				{Name: "id", Type: "uuid"},
				{Name: "a_id", Type: "uuid", References: &config.ReferenceConfig{Table: "a", Column: "id"}},
			},
		},
	}

	if _, err := topoSortTables(tables); err == nil {
		t.Fatal("expected error for circular dependency, got nil")
	}
}

func tableNames(tables []config.TableConfig) []string {
	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = t.Name
	}
	return names
}
