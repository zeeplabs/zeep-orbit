## v0.5.0

### ✨ Added

- **Column default values in the backend app schema builder** — set a default value when creating or editing a table column in the dashboard:
  - `integer` / `bigint` / `numeric` — a literal value
  - `boolean` — a true/false picker
  - `uuid` / `timestamptz` — auto-generate via `gen_random_uuid()` / `now()`
  - Not available for `text` / `jsonb` columns
  - SQL expressions are checked server-side against a strict, per-type allowlist before ever reaching the generated DDL — never accepted as free-form input.
- **Update notifications** — a sidebar banner (above Changelog) alerts when a new Zeep Orbit release is available on GitHub, showing the version and linking straight to the release page.

### 🐛 Fixed

- **Dashboard Google login failing behind multiple replicas** — the OAuth CSRF `state` was kept in an in-memory map per process, so `/login` and `/callback` landing on different pods behind a non-sticky load balancer always failed with "session expired or invalid." Now a signed, stateless token (HMAC-SHA256).
- **GitHub App installation had the same in-memory state issue** — fixed the same way.
- **`/apps/{id}/users` always returned an empty list** when email auth was enabled, due to an incorrect schema name lookup in 4 handlers.
- **`PATCH /{app}/auth/me` returned 405** despite being documented — the route was registered as `PUT`.
- **Auth provider "allowed domains" field couldn't be cleared** once set.
- **Google login button and brand theme flashed incorrectly** on first paint of the login screen.
- **App token refresh endpoint was missing from the per-app Swagger docs.**
- User registration now validates email format and normalizes email/name before persisting.

### Upgrade notes

No breaking changes, no manual migration steps. Standard upgrade.
