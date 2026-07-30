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
