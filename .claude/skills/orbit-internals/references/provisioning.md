# Schema-per-app provisioning, RLS, and config

## Schema name derivation

`schemaNameForDB(appName string) string` — `internal/dashboard/handler.go:2446` — is `strings.ReplaceAll(appName, "-", "_")`. Nothing else: no prefix, no lowercasing, no extra sanitization at that call site. `internal/provisioner/provisioner.go:36` (`Apply`) reimplements the same transform inline instead of importing the `dashboard` package's private function — a known duplication, not a bug to silently "fix" by cross-importing without checking the package dependency direction first (`provisioner` is lower-level than `dashboard`; `dashboard` already depends on `provisioner`, not the reverse).

Never hardcode `"app_" + name` or any other pattern — this exact bug shipped to production once. `internal/dashboard/apps_store.go:451` has an explicit comment against it.

## `internal/provisioner` responsibilities

- `Provisioner` (`provisioner.go:13`) wraps `*db.Pool`; `New(pool)` (`:18`) constructs it.
- `Apply(ctx, cfg)` (`:32`) — idempotent, reconciles **every** app/table in the given config: creates schema, auth/storage system tables, topologically sorts tables by FK dependency (`topoSortTables`, `topsort.go`) before creating them, adds missing columns/indexes. This is the "reconcile everything" path — it's why `UpdateApp`'s REST handler no longer calls it for plain Login/Storage/API-tab saves (D-228: an untouched table's drift used to fail unrelated saves).
- `EnsureAuthTables(schemaName)` (`:105`) — provisions only `_auth_users`/`_auth_sessions` for one app, no full reconciliation. Called when an app's `auth_email_enabled` toggle flips on after creation.
- `EnsureStorageTables(schemaName)` (`:119`) — same, for `_files`.
- `pgType(t string)` (`table.go:26`) — maps a config-level column type (`text`, `integer`, `bigint`, `numeric`/`decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`, `enum`) to a Postgres DDL type. `enum` is deliberately its own `case` (`table.go:44-48`): it becomes `TEXT` plus a `CHECK` constraint, never a native Postgres `ENUM` type — falling through to a default case here was the historical bug that left `numeric` columns physically `TEXT` in production (D-228 root cause).
- Other notable functions: `createSchema`/`createTable`/`addMissingColumns`/`applyColumnChanges` (`schema.go`, `table.go`); `EnsureRowLevelSecurity`/`RelaxOwnerColumn` (`table.go:200,215`); `BuildPolicySQL` (`policy.go:103`); `AddColumnForeignKey`/`DropColumnForeignKey`/`ReplaceColumnEnumValues` (`table.go`); `BackfillEnduserGrants`/`grantEnduserSchemaAccess` (`schema.go:56,79`).

## RLS modes

Enum + validation: `internal/config/rls.go:7-14`, `ValidRLS(rls string)`:

| Value | Meaning |
|---|---|
| `""` (empty) | Public — no RLS, no automatic filter. |
| `"owner"` / `"enabled"` | Table has an `owner_id` column; the application layer auto-injects `owner_id = $sub` on every query (`AutoScopesByOwner`, `rls.go:33-39`). Not native Postgres RLS. |
| `"policy"` | Table has `owner_id` (`HasOwnerColumn`, `rls.go:19-26`) but **no** automatic filter — visibility is entirely delegated to native Postgres row policies you (or the caller) must create. |

Any other value (including the legacy `"disabled"`) is invalid — `ValidRLS` rejects it (`rls.go:5`). A batch of 8 tables in production `internal-portal-rh` had `rls="disabled"` predating this validation rule; the fix normalized them to `""` (same no-access-filter behavior, not a security fix — see AGENTS.md / vault D-228 history if you need that context, not this file).

Attaching policies: `BuildPolicySQL(schema, table, def, columns)` (`policy.go:103`) generates `CREATE POLICY ... TO zeep_app_enduser` — policies only ever target the end-user role, never the platform/business roles (`policy_test.go:547-548`). `EnsureRowLevelSecurity` (`table.go:200`) turns on native RLS on the table.

**RLS is a fail-closed, one-way ratchet** (`apps_store.go:640-648`): switching a table's `rls` to `"policy"` always calls `EnsureRowLevelSecurity` unconditionally (idempotent to call twice) — and there is no code path that ever disables RLS once it's on for a table. Don't add one without understanding why this invariant exists first.

## Config surface (`internal/config`)

`types.go`: `Config{Platform, Apps}` (`:3`); `AppConfig{Name, Auth, Tables, Storage, RateLimit}` (`:12`); `TableConfig{Name, RLS string, Columns, Indexes}` (`:42-47`); `ColumnConfig{Name, Type, Required, Default, DefaultIsExpression, Unique, RenameFrom, References *ReferenceConfig, AllowedValues []string}` (`:49-67`); `ReferenceConfig{Table, Column, OnDelete}` (`:72-77` — cross-app FKs are explicitly out of scope; schema-per-app isolation is an architectural boundary, not just a naming convention); `IndexConfig{Name, Columns, Unique}` (`:89-93`).

Validation (`validate.go`): column type switch (`integer`/`bigint`/`numeric` alias `decimal`/`boolean`/`uuid`/`timestamptz`/`jsonb`/`enum`/`text`, roughly `:103-139`); `ValidateEnumValues` (`:154`); `validateReference` requires `column type == "uuid"` when referencing `_auth_users.id` (`:189-200`); `validateDefault` (`:88`) checks a default expression against a type-scoped allowlist (`defaultExpressions`, `:35-36` — e.g. `uuid → gen_random_uuid()`, `timestamptz → now()`); `detectReferenceCycle` (`:272`).
