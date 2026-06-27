# Changelog

All notable changes to this project will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [0.1.0] — 2026-06-27

### Added

- YAML config loader with `${ENV_VAR}` interpolation and full validation
- PostgreSQL client with `pgxpool` (connection pooling, startup ping)
- Idempotent schema/table provisioner (`CREATE SCHEMA IF NOT EXISTS`, `ALTER TABLE ADD COLUMN IF NOT EXISTS`)
- Thread-safe in-memory app registry (`sync.RWMutex`)
- SQL query builder: LIST (filter, order, pagination), INSERT, UPDATE (partial), DELETE, GET by ID — all parameterized, field names validated against registry
- JWT middleware: HS256 per-app secret, cross-app token isolation, `alg: none` blocked
- CRUD HTTP handlers: 200/201/204/400/401/404 status codes
- chi v5 router with `/health`, `/metrics`, `/{app}/{table}`, `/{app}/{table}/{id}` routes
- Prometheus metrics: `zeep_http_requests_total`, `zeep_http_request_duration_seconds`, `zeep_active_apps`
- Structured JSON request logging via `zap`
- Graceful shutdown with 30-second timeout on SIGINT/SIGTERM
- CLI (`cobra`): `zeep serve`, `zeep apply`, `zeep list`, `zeep status`
- Global CLI flags: `--config`, `--port`, `--db`
- Multi-stage Dockerfile (Go builder → scratch, < 20MB)
- `docker-compose.yml` with `postgres:16-alpine` and health check
