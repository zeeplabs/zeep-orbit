# Tasks: MVP Core

## Execution Order

Tasks marked `[P]` within the same group can run in parallel.
Dependencies listed explicitly.

---

## Group 1 — Project Scaffold

### T-001: Initialize Go module + project structure

**What:** `go mod init`, create directory tree, Makefile, .gitignore, go.mod with all dependencies.
**Where:** Repository root
**Done when:** `go build ./...` passes with no errors on empty stubs.
**Gate:** `go build ./...` green

### T-002: Config types + YAML loader [P with T-001]

**What:** Implement `internal/config/types.go` and `internal/config/loader.go`.

- Parse `apps.yaml`
- Env var interpolation (`${VAR}`)
- Validation: required fields, name regex `^[a-z][a-z0-9-]{0,62}$`, type whitelist, duplicate names
  **Where:** `internal/config/`
  **Done when:** Unit tests cover valid config, missing fields, bad name format, unknown types, duplicate app names.
  **Gate:** `go test ./internal/config/...` green

---

## Group 2 — Database Layer

_Depends on: T-001_

### T-003: PostgreSQL client setup

**What:** Implement `internal/db/client.go`.

- `pgxpool.New` from `DATABASE_URL`
- `Ping` on startup
- Exported `Pool` type
  **Where:** `internal/db/`
  **Done when:** Connects to real PostgreSQL in integration tests (Docker-based test DB).
  **Gate:** `go test ./internal/db/...` green

### T-004: Schema provisioner [P with T-003]

**What:** Implement `internal/provisioner/`.

- `CREATE SCHEMA IF NOT EXISTS app_{name}`
- `CREATE TABLE IF NOT EXISTS` with injected columns (id UUID PK, created_at, updated_at) + user columns
- `ALTER TABLE ADD COLUMN IF NOT EXISTS` for new columns on existing tables
- Column type mapping: config type → PostgreSQL type
- Idempotency: running twice = no-op
  **Where:** `internal/provisioner/`
  **Depends on:** T-003
  **Done when:** Integration tests: create schema, create table, re-run (idempotent), add column.
  **Gate:** `go test ./internal/provisioner/...` green

---

## Group 3 — App Registry

_Depends on: T-002_

### T-005: App registry

**What:** Implement `internal/registry/registry.go`.

- Thread-safe map: app name → App struct
- `Load(config)` — populates from config, resolves SchemaName `app_{name}`
- `Get(appName)` → App, bool
- `GetTable(appName, tableName)` → Table, bool
  **Where:** `internal/registry/`
  **Depends on:** T-002
  **Done when:** Unit tests: load N apps, get existing, get missing, concurrent reads.
  **Gate:** `go test ./internal/registry/...` green

---

## Group 4 — Query Builder

_Depends on: T-002, T-005_

### T-006: SQL query builder

**What:** Implement `internal/query/builder.go`.

- `BuildList` → SELECT with limit/offset/eq filter/order + parameterized
- `BuildInsert` → INSERT with RETURNING \*, strips id/timestamps, validates required columns
- `BuildUpdate` → UPDATE partial fields + `updated_at=now()` + RETURNING \*
- `BuildDelete` → DELETE WHERE id=$1
- `BuildGetByID` → SELECT WHERE id=$1
- Filter field names validated against known columns (injection prevention)
  **Where:** `internal/query/`
  **Done when:** Unit tests cover all builders, unknown field rejection, injection prevention, edge cases.
  **Gate:** `go test ./internal/query/...` green

---

## Group 5 — HTTP Server

_Depends on: T-005, T-006, T-003_

### T-007: JWT auth middleware

**What:** Implement `internal/server/middleware.go`.

- Extract Bearer token
- Lookup app from registry by `{app}` path param
- Validate HS256 + exp
- 401 on any failure
- Attach app to request context
  **Where:** `internal/server/`
  **Depends on:** T-005
  **Done when:** Unit tests: valid token, expired, wrong secret, missing header, unknown app.
  **Gate:** `go test ./internal/server/ -run TestMiddleware` green

### T-008: CRUD handlers [P with T-007]

**What:** Implement `internal/server/handler.go`.

