package config

import (
	"fmt"
	"strings"
	"testing"
)

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

// authUsersRefTable builds a single-table config with one column
// (requester_id) referencing "_auth_users" — "_auth_users" deliberately
// never appears in this table set, since it's provisioned separately per
// app (not one of app_tables), mirroring the real scenario ValidateTables
// must accept per ROWPOL-21.
func authUsersRefTable(refCol string, colType string, onDelete string) []TableConfig {
	return []TableConfig{
		{
			Name: "requests",
			Columns: []ColumnConfig{
				{Name: "id", Type: "uuid", Required: true},
				{
					Name:       "requester_id",
					Type:       colType,
					References: &ReferenceConfig{Table: "_auth_users", Column: refCol, OnDelete: onDelete},
				},
			},
		},
	}
}

func TestValidateTables_ReferenceAuthUsersValid(t *testing.T) {
	tables := authUsersRefTable("id", "uuid", "cascade")
	if err := ValidateTables(tables); err != nil {
		t.Fatalf("expected no error for a valid _auth_users.id reference, got: %v", err)
	}
}

func TestValidateTables_ReferenceAuthUsersWrongType(t *testing.T) {
	tables := authUsersRefTable("id", "text", "")
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error when the referencing column is not uuid, got nil")
	}
}

func TestValidateTables_ReferenceAuthUsersWrongColumn(t *testing.T) {
	tables := authUsersRefTable("role", "uuid", "")
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error when referencing a column other than _auth_users.id, got nil")
	}
}

func TestValidateTables_ReferenceAuthUsersInvalidOnDelete(t *testing.T) {
	tables := authUsersRefTable("id", "uuid", "purge")
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for invalid on_delete on an _auth_users reference, got nil")
	}
}

func TestValidateTables_ReferenceAuthUsersSetNullRequiresOptional(t *testing.T) {
	tables := authUsersRefTable("id", "uuid", "set_null")
	tables[0].Columns[1].Required = true
	if err := ValidateTables(tables); err == nil {
		t.Fatal("expected error for on_delete=set_null with required=true on an _auth_users reference, got nil")
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

// --- enum column type (CENUM-04, CENUM-05, CENUM-06) ---

// CENUM-04: an empty allowed-value list is rejected.
func TestValidateEnumValues_EmptyListRejected(t *testing.T) {
	err := ValidateEnumValues(nil)
	if err == nil {
		t.Fatal("expected error for empty allowed_values, got nil")
	}
	if !strings.Contains(err.Error(), "at least one value") {
		t.Errorf("error should identify the empty-list rule, got: %v", err)
	}
}

// CENUM-04 / edge case: more than 50 values is rejected, naming the limit.
func TestValidateEnumValues_TooManyValuesRejected(t *testing.T) {
	values := make([]string, MaxEnumValues+1)
	for i := range values {
		values[i] = fmt.Sprintf("v%d", i)
	}
	err := ValidateEnumValues(values)
	if err == nil {
		t.Fatalf("expected error for %d values, got nil", len(values))
	}
	if !strings.Contains(err.Error(), "51") || !strings.Contains(err.Error(), "50") {
		t.Errorf("error should state the actual count and the limit, got: %v", err)
	}

	// Exactly at the cap is still valid.
	if err := ValidateEnumValues(values[:MaxEnumValues]); err != nil {
		t.Errorf("expected %d values to be accepted, got: %v", MaxEnumValues, err)
	}
}

// CENUM-04 / edge case: an empty entry is rejected, identifying the entry.
func TestValidateEnumValues_EmptyEntryRejected(t *testing.T) {
	err := ValidateEnumValues([]string{"pending", "", "closed"})
	if err == nil {
		t.Fatal("expected error for empty entry, got nil")
	}
	if !strings.Contains(err.Error(), "allowed_values[1]") || !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should identify entry index 1 as empty, got: %v", err)
	}
}

// CENUM-04 / edge case: an entry over 100 characters is rejected.
func TestValidateEnumValues_EntryTooLongRejected(t *testing.T) {
	tooLong := strings.Repeat("a", MaxEnumValueLen+1)
	err := ValidateEnumValues([]string{"ok", tooLong})
	if err == nil {
		t.Fatal("expected error for over-long entry, got nil")
	}
	if !strings.Contains(err.Error(), "allowed_values[1]") || !strings.Contains(err.Error(), "101") {
		t.Errorf("error should identify entry index 1 and its length, got: %v", err)
	}

	// Exactly at the cap is still valid, counted in characters (not bytes),
	// so a 100-rune accented value must pass.
	if err := ValidateEnumValues([]string{strings.Repeat("á", MaxEnumValueLen)}); err != nil {
		t.Errorf("expected a %d-character value to be accepted, got: %v", MaxEnumValueLen, err)
	}
}

// CENUM-04 / edge case: an exact-match duplicate is rejected, naming the value.
func TestValidateEnumValues_DuplicateEntryRejected(t *testing.T) {
	err := ValidateEnumValues([]string{"pending", "active", "pending"})
	if err == nil {
		t.Fatal("expected error for duplicate entry, got nil")
	}
	if !strings.Contains(err.Error(), `"pending"`) || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should name the duplicated value, got: %v", err)
	}
}

// CENUM-06: free-text values (spaces, accents, a literal single quote) are
// accepted at validation time — escaping happens at DDL time, not here.
// Value matching is exact and case-sensitive, so "Active" and "active" are
// two distinct valid values, not a duplicate.
func TestValidateEnumValues_FreeTextAccepted(t *testing.T) {
	values := []string{"Em andamento", "Não iniciado", "O'Brien", "active", "Active"}
	if err := ValidateEnumValues(values); err != nil {
		t.Fatalf("expected free-text values to be accepted, got: %v", err)
	}
}

