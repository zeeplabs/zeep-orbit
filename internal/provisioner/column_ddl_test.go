package provisioner

import (
	"strings"
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/config"
)

func TestColumnDDL_LiteralDefaultIsQuotedAndEscaped(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{Name: "note", Type: "text", Default: "it's here"})
	if !strings.Contains(ddl, `DEFAULT 'it''s here'`) {
		t.Errorf("ddl = %q, want escaped quoted literal default", ddl)
	}
}

func TestColumnDDL_ExpressionDefaultIsUnquoted(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{Name: "created_at", Type: "timestamptz", Default: "now()", DefaultIsExpression: true})
	if !strings.Contains(ddl, "DEFAULT now()") {
		t.Errorf("ddl = %q, want unquoted DEFAULT now()", ddl)
	}
	if strings.Contains(ddl, "'now()'") {
		t.Errorf("ddl = %q, expression default must never be quoted", ddl)
	}
}

func TestColumnDDL_NoDefaultOmitsClause(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{Name: "note", Type: "text"})
	if strings.Contains(ddl, "DEFAULT") {
		t.Errorf("ddl = %q, want no DEFAULT clause when Default is empty", ddl)
	}
}

// CENUM-01: an enum column is provisioned as TEXT with a CHECK constraint
// restricting it to exactly the declared values.
func TestColumnDDL_EnumEmitsCheckConstraint(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{
		Name:          "status",
		Type:          "enum",
		AllowedValues: []string{"pending"},
	})
	if want := `"status" TEXT`; !strings.HasPrefix(ddl, want) {
		t.Errorf("ddl = %q, want physical type %q", ddl, want)
	}
	if want := `CHECK ("status" IN ('pending'))`; !strings.Contains(ddl, want) {
		t.Errorf("ddl = %q, want %q", ddl, want)
	}
}

// CENUM-01: every declared value ends up in the IN list, in declaration
// order (order is preserved for display, per spec's assumptions table).
func TestColumnDDL_EnumEmitsAllValuesInOrder(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{
		Name:          "status",
		Type:          "enum",
		AllowedValues: []string{"pending", "active", "closed"},
	})
	if want := `CHECK ("status" IN ('pending', 'active', 'closed'))`; !strings.Contains(ddl, want) {
		t.Errorf("ddl = %q, want %q", ddl, want)
	}
}

// CENUM-06 / edge case: a value containing a single quote is escaped by
// doubling it, so the clause stays valid SQL and cannot break out of the
// literal. Free-text values (spaces, accents) pass through unchanged.
func TestColumnDDL_EnumEscapesSingleQuotes(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{
		Name:          "status",
		Type:          "enum",
		AllowedValues: []string{"O'Brien", "Em andamento", "'); DROP TABLE t; --"},
	})
	want := `CHECK ("status" IN ('O''Brien', 'Em andamento', '''); DROP TABLE t; --'))`
	if !strings.Contains(ddl, want) {
		t.Errorf("ddl = %q, want %q", ddl, want)
	}
}

// CENUM-01: the CHECK clause sits after NOT NULL / DEFAULT / UNIQUE and
// before REFERENCES, so the emitted column definition is valid SQL when
// every clause is present at once.
func TestColumnDDL_EnumClauseOrdering(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{
		Name:          "status",
		Type:          "enum",
		Required:      true,
		Default:       "pending",
		Unique:        true,
		AllowedValues: []string{"pending", "active"},
		References:    &config.ReferenceConfig{Table: "statuses", Column: "id"},
	})
	want := `"status" TEXT NOT NULL DEFAULT 'pending' UNIQUE CHECK ("status" IN ('pending', 'active')) REFERENCES "myapp"."statuses"("id") ON DELETE NO ACTION`
	if ddl != want {
		t.Errorf("ddl = %q, want %q", ddl, want)
	}
}

// A non-enum column never gets a CHECK clause, even if AllowedValues is set.
func TestColumnDDL_NonEnumOmitsCheckConstraint(t *testing.T) {
	ddl := columnDDL("myapp", config.ColumnConfig{
		Name:          "note",
		Type:          "text",
		AllowedValues: []string{"pending"},
	})
	if strings.Contains(ddl, "CHECK") {
		t.Errorf("ddl = %q, want no CHECK clause on a non-enum column", ddl)
	}
}
