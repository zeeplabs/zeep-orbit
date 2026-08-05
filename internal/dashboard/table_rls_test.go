package dashboard

import "testing"

func TestResolveTableRLS(t *testing.T) {
	cases := []struct {
		name             string
		requested        string
		requireDefault   bool
		authEmailEnabled bool
		want             string
	}{
		{"omitted + require + auth → enabled", "", true, true, "enabled"},
		{"omitted + require + no auth → stays public", "", true, false, ""},
		{"omitted + no require → stays public", "", false, true, ""},
		{"explicit public always respected", "disabled", true, true, "disabled"},
		{"explicit restricted respected", "enabled", true, true, "enabled"},
		{"omitted + no require + no auth → public", "", false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveTableRLS(c.requested, c.requireDefault, c.authEmailEnabled)
			if got != c.want {
				t.Fatalf("resolveTableRLS(%q, %v, %v) = %q, want %q",
					c.requested, c.requireDefault, c.authEmailEnabled, got, c.want)
			}
		})
	}
}
