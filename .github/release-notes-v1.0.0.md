## v1.0.0

Zeep Orbit's first stable release. This version brings fine-grained access control (per-app and platform-wide), table relationships in the schema builder, retention/query-timeout controls, a full dashboard redesign, and a round of security and stability fixes found in a pre-release audit.

### ✨ Added

- **Per-app roles (admin / editor / viewer)** — every backend and frontend app now has per-user membership with a role that gates reads, writes, and management actions. A new "Members" tab in the app details page lets admins add, change, and remove members.
- **4-tier platform roles** (`superadmin` / `admin` / `auditor` / `member`), replacing the old 2-tier model. A single permission matrix drives both backend enforcement and what the dashboard UI shows.
- **Table relationships (foreign keys) and indexes** in the schema builder, with validation (unknown table/column, invalid `on_delete`, circular dependencies), topological table ordering, and idempotent provisioning.
- **Retention period + soft-delete purge**, a configurable query **statement timeout**, and a configurable **CSV export row cap** (Settings → Database).
- **Render deploy provider**: Environment ID field (for Projects with multiple Environments) and a live "Recent Deploys" list in the Deploy providers tab.
- **Full dashboard visual redesign** — every screen re-skinned to the new design system; `framer-motion` and `lucide-react` fully removed.

### 🔒 Security

Found and fixed in a pre-release audit before this version shipped:

- A platform-wide `admin`/`auditor` with explicit per-app membership was incorrectly downgraded to `viewer` on that app instead of keeping their real membership role.
- `GET /apps/{id}` exposed an app's JWT secret to any role with read access to the app, instead of only to admins — the secret is enough to forge a valid end-user token.
- Several per-app actions (deactivating/resetting end-user sessions, token issuance, table create/update/delete) and the Data Browser's write endpoints had no role gate, letting read-only roles (`viewer`, platform `auditor`) perform writes.
- The "at least one admin per app" invariant never actually ran, due to an invalid Postgres query (`SELECT COUNT(*) ... FOR UPDATE`) — every attempt to demote or remove an app's last admin failed with a raw 500 instead of a clean rejection. Two admins concurrently demoting each other could also deadlock. Both are now correct under concurrency.

### 🐛 Fixed

- `DELETE /apps/{id}` without permission returned a 500 instead of a clean 403.
- The role migration that introduced the 4-tier platform role model could re-run on every server restart and silently re-demote admins created after the first migration.
- The soft-delete purge job is hardened against multiple replicas racing the same purge window, partial-failure runs going unaudited, and a fragile date-interval query.

### Upgrade notes

No breaking API changes for end-user-facing app endpoints. Dashboard-side changes to be aware of:

- Existing dashboard `admin` users are reclassified to `member` on first boot after upgrading (one-time, guarded migration) — re-grant `admin`/`superadmin` to whoever needs platform-management access after upgrading.
- New Postgres columns are added additively via idempotent migrations (`ADD COLUMN IF NOT EXISTS`) — no manual migration steps required.
- `statement_timeout` now defaults to **30s** on app data-plane queries. Set it to `0` in Settings → Database to restore unbounded queries if you rely on long-running queries today.
