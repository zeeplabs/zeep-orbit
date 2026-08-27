# v1.7.0 — Enum Columns, Advanced Policy Clauses & App-Update Provisioning Fix

A pre-release audit of everything shipped since v1.6.0 found and fixed 1 high-severity provisioning gap. No breaking changes; no migration steps required.

## Added

- **`enum` column type**, backed by a Postgres `CHECK` constraint instead of client-side-only validation. Declarable at column creation with a fixed list of allowed values; an existing enum column's values can be widened or narrowed afterward through a dedicated action (Dashboard, REST, or the `orbit_update_column_enum_values` MCP tool) — narrowing is rejected if any existing row still holds a value being removed. Also available from AI build/edit chat, which can now propose an `enum` column for a status-like field.
- **`orbit_create_policy_advanced` MCP tool**: creates a row policy from an explicit structured clause set (chained AND/OR conditions), for policy shapes outside the 6 fixed templates `orbit_create_policy_from_template` offers.

## Fixed

- **Enabling email/password auth or setting a storage bucket on an existing app could silently skip provisioning the tables that feature needs** (`_auth_users`/`_auth_sessions`/`_files`). Login/signup or file upload would then fail with a Postgres relation-does-not-exist error the first time they were used — a side effect of a v1.7.0-cycle fix that removed unrelated full-schema reconciliation from the app-save path. Both save paths (Dashboard/REST and MCP/AI chat) now provision exactly the tables each toggle needs, scoped to that app's schema only.
- Saving any Login/Storage/API-tab field on an app could fail with an error naming a completely unrelated table — the save path no longer reconciles table schema it never touches.
- MCP tools creating a row policy collapsed every structural validation failure (unknown column, disallowed operator, bad claim name) into a generic "internal error" — the specific validation message now surfaces instead.

## Upgrade notes

No manual steps.
