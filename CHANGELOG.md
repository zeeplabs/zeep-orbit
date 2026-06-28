# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [Unreleased]

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
