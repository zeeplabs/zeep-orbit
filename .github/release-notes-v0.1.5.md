# v0.1.5 — Audit Log, File Storage, Rate Limiting, i18n, SDK Clients, Dashboard

## Highlights

### 🔐 Audit Log
Track every dashboard action — app CRUD, user management, auth, config changes, data browser. Superadmin view with filters by action type and user. Stored in `zeep_system.audit_log`.

### 📁 File Storage per App
Connect S3-compatible buckets (DO Spaces, Magalu, AWS, MinIO) per app via dashboard. Full REST API: upload (multipart), list, get, download (signed URL), delete.

### 🚦 Rate Limiting per App
Per-IP sliding window rate limiting. Set requests per minute in the dashboard "API" tab. Returns 429 when exceeded.

### 🌐 i18n
Full dashboard translation in pt-BR and English. Language switcher in sidebar. Preference saved per user.

### 📦 SDK Clients
6 official SDKs, same API across all languages:
- TypeScript: `@zeeptech/orbit-client`
- Go: `github.com/zeeplabs/orbit-go`
- Python: `zeeplabs-orbit-client`
- Rust: `zeep-orbit-client`
- Java: `com.zeeplabs:orbit-client`
- PHP: `zeeplabs/orbit-client`

### 🖥️ Dashboard
- Tabs in app form (Database, Login Providers, Storage S3) — routed via `?tab=`
- App owner info for superadmin
- User name field (onboarding, create user, table display)
- Sidebar shows name instead of email
- English as default language
- SVG favicon

### 🐛 Bug Fixes
- Login/logout redirect fixes

### 📦 Docker
```bash
docker pull ghcr.io/zeeplabs/zeep-orbit:v0.1.5
```

### 📋 Helm
```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-orbit
helm install zeep-orbit zeeplabs/zeep-orbit
```
