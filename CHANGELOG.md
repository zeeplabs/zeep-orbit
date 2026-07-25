# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [0.3.0] — 2026-07-25

### Added

- **App Tokens** — JWT-based token management for apps without email/password auth. Dashboard exposes 5 endpoints (list, create, revoke, revoke-all via secret regeneration, view secret) and a new "Tokens" tab on the app details page (create with configurable expiration — 7d/30d/365d/never —, one-time JWT reveal with copy, status badges, 2-step confirm to regenerate the app secret).
- `POST /{app}/auth/token/refresh` — reissues an app token JWT (same `jti`), extending `expires_at` by the token's original duration.
- `zeep_system.app_tokens` table (`id`, `app_id` FK cascade, `name`, `jti` unique, `expires_at`, `revoked_at`, `last_used_at`, `created_at`), indexed on `app_id` and `jti`.
- `internal/tokencache` — shared in-memory jti activity cache (30s TTL) used by the request-path middleware and invalidated immediately on revoke/regenerate-secret.

### Fixed

- `JWTMiddleware` validated app-token `jti`/revocation using a second JWT parse with a `nil` keyFunc, which `golang-jwt/v5` always rejects — the revocation check was dead code; a revoked token kept authenticating until its own `exp`. Now reuses the claims from the already-verified parse.
- `RegenerateAppSecret` was missing the app-ownership check present in every sibling token endpoint, letting any authenticated dashboard user rotate the JWT secret (and revoke all tokens) of an app they don't own.
- `CreateAppToken` could nil-pointer-dereference on a registry cache miss; now reads the JWT secret from the already-loaded app row instead.
- jti activity cache was never invalidated on revoke/regenerate-secret, giving a revoked token up to 30s of extra validity.
- `randomJTI` fell back to a fixed, predictable id when `crypto/rand.Read` failed instead of returning an error.

### Security

- Rate limiting added to `POST /{app}/auth/token/refresh`, matching `/register` and `/login`.

---

## [0.2.0] — 2026-07-13

### Fixed

- **Company Name / Theme resetting on restart** — the server reseeded `zeep_system.brand_config` on every boot via an upsert that always applies the env var (or hardcoded default) as the new value, overwriting anything saved through the dashboard Settings page. Startup seeding is now insert-only (`ON CONFLICT DO NOTHING`) and never touches an existing row — saved settings survive restarts.

### Removed

- **Custom logo upload** — removed the ability to upload a custom login logo / app icon from Settings. The dashboard and login page now always use the default Zeep Orbit logo.

---

## [0.1.11] — 2026-07-02

### Fixed

