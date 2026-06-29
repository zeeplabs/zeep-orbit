# v0.1.5 — Audit Log, File Storage, Rate Limiting, i18n, SDKs, Dashboard

## Added

### 🔐 Audit Log (M3)
- New table `zeep_system.audit_log` tracking all dashboard mutations
- Captures: user, action, resource type, IP, metadata JSONB
- Dashboard UI: table with color-coded badges, filters by action/user, pagination
- Superadmin only — accessible via sidebar "Audit" and `/auditoria`
- 17 handler instrumented (CreateApp, DeleteApp, CreateUser, Login, etc.)

### 📁 File Storage per App (M4)
- Per-app S3-compatible storage (DO Spaces, Magalu, AWS S3, MinIO)
- Config via dashboard "Storage (S3)" tab with encrypted credentials
- 6 endpoints under `/{app}/files/*` — upload (multipart), list, get, download (302 → signed URL), signed URL, delete
- Auto-generated `_files` table per app schema via provisioner
- `aws-sdk-go-v2` dependency

### 🚦 Rate Limiting per App
- Column `rate_limit_config` JSONB in `zeep_system.apps`
- Sliding-window per-IP middleware in `internal/server/ratelimit.go`
- Config via dashboard "API" tab with toggle + RPM field
- Returns `429 Too Many Requests` when exceeded
- Applied to all `/{app}/*` routes (CRUD, auth, files)

### 🌐 i18n — pt-BR and English
- `i18next` + `react-i18next` in all 13 pages + components
- ~250 translation keys with pt-BR and English
- Language switcher in sidebar (Globe icon)
- Language persisted per user in `dashboard_users.language` column
- `PUT /api/me/language` endpoint
- Default language: English

### 📦 SDK Clients — 6 languages
All clients share the same API design: `client.table().findMany()`, `client.auth().login()`, `client.files().upload()`

| Language | Package | Registry |
|----------|---------|----------|
| TypeScript | `@zeeptech/orbit-client` | npm |
| Go | `github.com/zeeplabs/orbit-go` | Go modules |
| Python | `zeeplabs-orbit-client` | PyPI |
| Rust | `zeep-orbit-client` | crates.io |
| Java | `com.zeeplabs:orbit-client` | Maven Central |
| PHP | `zeeplabs/orbit-client` | Packagist |

### 🖥️ Dashboard improvements
- **Tabs in AppForm:** 3 tabs (Database, Login Providers, Storage S3), routed via `?tab=` — preserves state on reload
- **App owner info:** Superadmin sees owner name/email in app cards
- **User name:** `name` column in `dashboard_users`. Fields in onboarding wizard and create user modal. Displayed in users table
- **Sidebar:** shows user name instead of email
- **Sidebar language switcher:** Globe icon with pt-BR / English toggle
- **Favicon:** SVG logotype favicon

### 📚 Documentation
- README updated with all new features and SDK examples
- Docusaurus: new pages `api/files.md`, `api/rate-limiting.md`, `clients.md`
- `RELEASE.md` with SDK publishing instructions for all 6 registries
- `packages-accounts.md` vault document

## Changed
- Dashboard tabs route via `useSearchParams` (survive page reload)
- `ListApps` JOINs `dashboard_users` for owner email/name
- All app queries include `rate_limit_config`, `storage_config`, `owner_name`

## Infrastructure
- Go: `aws-sdk-go-v2` (config, credentials, s3, presign)
- Frontend: `i18next` + `react-i18next`
- `clients/` directory with 6 SDK projects
- `packages-accounts.md` vault document for registry credentials