- HandleList, HandleCreate, HandleGetByID, HandleUpdate, HandleDelete, HandleHealth
- Uses query builder → pgx → JSON response
- Correct status codes: 200, 201, 204, 400, 401, 404
  **Where:** `internal/server/`
  **Depends on:** T-005, T-006, T-003
  **Done when:** Integration tests hit real PostgreSQL for all operations + error cases.
  **Gate:** `go test ./internal/server/...` green

### T-009: Router + server wiring

**What:** Implement `internal/server/server.go`.

- chi router
- Routes: `/{app}/{table}`, `/{app}/{table}/{id}`, `/health`, `/metrics`
- Middleware: logger → JWT (app routes only) → handler
- Prometheus metrics middleware
- Structured JSON request logging (zap)
  **Where:** `internal/server/`
  **Depends on:** T-007, T-008
  **Done when:** Server starts, /health returns 200, logs are JSON.
  **Gate:** Smoke test + `go test ./internal/server/...`

---

## Group 6 — CLI

_Depends on: T-002, T-004, T-009_

### T-010: CLI commands (cobra)

**What:** Implement `cmd/zeep/main.go`.

- `zeep serve` → load config → provisioner → start server
- `zeep apply` → load config → provisioner → print report
- `zeep list` → load config → print apps + tables + URLs
- `zeep status` → GET /health → print result
- Flags: `--config`, `--port`, `--db`
  **Where:** `cmd/zeep/`
  **Depends on:** T-002, T-004, T-009
  **Done when:** All four commands work end-to-end against live PostgreSQL.
  **Gate:** Manual E2E validation checklist (see below)

---

## Group 7 — Deploy

_Depends on: T-010_

### T-011: Dockerfile + docker-compose.yml

**What:**

- Multi-stage Dockerfile: golang:1.23-alpine builder → scratch final
- Image < 50MB
- `docker-compose.yml`: zeep + postgres:16-alpine + healthcheck
- `.env.example` with required vars
  **Where:** Repository root
  **Done when:** `docker compose up && curl localhost:8080/health` returns `{"status":"ok"}`.
  **Gate:** `docker compose up` → 200 on /health

---

## E2E Validation Checklist

Run after T-010 + T-011:

```bash
# Start
docker compose up -d

# Apply (first run — should create)
zeep apply
# → "Created schema app_billing | Created table invoices"

# Apply (second run — idempotent)
zeep apply
# → "No changes"

# List
zeep list
# → billing → invoices → http://localhost:8080/billing/invoices

# Create row
curl -X POST localhost:8080/billing/invoices \
  -H "Authorization: Bearer {jwt}" \
  -H "Content-Type: application/json" \
  -d '{"amount": 150.00}'
# → 201 + row with id + timestamps

# List rows
curl localhost:8080/billing/invoices -H "Authorization: Bearer {jwt}"
# → {"data":[...],"count":1,"limit":50,"offset":0}

# Filter
curl "localhost:8080/billing/invoices?status=eq.pending" -H "Authorization: Bearer {jwt}"
# → 1 result

# Update
curl -X PATCH localhost:8080/billing/invoices/{id} \
  -H "Authorization: Bearer {jwt}" -d '{"status":"paid"}'
# → updated row, updated_at changed

# Unauthorized — no token
curl localhost:8080/billing/invoices
# → 401 {"error":"unauthorized"}

# Cross-app token leak
curl localhost:8080/inventory/items -H "Authorization: Bearer {billing_jwt}"
# → 401

# Delete
curl -X DELETE localhost:8080/billing/invoices/{id} -H "Authorization: Bearer {jwt}"
# → 204

# Get deleted
curl localhost:8080/billing/invoices/{id} -H "Authorization: Bearer {jwt}"
# → 404 {"error":"not found"}

# Health (no auth)
curl localhost:8080/health
# → {"status":"ok","apps":2}
```

---

## Estimated Effort

| Group         | Tasks               | Effort      |
| ------------- | ------------------- | ----------- |
| Scaffold      | T-001, T-002        | 0.5 day     |
| Database      | T-003, T-004        | 1 day       |
| Registry      | T-005               | 0.5 day     |
| Query Builder | T-006               | 1 day       |
| HTTP Server   | T-007, T-008, T-009 | 2 days      |
| CLI           | T-010               | 0.5 day     |
| Deploy        | T-011               | 0.5 day     |
| **Total**     |                     | **~6 days** |
