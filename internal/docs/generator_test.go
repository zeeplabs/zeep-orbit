package docs

import (
	"testing"

	"github.com/zeeplabs/zeep-orbit/internal/registry"
)

// TestBuildResponseSchema_PolicyRLSExposesOwnerID covers T8's "Done when":
// a table with rls "policy" must expose owner_id in the OpenAPI response
// schema (spec.md Edge Cases: "o gerador de documentação OpenAPI ...
// processa uma tabela rls: 'policy' ... o schema de resposta SHALL incluir
// a propriedade owner_id (tipo uuid, somente leitura)").
func TestBuildResponseSchema_PolicyRLSExposesOwnerID(t *testing.T) {
	table := &registry.Table{Name: "posts", RLS: "policy"}

	schema := buildResponseSchema(table)

	prop, ok := schema.Properties["owner_id"]
	if !ok {
		t.Fatal("expected owner_id property to be present for rls: policy")
	}
	if prop.Type != "string" || prop.Format != "uuid" || !prop.ReadOnly {
		t.Fatalf("expected owner_id to be {type: string, format: uuid, readOnly: true}, got %+v", prop)
	}

	found := false
	for _, r := range schema.Required {
		if r == "owner_id" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected owner_id to be in required list, got %v", schema.Required)
	}
}

// TestBuildResponseSchema_OwnerRLSExposesOwnerID is a regression test: the
// predicate swap (table.RLS == "owner" -> config.HasOwnerColumn) must keep
// producing the exact same schema for rls: "owner" tables.
func TestBuildResponseSchema_OwnerRLSExposesOwnerID(t *testing.T) {
	table := &registry.Table{Name: "posts", RLS: "owner"}

	schema := buildResponseSchema(table)

	if _, ok := schema.Properties["owner_id"]; !ok {
		t.Fatal("expected owner_id property to be present for rls: owner")
	}
}

// TestBuildResponseSchema_EnabledRLSExposesOwnerID covers the pre-existing
// gap fixed for free by the same predicate swap (design.md: "enabled"
// tables also have an owner_id column and were previously not exposing it).
func TestBuildResponseSchema_EnabledRLSExposesOwnerID(t *testing.T) {
	table := &registry.Table{Name: "posts", RLS: "enabled"}

	schema := buildResponseSchema(table)

	if _, ok := schema.Properties["owner_id"]; !ok {
		t.Fatal("expected owner_id property to be present for rls: enabled")
	}
}

// TestBuildResponseSchema_NoRLSDoesNotExposeOwnerID covers the negative
// case: a table with no RLS at all has no owner_id column and must not
// expose it in the schema.
func TestBuildResponseSchema_NoRLSDoesNotExposeOwnerID(t *testing.T) {
	table := &registry.Table{Name: "posts", RLS: ""}

	schema := buildResponseSchema(table)

	if _, ok := schema.Properties["owner_id"]; ok {
		t.Fatal("expected owner_id property to be absent for rls: \"\"")
	}
}

// TestBuildResponseSchema_EnumColumnExposesAllowedValues covers a gap found
// by the v1.6.0..HEAD release-readiness audit: an enum column's CHECK
// constraint exists in Postgres, the Dashboard UI, and the MCP tools, but
// the generated OpenAPI spec dropped it entirely — openAPIType had no
// "enum" case, so it fell to the default ("string", no enum list),
// indistinguishable from a plain text column. Both the response and input
// schema must carry the allowed-values list.
func TestBuildResponseSchema_EnumColumnExposesAllowedValues(t *testing.T) {
	table := &registry.Table{Name: "assets", Columns: []registry.Column{
		{Name: "status", Type: "enum", AllowedValues: []string{"pending", "active", "closed"}},
	}}

	respSchema := buildResponseSchema(table)
	prop, ok := respSchema.Properties["status"]
	if !ok {
		t.Fatal("expected status property to be present")
	}
	if prop.Type != "string" {
		t.Fatalf("expected enum column to be OpenAPI type string, got %q", prop.Type)
	}
	if len(prop.Enum) != 3 || prop.Enum[0] != "pending" || prop.Enum[1] != "active" || prop.Enum[2] != "closed" {
		t.Fatalf("expected enum list [pending active closed], got %v", prop.Enum)
	}

	inputSchema := buildInputSchema(table)
	inputProp, ok := inputSchema.Properties["status"]
	if !ok {
		t.Fatal("expected status property to be present in the input schema")
	}
	if len(inputProp.Enum) != 3 {
		t.Fatalf("expected input schema to also carry the enum list, got %v", inputProp.Enum)
	}
}

// TestBuildResponseSchema_NonEnumColumnHasNoEnumList is the negative
// control: a plain text column must not get an empty-but-present "enum"
// key in the marshaled JSON (schemaOrRef.Enum's omitempty depends on it
// being nil, not an empty non-nil slice, for a text column).
func TestBuildResponseSchema_NonEnumColumnHasNoEnumList(t *testing.T) {
	table := &registry.Table{Name: "posts", Columns: []registry.Column{
		{Name: "title", Type: "text"},
	}}

	schema := buildResponseSchema(table)
	if schema.Properties["title"].Enum != nil {
		t.Fatalf("expected no Enum list for a text column, got %v", schema.Properties["title"].Enum)
	}
}
