# Design: MVP Core

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                      zeep-core                          │
│                                                         │
│  ┌──────────┐   ┌──────────┐   ┌──────────────────┐   │
│  │   CLI    │   │  Server  │   │  Config Loader   │   │
│  │ (cobra)  │   │  (chi)   │   │  (apps.yaml)     │   │
│  └────┬─────┘   └────┬─────┘   └────────┬─────────┘   │
│       │              │                   │              │
│       │         ┌────▼──────────────────▼──────────┐  │
│       │         │           App Registry            │  │
│       │         │  (in-memory, loaded from config)  │  │
│       │         └────────────────┬─────────────────┘  │
│       │                          │                      │
│  ┌────▼──────┐   ┌───────────────▼────────────────┐   │
│  │Provisioner│   │         Request Router           │   │
│  │(schema    │   │  /{app}/{table}[/{id}]           │   │
│  │ manager)  │   │  + JWT middleware per app         │   │
│  └────┬──────┘   └───────────────┬────────────────┘   │
│       │                          │                      │
│       │         ┌────────────────▼────────────────┐   │
│       │         │           CRUD Handler           │   │
│       │         │  (query builder → pgx → json)    │   │
│       │         └────────────────┬────────────────┘   │
│       │                          │                      │
│  ┌────▼──────────────────────────▼────────────────┐   │
│  │              PostgreSQL Client (pgx/v5)          │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                            │
                    ┌───────▼────────┐
                    │  PostgreSQL    │
                    │  (RDS / local) │
                    │                │
                    │  app_billing   │
                    │  app_inventory │
                    │  app_hr        │
                    └────────────────┘
```

---

## Repository Structure

```
zeep-core/
├── cmd/
│   └── zeep/
│       └── main.go          # Entrypoint, cobra root command
├── internal/
│   ├── config/
│   │   ├── loader.go        # YAML parsing + validation
│   │   └── types.go         # Config structs
│   ├── registry/
│   │   └── registry.go      # In-memory app registry (thread-safe)
│   ├── provisioner/
│   │   ├── provisioner.go   # Orchestrates schema + table creation
│   │   ├── schema.go        # CREATE SCHEMA IF NOT EXISTS
│   │   └── table.go         # CREATE TABLE, ALTER TABLE ADD COLUMN
│   ├── server/
│   │   ├── server.go        # chi router setup, middleware stack
│   │   ├── handler.go       # CRUD handlers
│   │   ├── middleware.go    # JWT auth middleware per app
│   │   └── response.go      # JSON helpers, error types
│   ├── query/
│   │   └── builder.go       # Builds parameterized SQL from HTTP params
│   └── db/
│       └── client.go        # pgx pool setup, helpers
├── apps.yaml                # Default config location
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── go.mod
```

---

## Key Data Structures

### Config types (`internal/config/types.go`)

```go
type Config struct {
    Platform PlatformConfig `yaml:"platform"`
    Apps     []AppConfig    `yaml:"apps"`
}

type PlatformConfig struct {
    DatabaseURL string `yaml:"database_url"`
}

type AppConfig struct {
    Name   string        `yaml:"name"`
    Auth   AuthConfig    `yaml:"auth"`
    Tables []TableConfig `yaml:"tables"`
}

type AuthConfig struct {
    JWTSecret string `yaml:"jwt_secret"`
}

type TableConfig struct {
    Name    string         `yaml:"name"`
    Columns []ColumnConfig `yaml:"columns"`
}

type ColumnConfig struct {
    Name     string `yaml:"name"`
    Type     string `yaml:"type"`
    Required bool   `yaml:"required"`
    Default  string `yaml:"default"`
    Unique   bool   `yaml:"unique"`
}
```

### Registry (`internal/registry/registry.go`)

```go
type Registry struct {
    mu   sync.RWMutex
    apps map[string]*App
}

type App struct {
    Config     AppConfig
    SchemaName string         // "app_{name}"
    Tables     map[string]*Table
}

type Table struct {
    Name    string
    Columns []Column
}
```

---

## Request Flow

```
Request: GET /billing/invoices?status=eq.paid&limit=10

1. chi router → match /{app}/{table}
2. JWT middleware:
   - extract Bearer token
   - lookup app in registry → get jwt_secret
   - validate HS256 signature + exp
   - 401 if invalid
