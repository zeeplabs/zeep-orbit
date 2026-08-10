package webhookengine

import "testing"

func TestExtractPath_FlatKey(t *testing.T) {
	payload := map[string]any{"eventType": "user.created"}
	v, found := ExtractPath(payload, "eventType")
	if !found {
		t.Fatal("expected flat key to be found")
	}
	if v != "user.created" {
		t.Fatalf("expected 'user.created', got %v", v)
	}
}

func TestExtractPath_NestedKey(t *testing.T) {
	payload := map[string]any{
		"user": map[string]any{"id": "u-123", "name": "Ana"},
	}
	v, found := ExtractPath(payload, "user.id")
	if !found {
		t.Fatal("expected nested key to be found")
	}
	if v != "u-123" {
		t.Fatalf("expected 'u-123', got %v", v)
	}
}

func TestExtractPath_ArrayIndex(t *testing.T) {
	payload := map[string]any{
		"items": []any{
			map[string]any{"id": "item-0"},
			map[string]any{"id": "item-1"},
		},
	}
	v, found := ExtractPath(payload, "items.1.id")
	if !found {
		t.Fatal("expected array-index path to be found")
	}
	if v != "item-1" {
		t.Fatalf("expected 'item-1', got %v", v)
	}
}

func TestExtractPath_MissingPathReturnsFoundFalseNoError(t *testing.T) {
	payload := map[string]any{"eventType": "user.created"}

	if _, found := ExtractPath(payload, "does.not.exist"); found {
		t.Fatal("expected missing nested path to report found=false")
	}
	if _, found := ExtractPath(payload, "missingKey"); found {
		t.Fatal("expected missing flat key to report found=false")
	}
	// Array index out of range and non-numeric segment against an array both
	// resolve to found=false, never a panic or error return value.
	arrPayload := map[string]any{"items": []any{"a", "b"}}
	if _, found := ExtractPath(arrPayload, "items.5"); found {
		t.Fatal("expected out-of-range array index to report found=false")
	}
	if _, found := ExtractPath(arrPayload, "items.notanumber"); found {
		t.Fatal("expected non-numeric segment against an array to report found=false")
	}
}

func TestResolveFields_EmptyMappingsReturnsEmptyMap(t *testing.T) {
	payload := map[string]any{"eventType": "user.created"}
	result, err := ResolveFields(payload, nil)
	if err != nil {
		t.Fatalf("ResolveFields with no mappings should not error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected an empty result map, got %v", result)
	}
}

func TestResolveFields_MissingSourcePathReturnsNamedError(t *testing.T) {
	payload := map[string]any{"eventType": "user.created"}
	_, err := ResolveFields(payload, []FieldMapping{
		{SourcePath: "user.id", Column: "external_id"},
	})
	if err == nil {
		t.Fatal("expected an error when a mapping's source_path is missing from the payload")
	}
	if !contains(err.Error(), "user.id") {
		t.Fatalf("expected the error to name the missing source path 'user.id', got %q", err.Error())
	}
}

func TestResolveFields_MultiFieldHappyPath(t *testing.T) {
	payload := map[string]any{
		"user": map[string]any{
			"id":    "u-123",
			"name":  "Ana Souza",
			"email": "ana@example.com",
		},
		"eventType": "user.created",
	}
	result, err := ResolveFields(payload, []FieldMapping{
		{SourcePath: "user.id", Column: "external_id"},
		{SourcePath: "user.name", Column: "full_name"},
		{SourcePath: "user.email", Column: "email"},
	})
	if err != nil {
		t.Fatalf("ResolveFields: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 resolved fields, got %d", len(result))
	}
	if result["external_id"] != "u-123" || result["full_name"] != "Ana Souza" || result["email"] != "ana@example.com" {
		t.Fatalf("unexpected resolved values: %+v", result)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
