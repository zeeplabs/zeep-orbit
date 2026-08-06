package dashboard

// PlatformAction names a class of platform-level operation gated by role.
// Used with HasPlatformPermission as the single source of truth for "may
// role X do action Y on the dashboard platform itself" — distinct from
// the per-app axis resolved by rbac-per-app.ResolveAppRole.
type PlatformAction string

const (
	ActionManageTemplates    PlatformAction = "templates"
	ActionManageBranding     PlatformAction = "branding"
	ActionManageUsers        PlatformAction = "users"
	ActionManageIntegrations PlatformAction = "integrations"
	ActionManageInfra        PlatformAction = "infra"
	ActionViewAudit          PlatformAction = "audit"
	ActionManageOwnApps      PlatformAction = "own_apps"
)

// platformPerms is the canonical role × action matrix. Any role not listed
// for a given action is denied. Unknown actions are not in the map and
// therefore also denied (HasPlatformPermission returns the zero value,
// which is false). Keep this table in sync with the spec's Section B.
var platformPerms = map[PlatformAction]map[string]bool{
	ActionManageTemplates: {
		"superadmin": true,
		"admin":      true,
	},
	ActionManageBranding: {
		"superadmin": true,
		"admin":      true,
	},
	ActionManageUsers: {
		"superadmin": true,
		"admin":      true,
	},
	ActionManageIntegrations: {
		"superadmin": true,
	},
	ActionManageInfra: {
		"superadmin": true,
	},
	ActionViewAudit: {
		"superadmin": true,
		"auditor":    true,
	},
	ActionManageOwnApps: {
		"superadmin": true,
		"admin":      true,
		"member":     true,
	},
}

// HasPlatformPermission reports whether the given role may perform action.
// Unknown roles and unknown actions return false (defensive default —
// deny by default, not allow).
func HasPlatformPermission(role string, action PlatformAction) bool {
	perms, ok := platformPerms[action]
	if !ok {
		return false
	}
	return perms[role]
}

// CanReadAnyApp reports whether the role can read any app's
// data/schema/logs without being a member. Used by the rbac-per-app
// ResolveAppRole extension (T-06 of dashboard-global-roles) — defined
// here so the spec's cross-spec integration has a single home.
func CanReadAnyApp(role string) bool {
	switch role {
	case "superadmin", "admin", "auditor":
		return true
	default:
		return false
	}
}

// CanCreateUserWithRole reports whether an actor with actorRole may
// create a new user assigned targetRole. The only rule enforced here
// is: only a superadmin can create another superadmin. All other
// validation (email format, password length, role name being one of
// the 4 known values) lives in CreateUser.
func CanCreateUserWithRole(actorRole, targetRole string) bool {
	if targetRole == "superadmin" && actorRole != "superadmin" {
		return false
	}
	return true
}
