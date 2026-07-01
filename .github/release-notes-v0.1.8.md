# v0.1.8 — Health Endpoint, Soft Delete, CORS, Hyphens, RLS Fix

## Highlights

### ❤️ Per-App Health Endpoint
Every app now exposes `GET /{app}/health` for monitoring and readiness checks:

```json
GET /meu-app/health

{
  "status": "ok",
  "app": "meu-app",
  "healthy": true,
  "checks": {
    "database": true,
    "schema": true
  }
}
```

- **200** — app is healthy (DB reachable, schema exists)
- **404** — app not found
- **503** — database or schema unavailable
- No authentication required — works out of the box for load balancers and monitoring tools

### 🏷️ App Name Now Supports Hyphens
App names can now use hyphens (`-`) alongside underscores (`_`):

| Before | After |
|--------|-------|
| `meu_app` | `meu-app` or `meu_app` |
| `https://host/meu_app/todos` | `https://host/meu-app/todos` |

Existing apps with underscores continue to work unchanged. PostgreSQL schemas automatically map hyphens to underscores.

### 🖥️ Dashboard — API Tab Improvements
- **Base URL card** visible in edit mode — shows full URL with copy button
- **Usage examples** for register, login, CRUD, and health — all translated via i18n
- All new texts available in pt-BR and English

### 🗑️ Soft Delete (Configurable)
New **Soft Delete** toggle in Dashboard Settings (superadmin only):

- **Disabled (default):** `DELETE` removes records permanently — current behavior, no breaking changes
- **Enabled:** `DELETE` becomes `UPDATE SET deleted_at = now()`. Listings automatically filter out soft-deleted records. Use `?deleted=true` to view them.
- `deleted_at` column is always present in every table (harmless when disabled)
- Toggle takes effect immediately — no restart required

### 🌐 CORS Support
Added CORS middleware (`github.com/go-chi/cors`) to allow cross-origin requests from SPAs and mobile apps:
- Accepts all origins (`*`) — ideal for self-hosted deployments
- Handles preflight `OPTIONS` requests automatically
- Headers: `Accept`, `Authorization`, `Content-Type`, `X-Requested-With`

### 🐛 Bug Fixes
- **RLS "enabled" now injects owner_id correctly** — `resolveOwner` was only handling `"owner"` RLS but not `"enabled"`. This caused INSERT to fail with NOT NULL violation on `owner_id` when creating records in apps with restricted access.
- **Error messages improved** — `HandleCreate` now includes the actual pgx error in the response for easier debugging.

### 🌐 i18n
- `appForm.tabApi` — API tab label
- `appForm.apiBaseUrl.*` — 8 new keys for Base URL card and examples
- Updated name-related keys for hyphen support

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.8
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```
