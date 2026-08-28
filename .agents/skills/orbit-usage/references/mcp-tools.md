# Orbit MCP server

If you have MCP tool access to an Orbit instance (an agent connected via `zeep-orbit` MCP server), you can manage apps/tables/policies/webhooks directly — same validation, provisioning, and audit path as the Dashboard UI and the REST API. No shortcuts, no separate code path.

**Always call `tools/list` first** to get the exact, current tool names and input schemas — the list below is a map of what exists by category, not a substitute for the live schema. Tool names/params can gain fields between Orbit releases; don't hardcode a remembered schema.

## Categories (illustrative names, confirm via `tools/list`)

- **App lifecycle**: `orbit_list_apps`, `orbit_get_app`, `orbit_get_app_schema`, `orbit_create_app`, `orbit_update_app`
- **Tables & columns**: `orbit_create_table`, `orbit_add_table_column`, `orbit_add_table_index`, `orbit_add_column_foreign_key`, `orbit_remove_column_foreign_key`, `orbit_update_column_enum_values`
- **Row-level security**: `orbit_set_table_rls_mode`, `orbit_list_policy_templates`, `orbit_create_policy_from_template`, `orbit_create_policy_advanced`, `orbit_list_table_policies`
- **Members & access**: `orbit_list_app_members`, `orbit_list_my_pats`
- **Tokens**: `orbit_list_app_tokens`
- **Webhooks**: `orbit_list_webhooks`, `orbit_get_webhook`, `orbit_create_webhook`, `orbit_save_webhook_event_mapping`, `orbit_list_webhook_deliveries`
- **Inspection / read-only**: `orbit_list_app_auth_providers`, `orbit_get_logs_metrics`

## Rules that matter when driving Orbit via MCP

- **`enum` columns are create-time only for their allowed values.** To create a column with fixed values, set `type: "enum"` and `allowed_values` at creation. To change the set of allowed values on an *existing* enum column, use the dedicated tool (`orbit_update_column_enum_values`) — do not try to recreate the column or push a generic "update column" call, and never attempt to convert an existing column to/from `enum`.
- **Full-replace table update endpoints do not retype existing columns.** If a tool updates a table by sending its whole column list, it is add-only for new columns — it will not apply a type/FK/enum-values change to a column that already exists. Use the dedicated per-mutation tool (foreign key add/remove, enum values update) instead.
- **Partial-update tools merge on absent fields.** When you want to *clear* a field on an app/table config (e.g. remove a foreign key, empty out a list), you must send that field explicitly (empty value/array) — omitting the key is interpreted as "don't touch it."
- **RLS is off by default per table** until you set a mode and attach policies — a newly created table with no policy is either fully locked or fully open depending on its RLS mode; check `orbit_set_table_rls_mode` / `orbit_list_table_policies` state before assuming a table is secured.
- **Errors returned by tools are in English**, same as the REST API — don't expect them localized.