3. CRUD handler:
   - lookup table in registry → validate exists
   - build SQL: SELECT * FROM app_billing.invoices WHERE status=$1 LIMIT 10
   - execute via pgx pool
   - serialize rows to JSON
   - return {"data": [...], "count": N}
```

---

## SQL Generation

### Table creation

```sql
CREATE SCHEMA IF NOT EXISTS app_billing;

CREATE TABLE IF NOT EXISTS app_billing.invoices (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    amount      DECIMAL     NOT NULL,
    status      TEXT        DEFAULT 'pending',
    customer_id UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Column addition (idempotent via pg_attribute check)

```sql
ALTER TABLE app_billing.invoices ADD COLUMN IF NOT EXISTS notes TEXT;
```

### Query builder output examples

```sql
-- GET /billing/invoices?status=eq.paid&limit=10&offset=20
SELECT * FROM app_billing.invoices WHERE status=$1 LIMIT 10 OFFSET 20;

-- POST /billing/invoices  body: {"amount": 100.00, "customer_id": "..."}
INSERT INTO app_billing.invoices (amount, customer_id)
VALUES ($1, $2)
RETURNING *;

-- PATCH /billing/invoices/{id}  body: {"status": "paid"}
UPDATE app_billing.invoices SET status=$1, updated_at=now()
WHERE id=$2 RETURNING *;

-- DELETE /billing/invoices/{id}
DELETE FROM app_billing.invoices WHERE id=$1;
```

---

## apps.yaml Contract

```yaml
platform:
  database_url: ${DATABASE_URL} # env var interpolation at load time

apps:
  - name: billing
    auth:
      jwt_secret: ${BILLING_JWT_SECRET}
    tables:
      - name: invoices
        columns:
          - { name: amount, type: decimal, required: true }
          - { name: status, type: text, default: pending }
          - { name: customer_id, type: uuid }

  - name: inventory
    auth:
      jwt_secret: ${INVENTORY_JWT_SECRET}
    tables:
      - name: items
        columns:
          - { name: sku, type: text, required: true, unique: true }
          - { name: quantity, type: integer, default: "0" }
```

Env var interpolation: `${VAR}` resolved from OS env at load time. No fallback — missing var = validation error.

---

## API Response Shapes

```json
// List: GET /{app}/{table}
{ "data": [...], "count": 42, "limit": 10, "offset": 0 }

// Single: GET /{app}/{table}/{id}
{ "id": "uuid", "amount": 100.00, "created_at": "2026-01-01T00:00:00Z", ... }

// Created: POST /{app}/{table} → 201
{ "id": "uuid", "amount": 100.00, "status": "pending", "created_at": "...", ... }

// Error
{ "error": "not found" }
{ "error": "unauthorized" }
{ "error": "validation: amount is required" }
```

---

## Dependencies

| Package                               | Purpose                             |
| ------------------------------------- | ----------------------------------- |
| `github.com/go-chi/chi/v5`            | HTTP router                         |
| `github.com/jackc/pgx/v5`             | PostgreSQL driver + connection pool |
| `gopkg.in/yaml.v3`                    | YAML parsing                        |
| `github.com/golang-jwt/jwt/v5`        | JWT validation                      |
| `github.com/spf13/cobra`              | CLI framework                       |
| `github.com/prometheus/client_golang` | Metrics                             |
| `go.uber.org/zap`                     | Structured logging                  |

All MIT/Apache-2.0. No CGO — pure Go binary.

---

## Docker Compose

```yaml
services:
  zeep:
    image: ghcr.io/zeeplabs/zeep-core:latest
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://zeep:zeep@db:5432/zeep
      ZEEP_CONFIG: /config/apps.yaml
    volumes:
      - ./apps.yaml:/config/apps.yaml:ro
    depends_on:
      db:
        condition: service_healthy

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: zeep
      POSTGRES_PASSWORD: zeep
      POSTGRES_DB: zeep
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U zeep"]
      interval: 5s
      retries: 5
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
```

---

## Security

- All user-provided values via parameterized queries (`$1`, `$2`) — no string interpolation
- Column/table names from config only (validated at load, not from HTTP request params)
- Filter field names validated against registry before use in SQL
- JWT secrets only from env vars, never logged
- `updated_at` always set server-side
- No unauthenticated write endpoints
