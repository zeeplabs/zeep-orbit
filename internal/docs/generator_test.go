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
