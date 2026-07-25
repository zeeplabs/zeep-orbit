# App Tokens — JWT management for apps without email auth

## Problem

Apps without email authentication have no way to generate JWTs except by
accessing the raw `jwt_secret` externally. There is no UI to view or regenerate
the secret, no token lifecycle management, and no way to revoke access without
changing the secret (which invalidates everything at once).

## Solution

A token management system in the dashboard that lets app owners generate
multiple JWTs with configurable expiration, revoke individual tokens, and
optionally refresh them via API endpoint.

## Scope

Feature is exclusive to apps with `auth_email_enabled = false`.

## Database

New table in `zeep_system`:

```sql
CREATE TABLE zeep_system.app_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id       UUID NOT NULL REFERENCES zeep_system.apps(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    jti          TEXT NOT NULL UNIQUE,       -- JWT ID, UUID v4 on creation
    expires_at   TIMESTAMPTZ,                -- NULL = never
    revoked_at   TIMESTAMPTZ,                -- NULL = active
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_app_tokens_app_id ON zeep_system.app_tokens(app_id);
CREATE INDEX idx_app_tokens_jti     ON zeep_system.app_tokens(jti);
```

## JWT Structure

```json
{
  "jti": "<uuid from app_tokens>",
  "iat": <now>,
  "exp": <optional, based on expires_at>,
  "token_type": "app_token",
  "sub": "<app_name>"
}
```

`token_type: "app_token"` distinguishes these from email-auth JWTs in
middleware.

Email-auth JWTs continue with their existing claims (`sub` = user UUID,
`email`, `app`) — no `token_type`, no `jti`.

## Dashboard UI

New tab in app details page: **Tokens**

- **List**: table of tokens per app (name, expiration, status, last used)
- **Create**: modal with `name` (text) and expiration select (7d, 30d, 365d,
  never). On creation, shows JWT once with copy button.
- **Revoke**: button per token — soft delete (`revoked_at = now()`), blocks
  immediately
- **Regenerate Secret**: button on the token tab header with two-step
  confirmation. Invalidates all existing tokens. Shows new secret once.
- **View Secret**: separate action to reveal the current `jwt_secret` (with
  confirmation dialog). Enables the user to generate JWTs externally if
  desired.
- Status badges: Ativo / Expirado / Revogado

## Dashboard API

| Method | Route | Action |
|---|---|---|
| `GET` | `/dashboard/api/apps/{id}/tokens` | List tokens for app |
| `POST` | `/dashboard/api/apps/{id}/tokens` | Create token → return JWT once |
| `POST` | `/dashboard/api/apps/{id}/tokens/{tokenId}/revoke` | Revoke token |
| `POST` | `/dashboard/api/apps/{id}/regenerate-secret` | Regenerate `jwt_secret` + revoke all tokens |
| `GET` | `/dashboard/api/apps/{id}/secret` | Show `jwt_secret` (with audit log) |

## Middleware (JWTMiddleware)

When a JWT with `token_type: "app_token"` is presented:

1. Extract `jti`
2. Look up in `zeep_system.app_tokens` (cached in memory with 30s TTL to
   avoid DB on every request)
3. Reject if `revoked_at IS NOT NULL` or `expires_at < now()` → 401
4. Update `last_used_at` (lazy: only if last update > 5 min ago, fire-and-forget)
5. Proceed

JWTs without `token_type` (email auth) continue unaffected — no extra check.

## Refresh Endpoint

`POST /{app}/auth/token/refresh`

- Requires a valid, non-revoked, non-expired JWT
- Issues a new JWT with updated `iat`/`exp`, same `jti`
- Original token remains valid until natural expiration
- Active only if `auth_email_enabled = false` (otherwise 404)
- Returns same expiry config as original token

## Regenerate Secret Flow

1. User clicks "Regenerate Secret"
2. Confirmation: "All existing tokens will be immediately invalidated."
3. Server generates new `jwt_secret` via `gen_random_bytes(32)`, updates DB
4. Server sets `revoked_at = now()` on all `app_tokens` for this app
5. Server pushes updated config to in-memory registry
6. Response shows new secret once (same pattern as token creation)
7. Audit log entry created

## Error states

| Scenario | HTTP | Behaviour |
|---|---|---|
| Token revoked | 401 | `{"error": "token revoked"}` |
| Token expired | 401 | `{"error": "token expired"}` |
| Invalid jti | 401 | `{"error": "unauthorized"}` |
| Email auth enabled | 404 | Refresh endpoint returns "not found" |
| Create token on email-auth app | 400 | `{"error": "app tokens only available for apps without email auth"}` |
