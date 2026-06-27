# zeep-core

**YAML schema → instant REST API backed by PostgreSQL.**

Define your data model in a YAML file. zeep-core provisions the database and serves a fully authenticated CRUD API — no migrations, no boilerplate.

```yaml
platform:
  database_url: ${DATABASE_URL}

apps:
  - name: billing
    auth:
      jwt_secret: ${BILLING_JWT_SECRET}
    tables:
      - name: invoices
        columns:
          - name: amount
            type: decimal
            required: true
          - name: status
            type: text
            required: true
```

```bash
docker compose up -d
curl localhost:8080/health
# {"status":"ok","apps":1}
```

---

## How it works

1. zeep reads your `apps.yaml`
2. Creates PostgreSQL schemas and tables (idempotent — safe to re-run)
3. Starts an HTTP server with one CRUD API per table
4. Each app is protected by its own HS256 JWT secret

Every table gets `id` (UUID, auto-generated), `created_at`, and `updated_at` automatically.

---

## Quick start

**Requirements:** Docker + Docker Compose

```bash
git clone https://github.com/zeep-tecnologia/zeep-core
cd zeep-core
cp .env.example .env   # edit secrets

docker compose up -d
```

Verify:

```bash
curl localhost:8080/health
```

---

## Configuration

### `apps.yaml` structure

```yaml
platform:
  database_url: ${DATABASE_URL}   # supports ${ENV_VAR} interpolation

apps:
  - name: myapp                   # lowercase, a-z0-9- only
    auth:
      jwt_secret: ${MY_JWT_SECRET}
    tables:
      - name: items
        columns:
          - name: title
            type: text
            required: true
          - name: published
            type: boolean
            default: "false"
          - name: score
            type: decimal
```

### Column types

| Type | PostgreSQL |
|------|-----------|
| `text` | TEXT |
| `integer` | INTEGER |
| `bigint` | BIGINT |
| `decimal` | DECIMAL |
| `boolean` | BOOLEAN |
| `uuid` | UUID |
| `timestamptz` | TIMESTAMPTZ |
| `jsonb` | JSONB |

### Column options

| Field | Description |
|-------|-------------|
| `required` | NOT NULL constraint |
| `unique` | UNIQUE constraint |
| `default` | DEFAULT value (SQL expression) |

### Auto-generated columns

Every table gets these automatically — do not declare them:

| Column | Type | Value |
|--------|------|-------|
| `id` | UUID | `gen_random_uuid()` |
| `created_at` | TIMESTAMPTZ | `now()` |
| `updated_at` | TIMESTAMPTZ | `now()` (updated on PATCH/PUT) |

---

## REST API

### Authentication

All app routes require a Bearer JWT signed with the app's `jwt_secret` (HS256).

```bash
TOKEN=$(jwt encode --secret "$MY_JWT_SECRET" '{}')

curl localhost:8080/myapp/items \
  -H "Authorization: Bearer $TOKEN"
```

Tokens are validated per-app — a token for `billing` is rejected on `inventory` routes.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/{app}/{table}` | List records |
| POST | `/{app}/{table}` | Create record |
| GET | `/{app}/{table}/{id}` | Get by ID |
| PUT / PATCH | `/{app}/{table}/{id}` | Update (partial) |
| DELETE | `/{app}/{table}/{id}` | Delete |
| GET | `/health` | Health check (no auth) |
| GET | `/metrics` | Prometheus metrics (no auth) |

### List — query parameters

| Param | Example | Description |
|-------|---------|-------------|
| `limit` | `?limit=20` | Max records (default 50, max 1000) |
| `offset` | `?offset=100` | Skip N records |
| `field=eq.value` | `?status=eq.active` | Filter by equality |
| `order` | `?order=created_at.desc` | Sort (`asc` or `desc`) |

### Response format

**List:**
```json
{
  "data": [...],
  "count": 42,
  "limit": 50,
  "offset": 0
}
```

**Single record:**
```json
{
  "id": "018e4c72-...",
  "title": "Hello",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

**Error:**
```json
{ "error": "not found" }
```

### Example — full CRUD

```bash
BASE="localhost:8080/billing/invoices"
AUTH="-H \"Authorization: Bearer $TOKEN\""

# Create
curl -X POST "$BASE" $AUTH \
  -H "Content-Type: application/json" \
  -d '{"amount": 150.00, "status": "pending"}'
# 201 + record

# List
curl "$BASE?status=eq.pending" $AUTH
# 200 + {"data":[...],"count":1,...}

# Get
curl "$BASE/018e4c72-..." $AUTH
# 200 + record

# Update
curl -X PATCH "$BASE/018e4c72-..." $AUTH \
  -d '{"status": "paid"}'
# 200 + updated record

# Delete
curl -X DELETE "$BASE/018e4c72-..." $AUTH
# 204
```

---

## CLI reference

```
zeep [command] [flags]

Commands:
  serve    Load config, provision database, start HTTP server
  apply    Provision database schemas and tables, print report
  list     Print apps, tables, and their API URLs
  status   Check if the server is running

Global flags:
  --config string   Config file path (default "./apps.yaml")
  --db string       Override DATABASE_URL from config
  --port int        HTTP server port (default 8080)
```

### `zeep serve`

Starts the full stack: provision → registry → HTTP server. Handles SIGINT/SIGTERM with a 30-second graceful shutdown.

```bash
zeep serve --config ./apps.yaml --port 8080
```

### `zeep apply`

Idempotent provisioning. Safe to run multiple times.

```bash
zeep apply
# ✓ Created schema billing
# ✓ Created table billing.invoices

zeep apply   # second run
#   No changes
```

### `zeep list`

Inspect config without a database connection.

```bash
zeep list
# billing
#   invoices → http://localhost:8080/billing/invoices
#   payments → http://localhost:8080/billing/payments
```

### `zeep status`

```bash
zeep status
# Status: ok
# Apps: 2
```

---

## Observability

### Health check

```
GET /health → {"status":"ok","apps":2}
```

### Prometheus metrics

```
GET /metrics
```

| Metric | Type | Description |
|--------|------|-------------|
| `zeep_http_requests_total` | Counter | Total requests by method and status |
| `zeep_http_request_duration_seconds` | Histogram | Request latency by method |
| `zeep_active_apps` | Gauge | Number of loaded apps |

### Structured logging

JSON logs via `zap`. Set `LOG_LEVEL=debug` for development output.

---

## Development

**Requirements:** Go 1.26+, PostgreSQL 14+

```bash
# Build
make build

# Run tests (unit only — no DB required)
make test

# Run all tests including integration
TEST_DATABASE_URL=postgres://user:pass@localhost/testdb go test ./...

# Lint
make lint

# Run locally
DATABASE_URL=postgres://... BILLING_JWT_SECRET=secret zeep serve
```

### Project structure

```
cmd/zeep/          CLI entrypoint (cobra)
internal/
  config/          YAML loader + validation
  db/              pgxpool client
  provisioner/     Schema/table provisioning
  registry/        Thread-safe app registry
  query/           SQL query builder (injection-safe)
  server/          HTTP handlers, JWT middleware, router
```

---

## License

MIT — see [LICENSE](LICENSE).
