package policytemplates

import "testing"

// TestList_SameSixTemplateIDsInSameOrder covers T3's Done-when: List()
// returns the same 6 template ids, in the same order, as
// policyTemplates.ts's TEMPLATE_DEFINITIONS array.
func TestList_SameSixTemplateIDsInSameOrder(t *testing.T) {
	want := []string{
		TemplateIDOwnerOnly,
		TemplateIDOpenRead,
		TemplateIDReadOnly,
		TemplateIDValueMatch,
		TemplateIDOpenReadOwnerWrite,
		TemplateIDBlockedByDefault,
	}
	got := List()
	if len(got) != len(want) {
		t.Fatalf("expected %d templates, got %d", len(want), len(got))
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Fatalf("expected template %d to have id %q, got %q", i, w, got[i].ID)
		}
	}
}

// TestGeneratedPolicyName mirrors generatedPolicyName's tpl_<id>_<action>
// naming convention.
func TestGeneratedPolicyName(t *testing.T) {
	got := GeneratedPolicyName("owner_only", "select")
	want := "tpl_owner_only_select"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// TestBuildOwnerOnlyPolicies_MatchesTSOutput asserts the Go builder's output
// field-for-field against buildOwnerOnlyPolicies(["select","update"],
// ["member"])'s documented TS output (policyTemplates.ts): one PolicyDef
// per action, each scoped to owner_id = <jwt sub claim>, named
// tpl_owner_only_<action>.
func TestBuildOwnerOnlyPolicies_MatchesTSOutput(t *testing.T) {
	got := BuildOwnerOnlyPolicies([]string{"select", "update"}, []string{"member"})
	want := []PolicyDef{
		{
			Name:   "tpl_owner_only_select",
			Action: "select",
			Roles:  []string{"member"},
			Clauses: []PolicyClause{
				{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
			},
		},
		{
			Name:   "tpl_owner_only_update",
			Action: "update",
			Roles:  []string{"member"},
			Clauses: []PolicyClause{
				{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
			},
		},
	}
	assertPolicyDefsEqual(t, "BuildOwnerOnlyPolicies", got, want)
}

// TestBuildOpenReadPolicy_MatchesTSOutput asserts against
// buildOpenReadPolicy(["member"])'s documented TS output: select, open to
// the chosen roles, dummy owner_id IS NOT NULL clause (no real row filter).
func TestBuildOpenReadPolicy_MatchesTSOutput(t *testing.T) {
	got := BuildOpenReadPolicy([]string{"member"})
	want := PolicyDef{
		Name:    "tpl_open_read_select",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "owner_id", Operator: "IS NOT NULL"}},
	}
	assertPolicyDefEqual(t, "BuildOpenReadPolicy", got, want)
}

// TestBuildReadOnlyPolicy_MatchesTSOutput asserts against
// buildReadOnlyPolicy(["member"])'s documented TS output: identical clause
// shape to open_read, but named tpl_read_only_select.
func TestBuildReadOnlyPolicy_MatchesTSOutput(t *testing.T) {
	got := BuildReadOnlyPolicy([]string{"member"})
	want := PolicyDef{
		Name:    "tpl_read_only_select",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "owner_id", Operator: "IS NOT NULL"}},
	}
	assertPolicyDefEqual(t, "BuildReadOnlyPolicy", got, want)
}

// TestBuildValueMatchPolicy_MatchesTSOutput asserts against
// buildValueMatchPolicy("status", "active", ["member"])'s documented TS
// output: select, filtered to a real column equal to a literal value.
func TestBuildValueMatchPolicy_MatchesTSOutput(t *testing.T) {
	got := BuildValueMatchPolicy("status", "active", []string{"member"})
	want := PolicyDef{
		Name:    "tpl_value_match_select",
		Action:  "select",
		Roles:   []string{"member"},
		Clauses: []PolicyClause{{Column: "status", Operator: "=", ValueSource: "literal", Value: "active"}},
	}
	assertPolicyDefEqual(t, "BuildValueMatchPolicy", got, want)
}

// TestBuildOpenReadOwnerWritePolicies_MatchesTSOutput asserts against
// buildOpenReadOwnerWritePolicies(["member"])'s documented TS output: open
// read for readRoles, write (update/delete) restricted to the row's owner,
// all 3 policies renamed onto the composite template's id.
func TestBuildOpenReadOwnerWritePolicies_MatchesTSOutput(t *testing.T) {
	got := BuildOpenReadOwnerWritePolicies([]string{"member"})
	want := []PolicyDef{
		{
			Name:    "tpl_open_read_owner_write_select",
			Action:  "select",
			Roles:   []string{"member"},
			Clauses: []PolicyClause{{Column: "owner_id", Operator: "IS NOT NULL"}},
		},
		{
			Name:   "tpl_open_read_owner_write_update",
			Action: "update",
			Roles:  []string{"member"},
			Clauses: []PolicyClause{
				{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
			},
		},
		{
			Name:   "tpl_open_read_owner_write_delete",
			Action: "delete",
			Roles:  []string{"member"},
			Clauses: []PolicyClause{
				{Column: "owner_id", Operator: "=", ValueSource: "claim", Value: "sub"},
			},
		},
	}
	assertPolicyDefsEqual(t, "BuildOpenReadOwnerWritePolicies", got, want)
}

func assertPolicyDefEqual(t *testing.T, label string, got, want PolicyDef) {
	t.Helper()
	if got.Name != want.Name {
		t.Errorf("%s: Name = %q, want %q", label, got.Name, want.Name)
	}
	if got.Action != want.Action {
		t.Errorf("%s: Action = %q, want %q", label, got.Action, want.Action)
	}
	if !stringSlicesEqual(got.Roles, want.Roles) {
		t.Errorf("%s: Roles = %v, want %v", label, got.Roles, want.Roles)
	}
	if len(got.Clauses) != len(want.Clauses) {
		t.Fatalf("%s: got %d clauses, want %d", label, len(got.Clauses), len(want.Clauses))
	}
	for i, wc := range want.Clauses {
		gc := got.Clauses[i]
		if gc != wc {
			t.Errorf("%s: Clauses[%d] = %+v, want %+v", label, i, gc, wc)
		}
	}
}

func assertPolicyDefsEqual(t *testing.T, label string, got, want []PolicyDef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d policies, want %d", label, len(got), len(want))
	}
	for i := range want {
		assertPolicyDefEqual(t, label, got[i], want[i])
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
