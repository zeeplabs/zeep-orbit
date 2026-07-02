# v0.1.11 — Google OAuth Fix, API Docs Branding

## Highlights

### 🐛 Google OAuth "Session Expired" Fix
Fixed a bug where Google login on the dashboard always redirected to "Sessão expirada ou inválida, tente novamente" after granting consent.

**Root cause:** the OAuth `state` param was stored in an in-memory map scoped to a single pod. With `replicaCount > 1` (the default — HPA runs 2–10) and no sticky session configured on the Service/Ingress, `/auth/google/login` and `/auth/google/callback` could land on different pods. The callback pod never had the `state` in its own memory, so validation always failed.

**Fix:** the `state` param is now a stateless token — it carries the `redirect` URL and an expiry, signed with HMAC-SHA256 using the app's existing JWT secret. Any replica can verify the signature and expiry without any shared state, cache, or database round trip.

- No new infrastructure dependency (no Redis, no new table)
- Works correctly regardless of replica count or pod restarts between login and callback
- Covered by new unit tests: valid round-trip, cross-replica validation, tampered signature, wrong secret, expired token, malformed token

### 🐛 API Docs Branding
The generated OpenAPI spec and the `/docs` index page showed the hardcoded title `zeep-orbit API` / `zeep-orbit API docs`. Both now show a neutral `API Documentation` title.

### 🎨 Dashboard Copy
Login page and app subtitle copy updated for clarity:

- App subtitle: `BaaS Platform Manager` → `Database-first backend platform`
- Login title: `BaaS Platform Manager` → `Create backends from your database`
- New login subtitle: `Generate backend apps, REST APIs, and permissions directly from your database schema.`

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.11
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit/helm
helm install zeep-orbit zeeplabs/zeep-orbit --values values.yaml

# Upgrade existing release:
helm repo update zeeplabs
helm upgrade zeep-orbit zeeplabs/zeep-orbit --values values.yaml --atomic
```
