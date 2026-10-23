## v1.11.0

Minor release focused on running Zeep Orbit with several replicas. Changes made on one pod now reach the others within about 2 seconds, and schema changes can no longer freeze a table until the pods are restarted. The dashboard also gets a large accessibility and session-handling pass.

### Added

- **`GET /ready` readiness endpoint.** Pings Postgres (2s timeout) through its own one-connection pool and answers `503 {"status":"unavailable"}` when the database is unreachable. `/health` stays a liveness check that never touches the database. The Helm chart and `k8s/deployment.yaml` now point the readiness probe at `/ready`.
- **`DB_MAX_CONNS`** sets the Postgres pool size per pod (default `10`, previously hardcoded). Exposed in the Helm chart as `database.maxConns`, rendered only when set. An invalid value stops boot.
- **Dashboard: unsaved-changes prompt on in-app navigation** (sidebar, mobile menu, back/forward, app detail tabs), not only on tab close.
- **Dashboard: view state in the URL.** Data Browser (app, table, page, sort, filters), Logs (app filter) and Audit log (category, e-mail, page) survive reloads and can be shared as links.

### Fixed

- **Replicas served stale apps, tables and settings until restarted.** Writes to apps, tables, system config and token revocations bump `zeep_system.state_version` through database triggers (dashboard, MCP, AI chat and manual SQL are all covered). Every pod polls it every 2s and reloads; a lookup miss syncs immediately. A revoked app token stops working on every pod within that window.
- **Creating or editing tables could freeze until the pods were restarted.** Every DDL path now runs in one transaction under a per-app lock shared by all replicas, with lock waits capped at 5s. A timeout rolls the whole change back, including table deletes, and answers `409 "table is busy with another operation, try again in a few seconds"` on REST and MCP. The lock holders are logged without query text, role name or client address.
- **The dashboard's app-users pages took an exclusive lock on `_auth_users` on every request**, which could queue behind a read and block logins. The catalog is now checked first.
- **Soft-delete purge** could leak its advisory lock and stop running on every replica, and hung with a one-connection pool. Both fixed.
- **Dashboard sessions:** an expired session redirects to login and back to the same screen; a database error during the session check answers `503` instead of logging the user out; logging out with unsaved edits no longer leaves a dead session.
- **Dashboard Settings** forms show an error with "Try again" when their saved values fail to load, instead of empty defaults that a Save would write back.
- Many dashboard fixes: requests that ignored failed responses, the GitHub template "delete" (now "Deactivate", with confirmation), mobile overflow on the app detail page and policy rows, and more. See `CHANGELOG.md`.

### Improved

- **Accessibility:** keyboard-operable tables and mobile menu, accessible table names, skip-to-content link, visible focus rings, WCAG AA contrast for status pills, warnings, tertiary text and destructive buttons, no text under 12px, `<html lang>` follows the selected language, reduced motion respected.
- **Localization:** dates follow the selected dashboard language; remaining hardcoded strings translated (`en` and `pt-BR`).

### Upgrade notes

- **Boot is stricter.** A pod now exits if it can't load the system config or the initial registry, instead of serving with no statement timeout and soft delete off. Check your database connectivity before rolling out.
- **Connection budget.** Each pod opens up to `DB_MAX_CONNS` connections, plus 1 for the readiness probe and up to 4 for inbound webhook deduplication. Keep `replicas × (DB_MAX_CONNS + 5)`, counting the HPA maximum, under Postgres `max_connections`.
- **Readiness probe.** If you deploy with your own manifests, point the readiness probe at `/ready` and keep the liveness probe on `/health`.
- **Migrations run automatically at boot** (new `zeep_system.state_version` table and triggers). They are idempotent and safe when several pods boot at once. During a rolling deploy, v1.10.0 pods keep working; they just don't poll for changes.
- **Not verified in this release:** running behind a connection pooler such as PgBouncer. The design avoids `LISTEN/NOTIFY` and session state for this reason, but test it in staging first.
- If a table freezes again, follow the runbook in `docs/docs/deployment.md` before restarting pods.
