# v0.3.0 — App Tokens

## Highlights

### ✨ App Tokens
Apps without email/password auth had no way to manage their JWTs — the `jwt_secret` wasn't visible from the dashboard, tokens couldn't be revoked individually, and rotating the secret invalidated every token at once with no recovery path.

A full JWT token management system is now available on the dashboard, exclusive to apps without email auth:

- **Tokens tab** on the app details page: list with status badges (Active/Expired/Revoked), create modal (name + expiration: 7d/30d/365d/never), one-time JWT reveal with copy button
- **Regenerate Secret**: 2-step confirmation, rotates the `jwt_secret` and revokes every existing token for the app
- **Reveal Secret**: view the current `jwt_secret`, audit-logged
- `POST /{app}/auth/token/refresh` — exchange a valid app token for a new one with the same `jti`, extending `expires_at` by the token's original duration
- New `zeep_system.app_tokens` table (`id`, `app_id` FK cascade, `name`, `jti` unique, `expires_at`, `revoked_at`, `last_used_at`, `created_at`)
- `JWTMiddleware` validates revocation/expiry per-request via an in-memory jti cache (30s TTL), with a fire-and-forget `last_used_at` touch

### 🐛 Security Fixes (found in code review, before release)
- **IDOR on secret regeneration** — `RegenerateAppSecret` was missing the ownership check every sibling token endpoint has, letting any authenticated dashboard user rotate the JWT secret (and revoke all tokens) of an app they didn't own. Now enforces the same `GetApp(ctx, pool, appID, user.ID, user.Role)` check.
- **Dead revocation check** — `JWTMiddleware` validated the app-token `jti` by re-parsing the JWT with a `nil` keyFunc, which `golang-jwt/v5` always rejects. The entire revocation/expiry-by-DB check silently never ran — a revoked token kept authenticating until its own `exp`. Fixed by reusing the claims from the already-verified parse.
- **Nil pointer on registry miss** — `CreateAppToken` dereferenced a registry lookup without checking the `ok` bool, panicking on a cache miss. Now reads the JWT secret from the already-loaded app row instead.
- **Stale revocation window** — the jti cache was never invalidated on revoke/regenerate-secret, giving a revoked token up to 30s of extra validity. Extracted into `internal/tokencache`, now invalidated immediately on revoke.
- **Predictable jti fallback** — `randomJTI` fell back to a fixed, deterministic id when `crypto/rand.Read` failed instead of returning an error.
- **Missing rate limit** — `POST /{app}/auth/token/refresh` now rate-limited like `/register` and `/login`.

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.3.0
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml

# Upgrade existing release:
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```
