package config

import "testing"

func referenceTestTables(extra func(*ColumnConfig)) []TableConfig {
	customerCol := ColumnConfig{Name: "customer_id", Type: "uuid"}
	extra(&customerCol)
	return []TableConfig{
		{
			Name: "customers",
			Columns: []ColumnConfig{
				{Name: "id", Type: "uuid", Required: true},
				{Name: "email", Type: "text", Unique: true},
				{Name: "notes", Type: "text"},
			},
		},
		{
			Name:    "orders",
			Columns: []ColumnConfig{{Name: "id", Type: "uuid", Required: true}, customerCol},
		},
	}
}

func TestValidateTables_ReferenceValid(t *testing.T) {
	tables := referenceTestTables(func(c *ColumnConfig) {
		c.References = &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "cascade"}
	})
	if err := ValidateTables(tables); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateTables_ReferenceUnknownTable(t *testing.T) {
	tables := referenceTestTables(func(c *ColumnConfig) {
		c.References = &ReferenceConfig{Table: "does_not_exist", Column: "id"}
	})
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for reference to unknown table, got nil")
	}
}

func TestValidateTables_ReferenceNonUniqueColumn(t *testing.T) {
	tables := referenceTestTables(func(c *ColumnConfig) {
		c.References = &ReferenceConfig{Table: "customers", Column: "notes"}
	})
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for reference to non-unique column, got nil")
	}
}

func TestValidateTables_ReferenceInvalidOnDelete(t *testing.T) {
	tables := referenceTestTables(func(c *ColumnConfig) {
		c.References = &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "purge"}
	})
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for invalid on_delete, got nil")
	}
}

func TestValidateTables_ReferenceSetNullRequiresOptional(t *testing.T) {
	tables := referenceTestTables(func(c *ColumnConfig) {
		c.Required = true
		c.References = &ReferenceConfig{Table: "customers", Column: "id", OnDelete: "set_null"}
	})
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for on_delete=set_null with required=true, got nil")
	}
}

func TestValidateTables_CycleRejected(t *testing.T) {
	tables := []TableConfig{
		{
			Name: "a",
			Columns: []ColumnConfig{
				{Name: "id", Type: "uuid", Required: true},
				{Name: "b_id", Type: "uuid", References: &ReferenceConfig{Table: "b", Column: "id"}},
			},
		},
		{
			Name: "b",
			Columns: []ColumnConfig{
				{Name: "id", Type: "uuid", Required: true},
				{Name: "a_id", Type: "uuid", References: &ReferenceConfig{Table: "a", Column: "id"}},
			},
		},
	}
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for circular reference, got nil")
	}
}

func TestValidateTables_SelfReferenceNotACycle(t *testing.T) {
	tables := []TableConfig{
		{
			Name: "employees",
			Columns: []ColumnConfig{
				{Name: "id", Type: "uuid", Required: true},
				{Name: "manager_id", Type: "uuid", References: &ReferenceConfig{Table: "employees", Column: "id"}},
			},
		},
	}
	if err := ValidateTables(tables); err != nil {
		t.Fatalf("expected no error for self-reference, got: %v", err)
	}
}

func TestValidateTables_IndexValid(t *testing.T) {
	tables := []TableConfig{
		{
			Name: "users",
			Columns: []ColumnConfig{
				{Name: "id", Type: "uuid", Required: true},
				{Name: "email", Type: "text"},
			},
			Indexes: []IndexConfig{{Name: "idx_users_email", Columns: []string{"email"}, Unique: true}},
		},
	}
	if err := ValidateTables(tables); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateTables_IndexUnknownColumn(t *testing.T) {
	tables := []TableConfig{
		{
			Name:    "users",
			Columns: []ColumnConfig{{Name: "id", Type: "uuid", Required: true}},
			Indexes: []IndexConfig{{Name: "idx_users_missing", Columns: []string{"does_not_exist"}}},
		},
	}
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for index on unknown column, got nil")
	}
}
