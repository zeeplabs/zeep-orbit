package config

// ValidRLS reports whether rls is one of the recognized RLS mode values:
// "" (public), "owner", "enabled", or "policy". Any other string (a typo
// like "disabled" or "polcy") is invalid and must be rejected by callers
// before it can silently fall through to the public/no-filter branch.
func ValidRLS(rls string) bool {
	switch rls {
	case "", "owner", "enabled", "policy":
		return true
	default:
		return false
	}
}

// HasOwnerColumn reports whether tables with this RLS mode need an owner_id
// column: created in the DDL and populated on every INSERT. True for
// "owner", "enabled", and "policy" — false for "" (no RLS at all).
func HasOwnerColumn(rls string) bool {
	switch rls {
	case "owner", "enabled", "policy":
		return true
	default:
		return false
	}
}

// AutoScopesByOwner reports whether this RLS mode applies the automatic
// `owner_id = $sub` filter to list/get/update/delete operations. True only
// for "owner" and "enabled" — "policy" has the owner_id column (see
// HasOwnerColumn) but leaves all visibility/write permission to native
// Postgres table policies, with no filter injected by the application.
func AutoScopesByOwner(rls string) bool {
	switch rls {
	case "owner", "enabled":
		return true
	default:
		return false
	}
}
