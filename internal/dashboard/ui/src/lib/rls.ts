// Mirrors internal/config/rls.go's HasOwnerColumn: true for "owner",
// "enabled", and "policy" — false for "" (no RLS at all). Single source of
// truth on the frontend for "does this rls mode have an owner_id column",
// so a future new RLS mode only needs updating here instead of at every
// inline call site (AGENTS.md: same class of bug as the schema-name rule).
export function hasOwnerColumn(rls: string): boolean {
  switch (rls) {
    case "owner":
    case "enabled":
    case "policy":
      return true;
    default:
      return false;
  }
}
