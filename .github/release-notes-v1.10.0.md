## v1.10.0

Minor release that makes `required` / `default` / `unique` changes on existing columns actually reach the database, and classifies data API write failures into specific HTTP status codes instead of a generic 500.

### Fixed

- **Changing `required`, `default` or `unique` on an existing column only updated the stored metadata — Postgres was never altered.** The provisioner is add-only for existing columns, so no `ALTER COLUMN` ever ran and metadata silently diverged from the database: a column shown as optional could stay `NOT NULL`, and every insert omitting it failed as a generic `500`. Saving a table now runs the real DDL (`SET/DROP NOT NULL`, `SET/DROP DEFAULT`, add/drop the column's single-column `UNIQUE` constraint) in one transaction. A change the existing data doesn't allow is rejected with `400` **before any DDL runs**, naming the column and count — `NULL` rows when making a column required, duplicated values when making it unique, a foreign key depending on a unique being removed, or a default Postgres rejects for the type. Adding a new required column without a default to a non-empty table is now also a `400` instead of `500 "provisioning failed"`. Same typed error in MCP tools and the AI build/edit chats.
- **Data API writes rejected because of the request now answer specific errors** instead of `failed to insert/update/delete row` (500). Messages name only the column/constraint, never the value sent:
  - missing/null required column → `400 column "x" is required`
  - foreign key to a row that doesn't exist (insert/update) → `400`
  - value invalid for the column type (any `22xxx`, or a JSON number that doesn't fit `integer`/`bigint`) → `400`
  - delete of a row another table still references → `409`
  - unique violation on update → `409` (insert already did this)
  - every remaining `500` is now logged server-side, including update/delete.

### Breaking change

- A write denied by a native row-level security `WITH CHECK` policy (`42501`) now answers **`403 "row-level security policy denied this write"`** instead of a generic `500`. Clients that special-cased the old `500` should handle `403`. Inbound webhook writes are unchanged (still `500`, so the sender retries).

### Upgrade notes

No migration needed. Columns whose metadata already drifted from the database before this release are **not** repaired automatically — saving a table only applies the changes made in that save. Audit existing tables (stored `zeep_system.app_tables.columns` vs `information_schema.columns`) and fix any drift once. `ADD UNIQUE` / `SET NOT NULL` scan the table under an exclusive lock; on very large tables, expect that save to take as long as the scan.
