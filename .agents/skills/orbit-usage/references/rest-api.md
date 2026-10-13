# Orbit REST API

Every app on Orbit gets these routes, mounted under `/{app}/...` where `{app}` is the app's name:

## Auth — `/{app}/auth/*`

| Route | Method | Notes |
|---|---|---|
| `/providers` | GET | List enabled auth providers (email, Google, ...) for this app |
| `/register` | POST | Rate-limited |
| `/login` | POST | Rate-limited. Returns a JWT scoped to this app |
| `/refresh` | POST | Refresh the session |
| `/token/refresh` | POST | Rate-limited |
| `/logout` | POST | Requires `Authorization: Bearer <jwt>` |
| `/me` | GET | Requires JWT |
| `/me` | PATCH | Requires JWT |
| `/google/login`, `/google/callback` | GET | OAuth flow, if Google auth is enabled for the app |

The JWT returned here is **per-app**, not the Dashboard/MCP PAT. Send it as `Authorization: Bearer <jwt>` on every table/files call below.

## Files — `/{app}/files/*`

Requires JWT. Only present if the app has storage enabled.

| Route | Method |
|---|---|
| `/` | POST (upload), GET (list) |
| `/{id}` | GET, DELETE |
| `/{id}/download` | GET |
| `/{id}/url` | GET — signed URL |

## Tables — `/{app}/{table}` and `/{app}/{table}/{id}`

Requires JWT. Rate-limited per app.

| Route | Method | Behavior |
|---|---|---|
| `/{app}/{table}` | GET | List rows. See query params below. Response: `{"data": [...], "count": N, "limit": L, "offset": O}` |
| `/{app}/{table}` | POST | Create a row. Body = JSON object of column values. Never send `id`, `created_at`, `updated_at`, `owner_id`, `deleted_at` — these are server-managed and rejected/ignored if sent. Returns the created row (201). |
| `/{app}/{table}/{id}` | GET | Fetch one row by id |
| `/{app}/{table}/{id}` | PUT / PATCH | Update — both behave the same (partial update: only send the fields you want to change) |
| `/{app}/{table}/{id}` | DELETE | Soft-delete if the app has soft-delete enabled, otherwise hard-delete |

### List query params (GET `/{app}/{table}`)

- `limit` (default 50, max 1000), `offset`
- `order={field}.asc` or `order={field}.desc`
- `deleted=true` — include soft-deleted rows (only meaningful if soft-delete is enabled)
- **Column filters**: `?{column}={operator}.{value}`, operators are `eq.` `ne.` `gt.` `gte.` `lt.` `lte.` `like.` `ilike.`. Example: `GET /myapp/invoices?status=eq.pending&amount=gte.100&order=created_at.desc`

### Row-Level Security

Every table call runs under the requesting user's RLS claims. If a table's policies don't grant access, you'll get an empty list (not an error) on GET, or a check-constraint/permission error on write — this is Postgres RLS behavior, not a bug in your client code.

### Errors

All error bodies are `{"error": "<message>"}`, message always in English. Write failures caused by the request are classified instead of collapsing into a generic 500 — messages name the column/constraint and never echo the value you sent:

| Cause | Status | Example message |
|---|---|---|
| Required column missing / set to null | 400 | `column "serial" is required` |
| Value not allowed by a check/enum constraint | 400 | `value not allowed for column "status"` |
| Value invalid for the column type (out of range, not a whole number, bad timestamp, invalid bytes) | 400 | `invalid value for column "qty"` |
| Foreign key points to a row that doesn't exist (insert/update) | 400 | `referenced row does not exist: foreign key constraint "children_parent_id_fkey"` |
| Native row-level security policy denied the write | 403 | `row-level security policy denied this write` |
| Unique constraint violated | 409 | `row already exists: unique constraint "assets_code_key"` |
| Delete of a row another table still references | 409 | `row is still referenced by another table: foreign key constraint "..."` |

A remaining 500 (`failed to insert row` / `failed to update row` / `failed to delete row`) is a server-side failure; the real cause is logged by Orbit.

## Health

`GET /{app}/health` — no auth required, cheap liveness check for that app.
