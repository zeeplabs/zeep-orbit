# Wiring a frontend to an Orbit backend app

Typical shape: an app owner (or an agent via MCP) defines the app's tables/policies on Orbit. Orbit then exposes:

1. A REST API at `/{app}/...` (see `rest-api.md`)
2. Auto-generated OpenAPI docs for that app, discoverable from the Dashboard
3. Official client SDKs that wrap #1 (see `clients.md`)

## End-to-end flow (example: React + TypeScript)

1. **Get an app-scoped JWT.** The frontend calls `POST /{app}/auth/login` (or `/register` for new users) — do this through the TS client's `orbit.auth.login(...)`, not raw `fetch`, so token refresh is handled consistently.
2. **Store the JWT** the same way you'd store any session token (memory + refresh flow, or a secure cookie if your setup proxies through a backend-for-frontend). Orbit does not dictate frontend session storage — that's your app's call, treat it with normal JWT-storage security hygiene (avoid `localStorage` for anything sensitive if you can avoid it).
3. **Instantiate one `OrbitClient`** per app, scoped with `baseURL` + `app` + the current JWT. Re-create or update the client instance when the JWT refreshes.
4. **Read data** with `orbit.table(name).findMany({ filters, limit, offset })` — map Orbit's filter operators (`eq.`, `gt.`, `ilike.`, ...) to whatever query-building your frontend does (search box → `ilike.%term%`, pagination → `limit`/`offset`).
5. **Write data** with `orbit.table(name).create/update/delete` — never construct the create/update body from the full row object returned by a GET; strip system fields (`id`, `created_at`, `updated_at`, `owner_id`, `deleted_at`) before sending, since the server rejects/ignores them anyway and sending them back is a common copy-paste bug.
6. **Files**, if the app has storage enabled: `orbit.files.upload(...)` returns a file record; use `orbit.files.signedURL(id)` to get a display/download URL rather than constructing storage URLs manually — signed URLs expire and are the only sanctioned way to reference private files from the frontend.
7. **Handle RLS-shaped errors as data, not exceptions to swallow**: an empty list from a GET can legitimately mean "RLS filtered everything out for this user," not "no rows exist." Don't build empty-state UI logic that assumes the table itself is empty — that assumption breaks the first time policies scope data per-owner.

## What Orbit does NOT do for your frontend

- No client-side ORM/schema generation beyond what the OpenAPI spec gives you — if you want typed responses in TypeScript, generate types from the app's OpenAPI doc yourself (standard `openapi-typescript`-style tooling), Orbit doesn't ship a codegen step.
- No frontend hosting/build pipeline — Orbit is the backend only.
- No client-side caching/state layer — pair the client with your app's own data-fetching layer (react-query, SWR, etc.) the same way you would for any REST backend.
