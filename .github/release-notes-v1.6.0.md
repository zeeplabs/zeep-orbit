# v1.6.0 — AI Chat Hardening

A pre-release security review of everything shipped since v1.5.0 (59 commits) found and fixed 2 blocking, 2 high, and 2 medium-severity issues. No breaking changes; no migration steps required.

## Security

- **Authorization gap in `orbit_update_app` / "Edit with AI":** an app editor (`CanWrite()` but not `CanManage()`) could disable an app's own email/password authentication through the MCP tool or AI chat, despite the equivalent change being blocked on the REST API. Both surfaces now require `CanManage()`, matching REST.
- **No rate limiting on AI chat:** `ai/build-chat` and `ai/edit-chat` (8 routes total) had no per-user rate limit, unlike every other authenticated surface in the dashboard. Each is now limited per authenticated user.
- **Silent config validation gap:** saving the global AI provider config with a blank model succeeded, causing every subsequent chat turn to fail with an undiagnosable generic error. Now rejected with a 400 at save time.
- **Missing `ON DELETE CASCADE`:** the foreign key from `ai_build_sessions.target_app_id` to `apps(id)` was created without cascade delete, unlike every other FK to `apps` in the schema — deleting an app that ever had an "Edit with AI" session made it permanently undeletable. Fixed to cascade.

## Fixed

- "Edit with AI" proposing a foreign-key change on an existing column showed a "Confirm and apply" button with no description of the change. Add/remove foreign-key operations now render their table/column/referenced-table detail before confirming.

## Upgrade notes

No manual steps. The FK cascade fix runs as an idempotent migration on next boot (`DROP CONSTRAINT IF EXISTS` + re-add with `ON DELETE CASCADE`), safe on installs already on either the old or new constraint.
