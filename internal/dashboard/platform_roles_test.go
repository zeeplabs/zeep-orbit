package dashboard

import "testing"

func TestHasPlatformPermission(t *testing.T) {
	roles := []string{"superadmin", "admin", "auditor", "member"}
	actions := []PlatformAction{
		ActionManageTemplates,
		ActionManageBranding,
		ActionManageUsers,
		ActionManageIntegrations,
		ActionManageInfra,
		ActionViewAudit,
		ActionManageOwnApps,
	}
	// Canonical matrix from the spec (Section B). If you change this,
	// you almost certainly want to change platformPerms in lockstep.
	want := map[PlatformAction]map[string]bool{
		ActionManageTemplates:    {"superadmin": true, "admin": true, "auditor": false, "member": false},
		ActionManageBranding:     {"superadmin": true, "admin": true, "auditor": false, "member": false},
		ActionManageUsers:        {"superadmin": true, "admin": true, "auditor": false, "member": false},
		ActionManageIntegrations: {"superadmin": true, "admin": false, "auditor": false, "member": false},
		ActionManageInfra:        {"superadmin": true, "admin": false, "auditor": false, "member": false},
		ActionViewAudit:          {"superadmin": true, "admin": false, "auditor": true, "member": false},
		ActionManageOwnApps:      {"superadmin": true, "admin": true, "auditor": false, "member": true},
	}

	for _, action := range actions {
		for _, role := range roles {
			got := HasPlatformPermission(role, action)
			w := want[action][role]
			if got != w {
				t.Errorf("HasPlatformPermission(%q, %q) = %v, want %v", role, action, got, w)
			}
		}
	}

	// Defensive defaults: unknown role or unknown action must deny,
	// not panic and not silently allow.
	defensive := []struct {
		role   string
		action PlatformAction
	}{
		{"godmode", ActionManageUsers},
		{"superadmin", PlatformAction("explode")},
		{"", ActionManageUsers},
		{"superadmin", PlatformAction("")},
		{"admin", PlatformAction("__nonexistent__")},
	}
	for _, c := range defensive {
		if HasPlatformPermission(c.role, c.action) {
			t.Errorf("HasPlatformPermission(%q, %q) = true, want false (defensive default)", c.role, c.action)
		}
	}
}

func TestCanReadAnyApp(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{"superadmin", true},
		{"admin", true},
		{"auditor", true},
		{"member", false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		if got := CanReadAnyApp(tt.role); got != tt.want {
			t.Errorf("CanReadAnyApp(%q) = %v, want %v", tt.role, got, tt.want)
		}
	}
}

func TestCanCreateUserWithRole(t *testing.T) {
	tests := []struct {
		actor  string
		target string
		want   bool
		why    string
	}{
		// Superadmin can create any role.
		{"superadmin", "superadmin", true, "superadmin can create superadmin"},
		{"superadmin", "admin", true, ""},
		{"superadmin", "auditor", true, ""},
		{"superadmin", "member", true, ""},

		// Admin can create the 3 non-superadmin roles; this is the key gate.
		{"admin", "superadmin", false, "the gate: admin cannot create superadmin"},
		{"admin", "admin", true, ""},
		{"admin", "auditor", true, ""},
		{"admin", "member", true, ""},

		// Auditor and member would normally be blocked earlier by the
		// ActionManageUsers gate in CreateUser; the role-creation gate
		// itself still applies for completeness.
		{"auditor", "superadmin", false, "auditor cannot create superadmin (action gate blocks earlier anyway)"},
		{"auditor", "admin", true, "function only blocks superadmin-target"},
		{"auditor", "auditor", true, ""},
		{"auditor", "member", true, ""},
		{"member", "superadmin", false, "action gate blocks member earlier; this gate also blocks superadmin target"},
		{"member", "member", true, "function does not block (action gate blocks member earlier)"},
		{"member", "admin", true, ""},

		// Defensive: empty actor + superadmin target is blocked; empty
		// target is not superadmin so the gate doesn't fire.
		{"", "superadmin", false, "empty actor cannot create superadmin"},
		{"admin", "", true, "empty target is not superadmin, gate doesn't fire"},
		{"", "", true, "both empty, target is not superadmin, gate doesn't fire"},
	}
	for _, tt := range tests {
		got := CanCreateUserWithRole(tt.actor, tt.target)
		if got != tt.want {
			msg := tt.why
			if msg == "" {
				msg = "(see test case)"
			}
			t.Errorf("CanCreateUserWithRole(%q, %q) = %v, want %v — %s", tt.actor, tt.target, got, tt.want, msg)
		}
	}
}