- **Google OAuth "session expired" on callback** — OAuth `state` was stored in an in-memory map per pod. With multiple replicas and no sticky session, `/login` and `/callback` could land on different pods, so the state was never found and login always failed with "Login expired, please try again". State is now a stateless, HMAC-signed token (embeds `redirect` + expiry, signed with the app's JWT secret) — any replica can validate it without shared storage.
- **API docs branding** — Generated OpenAPI spec title and docs index page now show "API Documentation" instead of the hardcoded "zeep-orbit" name.

### Changed

- **Dashboard copy** — Login page and app subtitle updated to "Database-first backend platform" / "Create backends from your database", with new supporting subtitle text.

## [0.1.10] — 2026-07-02

### Added

- **Global S3 Configuration** — Superadmin configures global S3 (endpoint, region, keys, bucket). Admins creating apps only need a folder name (auto-filled with app name). Files stored as subfolders within the global bucket.
- **Brand Logos** — Superadmin uploads login logo and sidebar icon to global S3 bucket. Instant sidebar update. Fallback to built-in SVGs when not configured.
- **Google Setup page** — First-time Google login users are prompted to set name + password as backup auth method.
- **Settings tabs** — Settings page reorganized into 5 tabs: Branding, Logo, Database, Auth Provider, Storage.
- **Toast notifications** — `sonner` toasts replace native `alert()` throughout the dashboard.
- **Public brand config endpoint** — `GET /dashboard/api/brand/config` (unauthenticated) returns logo URLs for login page.
- **`needs_setup` on `/api/me`** — Frontend detects Google users who need to complete account setup.
- **`storage_configured` on `/api/config`** — Frontend knows if global S3 is active.
- **`Folder` field in StorageConfig** — Files in global S3 are prefixed with the app's folder name.

### Fixed

- **Helm chart 404** — Chart now published alongside Docusaurus docs at `https://zeeplabs.github.io/zeep-orbit/helm/` (was on orphan `gh-pages` branch ignored by Pages).
- **Helm `--config` flag** — Removed invalid `--config /app/apps.yaml` from deployment args (`zeep serve` doesn't use config files).
- **Race condition on startup** — `ProvisionZeepSystem` wrapped in transaction with `pg_advisory_xact_lock` to serialize across concurrent pods.
- **Logo upload overwrite** — Uploading one logo no longer clears the other.
- **Global S3 save resets soft delete** — `GlobalStorageCard` now preserves existing `soft_delete_enabled`.
- **Hardcoded texts** — All user-facing strings in BrandSettingsPage and AppFormPage storage section moved to i18n keys.

### Changed

- **Settings page** — Now uses tabs routed via `?tab=` (branding, logo, database, auth, storage).
- **App form storage** — When global S3 is active, bucket field is read-only and automatically set to the app name.
- **Dashboard sidebar** — Icon fetched dynamically from `GET /dashboard/api/brand/config` with query cache invalidation on upload.
- **`storage_config` column** — App storage config now supports `folder` field for global S3 subfolder prefix.
- **`brand_config`** — Added `icon_url` column for sidebar icon.
- **`system_config`** — Added `storage_config` JSONB column for global S3 settings.

## [0.1.5] — 2026-06-29

### Added

- **Audit Log** — action history tracked in `zeep_system.audit_log` (who, what, when, IP). Dashboard UI with filters by action/user, pagination. Superadmin only.
- **File Storage per App** — S3-compatible buckets (DO Spaces, Magalu, AWS, MinIO). Config per app via dashboard. Endpoints: upload, list, get, download (signed URL), delete. Uses `aws-sdk-go-v2`.
- **Rate Limiting per App** — configurable RPM via `rate_limit_config` JSONB. Sliding-window middleware per IP. Config in dashboard "API" tab. Returns 429 when exceeded.
- **i18n — pt-BR + English** — `i18next` + `react-i18next` in all 13 pages. Language switcher in sidebar. ~250 translation keys.
- **Language per User** — `language` column in `dashboard_users`. `PUT /api/me/language`. Auto-applied on login.
- **SDK Clients** — 6 official clients, same API design:
  - TypeScript: `@zeeptech/orbit-client` (npm)
  - Go: `github.com/zeeplabs/orbit-go` (git tag)
  - Python: `zeeplabs-orbit-client` (PyPI)
  - Rust: `zeep-orbit-client` (crates.io)
  - Java: `com.zeeplabs:orbit-client` (Maven Central)
  - PHP: `zeeplabs/orbit-client` (Packagist)
- **Dashboard — tabs no form** — AppFormPage reorganizado em 3 tabs + roteadas via `?tab=`
- **Dashboard — owner no card** — superadmin vê nome/email do dono do app no card
- **Dashboard — nome do usuário** — coluna `name` em `dashboard_users`. Campos no onboarding e criar usuário
- **Dashboard — sidebar com nome** — exibe nome do usuário (fallback email)
- **Dashboard — favicon** — SVG logotype como favicon
- **Dashboard — English default** — idioma padrão alterado para `en`

### Changed

- Dashboard tabs roteadas via `useSearchParams` (preserva tab ao recarregar)
- Backend: todas as queries de app agora incluem `rate_limit_config`, `storage_config`, `owner_email`, `owner_name`
- Backend: `ListApps` faz JOIN com `dashboard_users` para dados do dono

### Docs

- README atualizado com File Storage, Rate Limiting, SDK Clients, i18n
- Docusaurus: novas páginas `api/files.md`, `api/rate-limiting.md`, `clients.md`
- RELEASE.md: instruções de publicação para todos os 6 SDKs
- CHANGELOG.md: esta entrada

## [0.1.0] — 2026-06-28

### Added

- Web dashboard (React + Vite + TypeScript, embedded via `go:embed`)
- Dashboard auth: email/password login + session cookies
- Dashboard auth: Google OAuth sign-in (config via DB or env vars, encrypted secrets)
- Dashboard onboarding wizard (first-time superadmin setup)
- Dashboard app CRUD (create/edit/delete apps with dynamic tables & columns)
- Dashboard user management (superadmin creates/manages dashboard admins)
- Dashboard data browser (browse, filter, sort, edit inline, delete rows, export CSV)
- Dashboard real-time request logs with metrics (ring buffer, app-level filter)
- Dashboard white-label branding (5 themes, company name, persisted to DB)
- Dashboard change password (own + superadmin for any user)
- Dashboard app users management (list, search, deactivate/reactivate, reset sessions)
- Dashboard auth providers configuration (Google OAuth setup via UI)
- Per-app auth providers (email + Google OAuth configurable per app)
- Native email/password auth per app (register, login, refresh, logout, me)
- Google OAuth per app (/{app}/auth/google/login + callback)
- Row-Level Security (`rls: owner` — auto-filter by JWT `sub`)
- OpenAPI/Swagger docs auto-generated per app
- Helm chart (production-grade: HPA, PDB, ingress, ServiceMonitor, IRSA)
- K8s manifests (Kustomize)
- GitHub Actions CI/CD (multi-platform Docker build, Helm chart release)
- AES-256-GCM encryption for sensitive data at rest
- Auth providers config table (`zeep_system.auth_providers`)
- `go mod tidy` — dependency cleanup

### Fixed

- Login 500 error when `google_id` is NULL (COALESCE fix for pgx v5)
- FK violation on DataBrowserCreate (owner_id injection removed)
- Race condition TOCTOU on bootstrap endpoint
- DDL injection prevention on table/column names
- JWT secret exposure in API responses
- React Query cache not cleared on login/logout (user switching)

### Security

- Rate limiting on public auth routes (10 req/min)
- Security headers (X-Content-Type-Options, X-Frame-Options, etc.)
- bcrypt cost 12 for password hashing
- CSV formula injection protection
- Encryption at rest for OAuth client secrets (AES-256-GCM)
