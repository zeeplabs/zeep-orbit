## v1.2.0

Closes the last gap in table policy management: policies can now be edited in place instead of deleted and recreated.

### ✨ Added

- **Table policy editing** — new `PUT /dashboard/api/apps/{id}/tables/{table}/policies/{policyId}` lets admins edit an existing row policy's roles, clauses, and action without deleting and recreating it, preserving the original creator/creation-time trail and avoiding the brief window with no enforcement that a delete+recreate cycle left open. The edit replaces the native Postgres policy (`DROP POLICY` + `CREATE POLICY`, same transaction as the catalog update) and stamps new `updated_at`/`updated_by` columns on `zeep_system.table_policies`.
- The Policies tab now has an "Edit" button next to delete, opening the same builder form used for creation, pre-populated with the policy's current data — including any role no longer in the app's configured role list, still shown as a selected chip instead of being silently dropped.

### Upgrade notes

No breaking changes. No new required configuration.
