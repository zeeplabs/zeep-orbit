# Feature: MVP Core

## Overview

The foundational capability of zeep-core: parse a YAML config defining N apps with schemas, provision those schemas on a PostgreSQL instance, and serve auto-generated REST CRUD APIs — one per table per app — protected by per-app JWT auth.

---

## Requirements

### Config Loading

**REQ-001** The system MUST load app definitions from `apps.yaml` at startup and on `zeep apply`.

**REQ-002** Each app definition MUST include: `name` (slug), `auth.jwt_secret`, and at least one `table`.

**REQ-003** Each table definition MUST include: `name` and at least one `column` with `name` and `type`.

**REQ-004** Supported column types for M1: `text`, `integer`, `bigint`, `decimal`, `boolean`, `uuid`, `timestamptz`, `jsonb`.

**REQ-005** The system MUST validate the config on load and return clear errors for: missing required fields, invalid types, duplicate app names, duplicate table names within an app.

**REQ-006** Columns MAY include: `required` (bool, default false), `default` (literal value as string), `unique` (bool, default false).

**REQ-007** App `name` MUST match `^[a-z][a-z0-9-]{0,62}$`. Names become URL path segments and PostgreSQL schema names.

---

### Schema Provisioning

**REQ-010** On `zeep apply`, the system MUST create PostgreSQL schema `app_{name}` if it does not exist.

**REQ-011** On `zeep apply`, the system MUST create tables defined in config that do not exist in the target schema.

**REQ-012** Every provisioned table MUST include an `id` column (`uuid`, primary key, default `gen_random_uuid()`) — injected automatically, not required in config.

**REQ-013** Every provisioned table MUST include `created_at` (`timestamptz`, default `now()`) and `updated_at` (`timestamptz`, default `now()`) — injected automatically.

**REQ-014** On `zeep apply`, if a table exists but has new columns in config, the system MUST add those columns via `ALTER TABLE`.

**REQ-015** The system MUST NOT drop columns or tables on `zeep apply`. Destructive operations require explicit `zeep drop` (out of scope for M1).

**REQ-016** The system MUST be idempotent: running `zeep apply` twice with the same config produces no changes on the second run.

---

### REST API

**REQ-020** For each table in each app, the system MUST expose these endpoints:

```
GET    /{app}/{table}          → list rows
POST   /{app}/{table}          → insert row
GET    /{app}/{table}/{id}      → get row by id
PUT    /{app}/{table}/{id}      → replace row (full update)
PATCH  /{app}/{table}/{id}      → partial update
DELETE /{app}/{table}/{id}      → delete row
```

**REQ-021** `GET /{app}/{table}` MUST support query params:

- `?limit={n}` — max rows returned (default 50, max 1000)
- `?offset={n}` — pagination offset (default 0)
- `?{field}=eq.{value}` — equality filter
- `?order={field}.asc` or `?order={field}.desc`

**REQ-022** List response MUST include a `count` field with total matching rows (before limit/offset).

**REQ-023** All responses MUST be JSON. Content-Type: `application/json`.

**REQ-024** `POST` and `PUT`/`PATCH` bodies MUST be JSON. `id`, `created_at`, `updated_at` are ignored if provided in body.

**REQ-025** `POST` MUST return `201 Created` with the created row.

**REQ-026** `GET /{id}` on non-existent row MUST return `404 Not Found` with `{"error": "not found"}`.

**REQ-027** `DELETE` MUST return `204 No Content` on success.

**REQ-028** Requests to unknown apps or tables MUST return `404 Not Found`.

---

### Authentication

**REQ-030** Every request to `/{app}/*` MUST include `Authorization: Bearer {token}`.

**REQ-031** The token MUST be a valid JWT signed with the app's configured `jwt_secret` (HS256).

**REQ-032** Missing or invalid token MUST return `401 Unauthorized` with `{"error": "unauthorized"}`.

**REQ-033** The system MUST NOT share JWT secrets across apps.

**REQ-034** JWT validation MUST check: signature, expiry (`exp` claim if present).

---

### CLI

**REQ-040** `zeep apply [--config path]` — applies config to PostgreSQL, reports created/updated schemas and tables.

**REQ-041** `zeep list` — lists all apps with their tables and endpoint URLs.

**REQ-042** `zeep status` — shows server health, connected PostgreSQL, and app count.

**REQ-043** `zeep serve [--config path] [--port n]` — starts the HTTP server.

**REQ-044** Flags: `--config` (default `./apps.yaml`), `--port` (default `8080`), `--db` (overrides config DSN).

---

### Observability

**REQ-050** Every request MUST be logged: method, path, status code, latency (structured JSON).

**REQ-051** `GET /health` → `{"status": "ok", "apps": N}` — no auth required.

**REQ-052** `GET /metrics` — Prometheus metrics: request count, latency histogram, active apps. No auth in M1.

---

### Deploy

**REQ-060** Ship `docker-compose.yml` that starts zeep-core + PostgreSQL with zero additional config.

**REQ-061** Docker image MUST be < 50MB.

**REQ-062** `DATABASE_URL` env var for PostgreSQL DSN. `ZEEP_CONFIG` for config file path.

---

## Out of Scope (M1)

- Storage / file uploads
- Audit logs
- Corporate SSO / LDAP
- RBAC
- Web dashboard
- GraphQL
- Realtime / websockets
- Schema deletion / column drops
