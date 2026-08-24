## Highlights

- **14 new MCP tools** extend the MCP server shipped in v1.4.0 with two tiers of new capability:
  - **10 read-only tools** — `orbit_get_app`, `orbit_list_table_policies`, `orbit_list_app_members`, `orbit_list_app_tokens`, `orbit_list_app_auth_providers`, `orbit_list_my_pats`, `orbit_list_webhooks`, `orbit_get_webhook`, `orbit_list_webhook_deliveries`, `orbit_get_logs_metrics` — let an AI agent inspect an app's configuration and operational surface directly. Every tool authorizes through the exact same permission path its REST equivalent already uses, and never returns a secret its REST equivalent doesn't already redact.
  - **4 safe mutation tools** — `orbit_add_table_column` and `orbit_add_table_index` add exactly one new column or index, merged server-side against the table's current stored definition, so the request can never omit or corrupt an existing column or index — unlike a naive resend to the full-replace `PUT .../tables/{tableId}` endpoint. `orbit_create_webhook` and `orbit_save_webhook_event_mapping` wrap the existing, already-additive webhook endpoints. Table-schema tools require `role.CanWrite()`; webhook tools require the stricter `role.CanManage()`, matching each operation's existing REST authorization tier exactly.

## Security

- **Fixed a live incident**: the dashboard API and `orbit_list_apps` were returning an app's real credentials in plaintext — AWS storage keys, OAuth `client_secret`, and the app's own JWT secret — to any app member, not just an admin. Responses now carry only display-safe fields plus a `client_secret_set` boolean. **If you have apps with a configured storage bucket or OAuth login provider, treat those credentials as exposed and rotate them** — this release stops further disclosure, it doesn't invalidate what may already have been read.
- Fixed a regression this same investigation surfaced: saving an app's Login settings without retyping its Google OAuth `client_secret` silently deleted that secret and broke the app's Google login (the form can't re-populate a secret it's never given back). Auth provider updates now merge onto the existing stored config field-by-field instead of overwriting it raw.
- Fixed: turning an app's Google login toggle off didn't actually disable it server-side.
- Credential redaction now runs off an allow-list of known-safe display fields (was a deny-list of one known secret field name), closing the class of bug where a credential under an unexpected key could slip through unmasked.

## Fixed

- `GET /docs/{app}/openapi.json` could serve a stale OpenAPI spec missing a recently added table due to a missing cache header — responses are now marked `Cache-Control: no-store`.

## Upgrade notes

- No manual migration steps.
- Rotate any AWS storage or OAuth `client_secret` credentials configured on apps that existed before this release — see Security above.