// CENUM-04: ValidateTables applies ValidateEnumValues to every enum column,
// and leaves non-enum columns untouched by the enum rules.
func TestValidateTables_EnumColumnWiring(t *testing.T) {
	valid := defaultTestTable(ColumnConfig{Type: "enum", AllowedValues: []string{"pending", "active"}})
	if err := ValidateTables(valid); err != nil {
		t.Fatalf("expected valid enum column to be accepted, got: %v", err)
	}

	missing := defaultTestTable(ColumnConfig{Type: "enum"})
	err := ValidateTables(missing)
	if err == nil {
		t.Fatal("expected error for enum column without allowed_values, got nil")
	}
	if !strings.Contains(err.Error(), "column[0] (col)") {
		t.Errorf("error should identify the offending column, got: %v", err)
	}

	// A text column carrying allowed_values is not subject to the enum rules.
	notEnum := defaultTestTable(ColumnConfig{Type: "text", AllowedValues: []string{"", ""}})
	if err := ValidateTables(notEnum); err != nil {
		t.Errorf("expected non-enum column to ignore allowed_values, got: %v", err)
	}
}

// CENUM-05: a default that is not a member of allowed_values is rejected; a
// default that is a member is accepted.
func TestValidateTables_EnumDefaultMustBeMember(t *testing.T) {
	member := defaultTestTable(ColumnConfig{
		Type:          "enum",
		AllowedValues: []string{"pending", "active"},
		Default:       "pending",
	})
	if err := ValidateTables(member); err != nil {
		t.Fatalf("expected default that is a member to be accepted, got: %v", err)
	}

	notMember := defaultTestTable(ColumnConfig{
		Type:          "enum",
		AllowedValues: []string{"pending", "active"},
		Default:       "closed",
	})
	err := ValidateTables(notMember)
	if err == nil {
		t.Fatal("expected error for default outside allowed_values, got nil")
	}
	if !strings.Contains(err.Error(), `"closed"`) {
		t.Errorf("error should name the offending default, got: %v", err)
	}
}

func defaultTestTable(col ColumnConfig) []TableConfig {
	col.Name = "col"
	return []TableConfig{{Name: "t", Columns: []ColumnConfig{col}}}
}

func TestValidateTables_DefaultLiteralValid(t *testing.T) {
	cases := []ColumnConfig{
		{Type: "text", Default: "anything goes"},
		{Type: "integer", Default: "42"},
		{Type: "integer", Default: "-7"},
		{Type: "bigint", Default: "9223372036854775807"},
		{Type: "numeric", Default: "3.14"},
		{Type: "boolean", Default: "true"},
		{Type: "boolean", Default: "FALSE"},
		{Type: "uuid", Default: "550e8400-e29b-41d4-a716-446655440000"},
		{Type: "timestamptz", Default: "2026-01-01T00:00:00Z"},
		{Type: "jsonb", Default: `{"a":1}`},
		{Type: "jsonb", Default: `[]`},
	}
	for _, c := range cases {
		if err := ValidateTables(defaultTestTable(c)); err != nil {
			t.Errorf("type=%s default=%q: expected no error, got: %v", c.Type, c.Default, err)
		}
	}
}

func TestValidateTables_DefaultLiteralInvalid(t *testing.T) {
	cases := []ColumnConfig{
		{Type: "integer", Default: "not-a-number"},
		{Type: "integer", Default: "3.14"},
		{Type: "bigint", Default: "abc"},
		{Type: "numeric", Default: "abc"},
		{Type: "boolean", Default: "yes"},
		{Type: "uuid", Default: "not-a-uuid"},
		{Type: "timestamptz", Default: "not-a-date"},
		{Type: "jsonb", Default: "{not valid json"},
	}
	for _, c := range cases {
		if err := ValidateTables(defaultTestTable(c)); err == nil {
			t.Errorf("type=%s default=%q: expected error, got nil", c.Type, c.Default)
		}
	}
}

func TestValidateTables_DefaultExpressionAllowed(t *testing.T) {
	cases := []ColumnConfig{
		{Type: "uuid", Default: "gen_random_uuid()", DefaultIsExpression: true},
		{Type: "timestamptz", Default: "now()", DefaultIsExpression: true},
	}
	for _, c := range cases {
		if err := ValidateTables(defaultTestTable(c)); err != nil {
			t.Errorf("type=%s expression=%q: expected no error, got: %v", c.Type, c.Default, err)
		}
	}
}

func TestValidateTables_DefaultExpressionRejectsUnlistedValue(t *testing.T) {
	cases := []ColumnConfig{
		// Not on the allowlist for this type, despite being marked as an expression.
		{Type: "uuid", Default: "now()", DefaultIsExpression: true},
		{Type: "text", Default: "now()", DefaultIsExpression: true},
		// Classic injection attempt: must never be embedded unquoted.
		{Type: "text", Default: "'; DROP TABLE users; --", DefaultIsExpression: true},
	}
	for _, c := range cases {
		if err := ValidateTables(defaultTestTable(c)); err == nil {
			t.Errorf("type=%s expression=%q: expected error, got nil", c.Type, c.Default)
		}
	}
}

func TestValidateTables_DefaultEmptyAlwaysValid(t *testing.T) {
	if err := ValidateTables(defaultTestTable(ColumnConfig{Type: "integer", Default: ""})); err != nil {
		t.Fatalf("expected no error for empty default, got: %v", err)
	}
}
