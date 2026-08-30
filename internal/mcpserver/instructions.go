package mcpserver

// serverInstructions is sent to every connecting MCP client during
// initialize (ServerOptions.Instructions) so an agent can drive this server
// correctly without a separately-installed skill (the orbit-usage skill in
// zeeplabs/zeep-orbit-agent-skills covers the same ground for humans/agents
// that install it explicitly; this is the zero-install baseline).
const serverInstructions = `This is the Zeep Orbit MCP server: it manages apps, tables, columns, RLS policies, webhooks, and tokens for a self-hosted Backend-as-a-Service. Tool calls run through the same provisioner/audit path as the Dashboard UI and REST API — no shortcuts.

Always call tools/list first for exact, current tool names and input schemas. Tool names below are illustrative categories, not a schema reference:
- App lifecycle: orbit_list_apps, orbit_get_app, orbit_get_app_schema, orbit_create_app, orbit_update_app
- Tables & columns: orbit_create_table, orbit_add_table_column, orbit_add_table_index, orbit_add_column_foreign_key, orbit_remove_column_foreign_key, orbit_update_column_enum_values
- Row-level security: orbit_set_table_rls_mode, orbit_list_policy_templates, orbit_create_policy_from_template, orbit_create_policy_advanced, orbit_list_table_policies
- Members & access: orbit_list_app_members, orbit_list_my_pats
- Tokens: orbit_list_app_tokens
- Webhooks: orbit_list_webhooks, orbit_get_webhook, orbit_create_webhook, orbit_save_webhook_event_mapping, orbit_list_webhook_deliveries
- Inspection: orbit_list_app_auth_providers, orbit_get_logs_metrics

Rules that matter:
- enum columns: allowed values are set only at column creation (type: "enum" + allowed_values). To change values on an existing enum column, use orbit_update_column_enum_values — never try to recreate the column or convert an existing column to/from enum.
- Full-replace table update calls are add-only for columns: they will not retype, add an FK, or change enum values on a column that already exists. Use the dedicated per-mutation tool instead.
- Partial-update tools merge on absent fields: to clear a field (e.g. remove an FK, empty a list) you must send that field explicitly with an empty value — omitting the key means "don't touch it."
- RLS is off by default per table until a mode is set and policies attached. Check orbit_set_table_rls_mode / orbit_list_table_policies before assuming a table is secured.
- Tool errors are in English, same as the REST API — do not expect them localized.

This server does not cover the REST API for an app's own end users, or client SDK usage for a frontend talking to an app — that is a separate consumer-facing surface (see the zeep-orbit-agent-skills repo's orbit-usage skill if you need that).`
