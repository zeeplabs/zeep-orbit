# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

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
