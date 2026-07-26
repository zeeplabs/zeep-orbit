package dashboard

import (
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple name", "My App", "my-app"},
		{"with underscores", "my_app", "my-app"},
		{"with accents", "Café App", "caf-app"},
		{"special characters", "Hello! World?", "hello-world"},
		{"multiple spaces", "  spaced   out  ", "spaced-out"},
		{"leading hyphen", "-leading", "leading"},
		{"trailing hyphen", "trailing-", "trailing"},
		{"multiple hyphens", "foo---bar", "foo-bar"},
		{"empty string", "", ""},
		{"all special", "!@#$%", ""},
		{"mixed case", "Meu App Legal", "meu-app-legal"},
		{"numbers allowed", "app123 v2", "app123-v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
