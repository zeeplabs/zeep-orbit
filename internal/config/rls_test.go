package config

import "testing"

// TestValidRLS covers spec AC P1-7 (RLSP-09): only "", "owner", "enabled",
// "policy" are valid; any other string (typo or unrecognized value) is not.
func TestValidRLS(t *testing.T) {
	cases := []struct {
		rls  string
		want bool
	}{
		{"", true},
		{"owner", true},
		{"enabled", true},
		{"policy", true},
		{"disabled", false},
		{"polcy", false},
		{"enable", false},
		{"OWNER", false},
	}
	for _, c := range cases {
		if got := ValidRLS(c.rls); got != c.want {
			t.Errorf("ValidRLS(%q) = %v, want %v", c.rls, got, c.want)
		}
	}
}

// TestHasOwnerColumn covers design.md's predicate table: true for
// "owner"/"enabled"/"policy" (all three need the owner_id column), false
// for "" and any unrecognized value.
func TestHasOwnerColumn(t *testing.T) {
	cases := []struct {
		rls  string
		want bool
	}{
		{"", false},
		{"owner", true},
		{"enabled", true},
		{"policy", true},
		{"disabled", false},
	}
	for _, c := range cases {
		if got := HasOwnerColumn(c.rls); got != c.want {
			t.Errorf("HasOwnerColumn(%q) = %v, want %v", c.rls, got, c.want)
		}
	}
}

// TestAutoScopesByOwner covers spec AC P1-4/P1-5: only "owner"/"enabled"
// apply the automatic owner_id filter; "policy" (has owner_id column but no
// auto-filter — visibility left entirely to native policies) and "" must
// both be false.
func TestAutoScopesByOwner(t *testing.T) {
	cases := []struct {
		rls  string
		want bool
	}{
		{"", false},
		{"owner", true},
		{"enabled", true},
		{"policy", false},
		{"disabled", false},
	}
	for _, c := range cases {
		if got := AutoScopesByOwner(c.rls); got != c.want {
			t.Errorf("AutoScopesByOwner(%q) = %v, want %v", c.rls, got, c.want)
		}
	}
}
