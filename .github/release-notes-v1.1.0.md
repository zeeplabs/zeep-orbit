## v1.1.0

Native row-level access control for end users, configurable end-user business roles, richer app-user editing, and a round of pre-release hardening caught by an internal audit before this version shipped.

### ✨ Added

- **End-user row policies (native Postgres RLS)** — define row-level access rules per table/action (`select`/`insert`/`update`/`delete`) combining an end-user's business `role` with a condition on the row's own data (e.g. `requester_id != claim:sub`), enforced natively by Postgres (`CREATE POLICY`/`ROW LEVEL SECURITY`) — the rule holds even against a direct Postgres connection, not just the Orbit REST API. Configured from a new "Policies" tab on the table detail view. Tables with no policy keep today's exact behavior.
- **Configurable end-user business roles per app** — define your own role list per app (chips, not a raw CSV input) instead of being stuck with the hardcoded `member` default. Removing a role still assigned to a user or referenced by a row policy is blocked with a clear `409` instead of silently orphaning data.
- **App user email/phone editing** — the "Usuários do App" edit drawer now also lets admins correct an end-user's `email` and `phone`, with a country-aware phone mask (235 countries) and E.164 validation.
- **Users tab on the app detail screen** — app-user management moved from a standalone page to a tab on `/apps/:id`; old links redirect automatically.

### 🔒 Security

Found and fixed before this version shipped:

- Apps provisioned before this release would have lost end-user data access after upgrading — end-user requests now always run under a dedicated, restricted Postgres role (`zeep_app_enduser`), and the grant making that role able to see an app's tables previously only ran when the app's schema was touched through the Dashboard. A boot-time backfill now grants every existing app before serving any request.
- Login/token-refresh rate limiting was not actually throttling anything — it bucketed by client host **and ephemeral port**, so every new connection got its own bucket, letting brute-force attempts through unlimited. Now buckets by host only.
- A transient database read failure while updating global system settings (Settings → Database) could silently overwrite global storage credentials and reset retention/timeout settings with zero values instead of failing the request with a clear error.

### 🐛 Fixed

- `PhoneInput` (app-user edit drawer) no longer resets the country picker to Brazil when a country is chosen before any digits are typed — the common case when editing a user who has no phone on file yet.
- CI now runs the full Go test suite against a real Postgres service. Previously every test requiring a database silently skipped for lack of `TEST_DATABASE_URL`, including all the tests proving row-policy/RLS enforcement — the most security-sensitive code in this release shipped with effectively no CI coverage until now.

### Upgrade notes

- **Requires `CREATEROLE` (or superuser) on the configured Postgres connection.** Boot now creates the `zeep_app_enduser` role if it doesn't exist. A managed Postgres instance with a restricted connection role will fail to boot with a clear Postgres permission error until that privilege is granted.
- No breaking API changes. `PUT /dashboard/api/apps/{id}/users/{userId}/role` — added and removed within this same development cycle, never part of a published release — was folded into the combined `PUT /dashboard/api/apps/{id}/users/{userId}` (`{email, phone, role}`).
